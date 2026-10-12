package remote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// guacUpstreamError 是 Guacamole 状态码 UPSTREAM_ERROR（0x0203）。
// 浏览器收到 error 指令后用它弹出失败原因，而不是停在「连接中」。
const guacUpstreamError = "515"

// 会话收尾原因（写入 close_reason）。
const (
	// closeReasonNormal 客户端正常关闭。
	closeReasonNormal = "客户端断开"
	// closeReasonGatewayClosed 网关侧关闭（网络中断或远端会话结束）。
	closeReasonGatewayClosed = "网关连接结束"
	// closeReasonHandshakeFailed 握手失败。
	closeReasonHandshakeFailed = "握手失败"
	// closeReasonIdleTimeout 空闲超时被回收。
	closeReasonIdleTimeout = "空闲超时断开"
	// closeReasonRestart 上次进程异常退出遗留。
	closeReasonRestart = "服务重启中断"
)

// ServeRemoteDesktop 承载一次远程桌面（RDP / VNC）WebSocket 会话。
//
// 整个生命周期都在这里闭环：建立会话记录 → 拨号 guacd → 握手（权威注入凭据与录像参数）
// → 纯字节中继 → 收尾落库。任何一步失败都不会留下处于 active 的僵尸会话行。
//
// 参数 Parameters:
//   - client (*websocket.Conn): 已升级的浏览器连接（本函数负责关闭）。
//   - ticket (Ticket): 已消费的一次性票据。
//   - clientIP (string): 操作人来源地址。
//   - hint (DisplayHint): 浏览器上报的初始分辨率。
func (s *Service) ServeRemoteDesktop(client *websocket.Conn, ticket Ticket, clientIP string, hint DisplayHint) {
	defer client.Close()

	if !isRemoteDesktopProtocol(ticket.Protocol) {
		writeClientError(client, "不支持的远程桌面协议")
		return
	}
	if s.guac.Client == nil {
		writeClientError(client, "远程桌面未启用（未配置 guacd）")
		return
	}

	// 连接配置与密码：这是密码唯一的落点，永远不会下发给浏览器。
	endpoint, password, err := s.DecryptEndpointSecret(ticket.AgentID, ticket.Protocol)
	if err != nil {
		s.logSessionFailure(ticket, clientIP, fmt.Sprintf("读取连接配置失败: %v", err))
		// 也要补一条审计记录：只在日志里留痕的话，管理员在会话列表里看不到这次失败尝试，
		// 会误判成「这台机器从来没人连过」。
		s.recordFailedSession(ticket, clientIP, "读取连接配置失败")
		writeClientError(client, "读取连接配置失败，请检查凭据是否已配置")
		return
	}

	slog.Info("远程桌面开始连接",
		"agent_id", ticket.AgentID,
		"protocol", ticket.Protocol,
		"operator", ticket.Operator,
		"host", endpoint.Host,
		"port", endpoint.Port)

	startedAt := time.Now()
	session := &RDPSession{
		AgentID:   ticket.AgentID,
		Protocol:  ticket.Protocol,
		Operator:  ticket.Operator,
		Username:  endpoint.Username,
		ClientIP:  clientIP,
		Status:    SessionActive,
		StartedAt: startedAt,
	}
	if err := s.db.Create(session).Error; err != nil {
		writeClientError(client, "创建会话记录失败")
		return
	}

	// 收尾统一在 defer 完成，保证不存在永久 active 的行。
	var (
		bytesIn        int64
		bytesOut       int64
		reason         = closeReasonNormal
		recordingPath  string
		sessionFailure bool
	)
	defer func() {
		s.finishSession(session, bytesIn, bytesOut, reason, recordingPath, sessionFailure)
	}()

	// 录像文件名需要会话主键，因此必须在建行之后构造。
	plan := s.guac.Client.BuildConnectPlan(endpoint, password, session.ID, startedAt)
	recordingPath = plan.RecordingRelative

	dialCtx, cancel := context.WithTimeout(context.Background(), s.guac.Client.dialTimeout)
	defer cancel()
	gateway, err := s.guac.Client.Dial(dialCtx)
	if err != nil {
		reason, sessionFailure = closeReasonHandshakeFailed, true
		s.logSessionFailure(ticket, clientIP, err.Error())
		writeClientError(client, "连接远程桌面网关失败："+err.Error())
		return
	}
	defer gateway.Close()

	// 握手只发生在 webserver 和 guacd 之间，不读浏览器。
	// 浏览器的 guacamole-common-js 不会发 select，在这里等它会一直卡到超时。
	handshakeTimeout := s.guac.HandshakeTimeout
	if handshakeTimeout <= 0 {
		handshakeTimeout = 15 * time.Second
	}
	_ = gateway.SetReadDeadline(time.Now().Add(handshakeTimeout))
	handshakeErr := handshakeGuacd(gateway, ticket.Protocol, hint, plan.Authority, plan.Inject)
	_ = gateway.SetReadDeadline(time.Time{})

	// #region debug-point A:guacd-handshake-result
	if event, marshalErr := json.Marshal(map[string]any{
		"sessionId":    "vnc-realvnc-519",
		"runId":        "pre-fix",
		"hypothesisId": "A",
		"location":     "internal/remote/guacproxy.go:handshakeGuacd",
		"msg":          "[DEBUG] guacd handshake result",
		"data":         map[string]any{"agentId": ticket.AgentID, "protocol": ticket.Protocol, "host": endpoint.Host, "port": endpoint.Port, "sessionId": session.ID, "error": fmt.Sprint(handshakeErr)},
		"ts":           time.Now().UnixMilli(),
	}); marshalErr == nil {
		if request, requestErr := http.NewRequest(http.MethodPost, "http://127.0.0.1:7777/event", bytes.NewReader(event)); requestErr == nil {
			request.Header.Set("Content-Type", "application/json")
			if response, reportErr := (&http.Client{Timeout: time.Second}).Do(request); reportErr == nil {
				_ = response.Body.Close()
			}
		}
	}
	// #endregion

	if handshakeErr != nil {
		reason, sessionFailure = closeReasonHandshakeFailed, true
		s.logSessionFailure(ticket, clientIP, handshakeErr.Error())
		writeClientError(client, handshakeErr.Error())
		return
	}

	// 第一条指令到了，浏览器才把隧道标成已打开并开始发 sync/ping。
	// 用空 opcode（隧道内部指令）带上会话号，画面指令还没来时也不会被 30 秒收包超时掐掉。
	if err := writeClientInstruction(client, "", strconv.FormatUint(uint64(session.ID), 10)); err != nil {
		reason, sessionFailure = closeReasonHandshakeFailed, true
		s.logSessionFailure(ticket, clientIP, err.Error())
		return
	}

	reason = s.relay(client, gateway, &bytesIn, &bytesOut)
	slog.Info("远程桌面会话结束",
		"agent_id", ticket.AgentID,
		"protocol", ticket.Protocol,
		"operator", ticket.Operator,
		"client_ip", clientIP,
		"session_id", session.ID,
		"duration_seconds", int(time.Since(startedAt).Seconds()),
		"bytes_in", bytesIn,
		"bytes_out", bytesOut,
		"close_reason", reason,
		"recording_path", recordingPath)
}

// relay 在握手完成后把 guacd 的指令流转成浏览器能收的 WebSocket 文本帧。
//
// guacamole-common-js 的 WebSocketTunnel 要求每个文本帧里是完整指令（可以多条），
// 帧被从中间切开会直接报 Incomplete instruction 并断开。所以下行仍按指令边界切，
// 不能按固定字节块转发。上行要吃掉隧道自己的 ping（opcode 为空），原样回显，
// 不能转给 guacd——guacd 不认识这条内部指令。
//
// 参数 Parameters:
//   - client (*websocket.Conn): 浏览器连接。
//   - gateway (*GuacdConn): guacd 连接。
//   - bytesIn (*int64): 累计上行字节。
//   - bytesOut (*int64): 累计下行字节。
//
// 返回 Returns:
//   - reason (string): 断开原因。
func (s *Service) relay(client *websocket.Conn, gateway *GuacdConn, bytesIn, bytesOut *int64) string {
	var (
		wg       sync.WaitGroup
		once     sync.Once
		writeMu  sync.Mutex
		reason   atomic.Value
		shutDown = func(value string) {
			once.Do(func() {
				reason.Store(value)
				// 主动断开两端，让另一个方向的阻塞读立即返回。
				_ = gateway.Close()
				_ = client.Close()
			})
		}
	)
	reason.Store(closeReasonNormal)

	// 客户端 → 网关
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			_, data, err := client.ReadMessage()
			if err != nil {
				shutDown(closeReasonNormal)
				return
			}
			n, err := forwardClientInstructions(client, gateway, &writeMu, data)
			if err != nil {
				shutDown(closeReasonGatewayClosed)
				return
			}
			atomic.AddInt64(bytesIn, int64(n))
		}
	}()

	// 网关 → 客户端。一条指令一个文本帧。
	wg.Add(1)
	go func() {
		defer wg.Done()
		reader := gateway.Buffered()
		for {
			if s.guac.IdleTimeout > 0 {
				_ = gateway.SetReadDeadline(time.Now().Add(s.guac.IdleTimeout))
			}
			opcode, args, err := readInstruction(reader)
			if opcode != "" || len(args) > 0 {
				if opcode == opError {
					slog.Warn("远程桌面网关返回错误",
						"message", strings.Join(args, " "),
						"status", lastArg(args))
				}
				payload, encErr := encodeInstruction(opcode, args...)
				if encErr != nil {
					shutDown(closeReasonGatewayClosed)
					return
				}
				writeMu.Lock()
				writeErr := client.WriteMessage(websocket.TextMessage, payload)
				writeMu.Unlock()
				if writeErr != nil {
					shutDown(closeReasonNormal)
					return
				}
				atomic.AddInt64(bytesOut, int64(len(payload)))
			}
			if err != nil {
				switch {
				case errors.Is(err, io.EOF):
					shutDown(closeReasonGatewayClosed)
				case isTimeout(err):
					shutDown(closeReasonIdleTimeout)
				default:
					shutDown(closeReasonGatewayClosed)
				}
				return
			}
		}
	}()

	wg.Wait()
	return reason.Load().(string)
}

func lastArg(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

// forwardClientInstructions 处理浏览器发来的一个 WebSocket 帧。
//
// 帧里可以有多条完整指令。空 opcode 且第一个参数是 ping 的，是隧道探活，回显给浏览器；
// 其余原样写给 guacd。
//
// 返回 Returns:
//   - forwarded (int): 写给 guacd 的字节数。
//   - err (error): 帧不完整或写入失败时返回非 nil。
func forwardClientInstructions(client *websocket.Conn, gateway *GuacdConn, writeMu *sync.Mutex, data []byte) (int, error) {
	pending := data
	forwarded := 0
	for len(bytes.TrimSpace(pending)) > 0 {
		buffered := bufio.NewReader(bytes.NewReader(pending))
		opcode, args, err := readInstruction(buffered)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return forwarded, fmt.Errorf("浏览器指令不完整")
			}
			return forwarded, err
		}
		consumed := len(pending) - buffered.Buffered()
		chunk := pending[:consumed]
		pending = pending[consumed:]
		if opcode == "" && len(args) >= 1 && args[0] == "ping" {
			writeMu.Lock()
			err = writeClientInstruction(client, opcode, args...)
			writeMu.Unlock()
			if err != nil {
				return forwarded, err
			}
			continue
		}
		if _, err := gateway.Write(chunk); err != nil {
			return forwarded, err
		}
		forwarded += len(chunk)
	}
	return forwarded, nil
}

// encodeInstruction 把一条指令编码成协议字节。
func encodeInstruction(opcode string, args ...string) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeInstruction(&buf, opcode, args...); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeClientInstruction 向浏览器写一条 Guacamole 指令（文本帧，不能用二进制帧）。
func writeClientInstruction(client *websocket.Conn, opcode string, args ...string) error {
	payload, err := encodeInstruction(opcode, args...)
	if err != nil {
		return err
	}
	return client.WriteMessage(websocket.TextMessage, payload)
}

// writeClientError 让浏览器弹出失败原因。
//
// 必须是 error 指令：纯文本会被隧道解析器当成坏指令丢掉，页面就一直停在「连接中」。
func writeClientError(client *websocket.Conn, message string) {
	_ = writeClientInstruction(client, opError, message, guacUpstreamError)
}

// finishSession 收尾会话记录：写结束时间、时长、字节数与断开原因。
//
// 参数 Parameters:
//   - session (*RDPSession): 会话行。
//   - bytesIn, bytesOut (int64): 双向字节数。
//   - reason (string): 断开原因。
//   - recordingPath (string): 录像相对路径。
//   - failed (bool): 是否属于建立失败。
func (s *Service) finishSession(session *RDPSession, bytesIn, bytesOut int64, reason, recordingPath string, failed bool) {
	endedAt := time.Now()
	status := SessionClosed
	if failed {
		status = SessionFailed
	}
	updates := map[string]any{
		"status":         status,
		"ended_at":       endedAt,
		"duration_sec":   int64(endedAt.Sub(session.StartedAt).Seconds()),
		"bytes_in":       bytesIn,
		"bytes_out":      bytesOut,
		"close_reason":   reason,
		"recording_path": recordingPath,
	}
	if err := s.db.Model(&RDPSession{}).Where("id = ?", session.ID).Updates(updates).Error; err != nil {
		slog.Error("更新远程桌面会话记录失败", "session_id", session.ID, "error", err)
	}
}

// logSessionFailure 记录会话建立失败的日志。
//
// 绝不记录密码、票据明文与录像内容——只记标识与原因。
func (s *Service) logSessionFailure(ticket Ticket, clientIP, detail string) {
	slog.Warn("远程桌面会话建立失败",
		"agent_id", ticket.AgentID,
		"protocol", ticket.Protocol,
		"operator", ticket.Operator,
		"client_ip", clientIP,
		"detail", detail)
}

// recordFailedSession 为「连接尚未建立就失败」的情况补一条审计记录。
//
// 为什么需要：审计要求异常同样可见。只写日志的话，管理员在会话列表里看不到这次
// 失败尝试，会误判成「这台机器从来没人连过」。这里会话行是自包含的（已完成状态）。
//
// 参数 Parameters:
//   - ticket (Ticket): 票据信息。
//   - clientIP (string): 操作人来源地址。
//   - reason (string): 失败原因。
func (s *Service) recordFailedSession(ticket Ticket, clientIP, reason string) {
	now := time.Now()
	if err := s.db.Create(&RDPSession{
		AgentID:     ticket.AgentID,
		Protocol:    ticket.Protocol,
		Operator:    ticket.Operator,
		ClientIP:    clientIP,
		Status:      SessionFailed,
		CloseReason: reason,
		StartedAt:   now,
		EndedAt:     &now,
	}).Error; err != nil {
		slog.Warn("写入失败的远程桌面会话记录失败",
			"agent_id", ticket.AgentID, "operator", ticket.Operator, "error", err)
	}
}

// ReconcileOrphanSessions 把上一次进程遗留的 active 会话标记为「服务重启中断」。
//
// 为什么需要：进程被强杀时 defer 不会执行，会留下永久 active 的僵尸行，
// 让审计列表看起来永远有会话在进行中。启动时对账一次即可。
//
// 返回 Returns:
//   - affected (int64): 被修正的会话数。
//   - err (error): 更新失败时返回非 nil。
func (s *Service) ReconcileOrphanSessions() (int64, error) {
	result := s.db.Exec(`update rdp_sessions set
		status = ?,
		close_reason = ?,
		ended_at = now(),
		duration_sec = greatest(0, extract(epoch from (now() - started_at))::bigint)
		where status = ?`, SessionFailed, closeReasonRestart, SessionActive)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// isTimeout 判断错误是否为超时。
func isTimeout(err error) bool {
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return errors.Is(err, context.DeadlineExceeded)
}
