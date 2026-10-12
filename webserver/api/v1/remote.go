package v1

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/company/monitor-webserver/internal/remote"
	"github.com/company/monitor-webserver/internal/util"
)

// terminalFailureMaxReasonLength 是推送到终端的失败原因最大长度（避免刷屏）。
const terminalFailureMaxReasonLength = 600

// upgrader 把 HTTP 升级为 WebSocket（SSH 终端用）。
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// 鉴权走一次性 ticket，不依赖 Origin，因此放开跨源校验。
	CheckOrigin: func(*http.Request) bool { return true },
}

// rdpUpgrader 专供远程桌面。guacamole-common-js 会带上子协议 "guacamole"，
// 读写缓冲也按画面帧放大。不改 SSH 用的 upgrader。
var rdpUpgrader = websocket.Upgrader{
	ReadBufferSize:  64 << 10,
	WriteBufferSize: 64 << 10,
	Subprotocols:    []string{"guacamole"},
	CheckOrigin:     func(*http.Request) bool { return true },
}

// RemoteAPI 是远程运维（SSH/RDP）与操作审计的 HTTP 控制器。
type RemoteAPI struct {
	// remote 远程运维服务。
	remote *remote.Service
}

// NewRemoteAPI 创建远程运维控制器。
func NewRemoteAPI(service *remote.Service) *RemoteAPI { return &RemoteAPI{remote: service} }

// Register 注册远程运维 REST 路由（全部挂在已登录组）。
func (h *RemoteAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/monitor/agents/:id/endpoint", h.Endpoint)
	group.PUT("/monitor/agents/:id/endpoint", guard("monitor:access:manage"), h.SaveEndpoint)
	group.DELETE("/monitor/agents/:id/endpoint", guard("monitor:access:manage"), h.DeleteEndpoint)
	group.POST("/monitor/ssh/ticket", guard("monitor:ssh:connect"), h.SSHTicket)
	group.POST("/monitor/rdp/token", guard("monitor:rdp:connect"), h.RDPToken)
	group.GET("/monitor/sftp/ls", guard("monitor:ssh:connect"), h.SFTPLs)
	group.POST("/monitor/sftp/upload", guard("monitor:ssh:connect"), h.SFTPUpload)
	group.GET("/monitor/sftp/download", guard("monitor:ssh:connect"), h.SFTPDownload)
	group.POST("/monitor/sftp/rename", guard("monitor:ssh:connect"), h.SFTPRename)
	group.POST("/monitor/sftp/delete", guard("monitor:ssh:connect"), h.SFTPDelete)
	group.GET("/monitor/sftp/read", guard("monitor:sftp:read"), h.SFTPRead)
	group.POST("/monitor/sftp/write", guard("monitor:sftp:write"), h.SFTPWrite)
	// 进程列表与杀进程：读取复用 SSH 连接权限，结束进程单独用更高权限的标识。
	group.GET("/monitor/agents/:id/processes", guard("monitor:ssh:connect"), h.AgentProcesses)
	group.POST("/monitor/agents/:id/processes/kill", guard("monitor:ssh:process:kill"), h.KillAgentProcess)
	group.POST("/monitor/agents/:id/processes/restart", guard("monitor:ssh:process:restart"), h.RestartAgentProcess)
	// 主机时钟：SSH 控制台左侧栏的时区 / 时钟 / 时钟偏移。
	//
	// 只读，与「查询进程列表」一样复用 monitor:ssh:connect：能打开 SSH 终端的人本就握着
	// 远端 shell，再为读一个 date 单独开权限标识只会增加授权负担，因此不为它新增菜单行
	// （与 sftp/ls 等只读端点同一取舍；只有 sftp/read、sftp/write 这类高敏感操作才单独建行）。
	group.GET("/monitor/agents/:id/clock", guard("monitor:ssh:connect"), h.AgentClock)
	// 监听端口与 systemd 服务：SSH 控制台右上角两个抽屉的数据来源。只读，复用 SSH 连接权限。
	group.GET("/monitor/agents/:id/ports", guard("monitor:ssh:connect"), h.AgentPorts)
	group.GET("/monitor/agents/:id/services", guard("monitor:ssh:connect"), h.AgentServices)
	// 服务启停 / 重启 / 自启：高危，独立权限标识（与「结束进程」同级），
	// 并在服务端对核心系统与安全相关的单元做硬拦截（见 remote.ServiceOperationBlocked）。
	group.POST("/monitor/agents/:id/services/operate", guard("monitor:ssh:service:manage"), h.OperateAgentService)
	// 二级抽屉：点击列表行后展开的详情与日志。均为只读，复用 SSH 连接权限。
	//
	// 单元名与进程号都走查询参数而不是路径段：单元名里带点（acpid.service），
	// 放路径里容易被客户端或中间件做额外处理（编码、按扩展名识别）。
	group.GET("/monitor/agents/:id/services/detail", guard("monitor:ssh:connect"), h.AgentServiceDetail)
	group.GET("/monitor/agents/:id/services/logs", guard("monitor:ssh:connect"), h.AgentServiceLogs)
	group.GET("/monitor/agents/:id/processes/detail", guard("monitor:ssh:connect"), h.AgentProcessDetail)
	// 远程桌面（RDP / VNC）会话审计：与 SSH 的会话审计分开成独立端点与权限，
	// 这样既不触碰既有实现，也能把远程桌面的查看权限单独授予。
	group.GET("/monitor/rdp/sessions", guard("monitor:rdp:view"), h.RemoteSessions)
	group.GET("/monitor/rdp/sessions/:id/recording", guard("monitor:rdp:view"), h.RDPRecording)
	group.GET("/monitor/audit/sessions", guard("monitor:audit:view"), h.ListSessions)
	group.GET("/monitor/audit/sessions/:id/commands", guard("monitor:audit:view"), h.SessionCommands)
	group.GET("/monitor/audit/sessions/:id/recording", guard("monitor:audit:view"), h.Recording)
	// 批量清理审计记录：删除不可逆（连同录像与命令流），因此不复用只读的 view 权限。
	group.DELETE("/monitor/audit/sessions/:ids", guard("monitor:audit:delete"), h.DeleteSessions)
	group.DELETE("/monitor/rdp/sessions/:ids", guard("monitor:rdp:delete"), h.DeleteRemoteSessions)
}

// DeleteSessions 处理 DELETE /api/v1/monitor/audit/sessions/:ids：批量清理 SSH 会话审计记录。
//
// 路径参数为逗号分隔的会话 ID；命令流与录像文件会一并清理（见 remote.Service.DeleteSessions）。
func (h *RemoteAPI) DeleteSessions(c *gin.Context) {
	ids := uintIDs(util.ParseIDList(c.Param("ids")))
	if len(ids) == 0 {
		BadRequest(c, "会话 ID 无效")
		return
	}
	removed, err := h.remote.DeleteSessions(ids)
	if err != nil {
		Fail(c, err, "清理会话记录失败")
		return
	}
	OK(c, gin.H{"removed": removed})
}

// DeleteRemoteSessions 处理 DELETE /api/v1/monitor/rdp/sessions/:ids：批量清理远程桌面会话记录。
//
// 路径参数为逗号分隔的会话 ID；对应录像文件会一并删除。
func (h *RemoteAPI) DeleteRemoteSessions(c *gin.Context) {
	ids := uintIDs(util.ParseIDList(c.Param("ids")))
	if len(ids) == 0 {
		BadRequest(c, "会话 ID 无效")
		return
	}
	removed, err := h.remote.DeleteRemoteSessions(ids)
	if err != nil {
		Fail(c, err, "清理远程会话失败")
		return
	}
	OK(c, gin.H{"removed": removed})
}

// uintIDs 把 ID 列表转换为无符号主键列表（util.ParseIDList 已过滤非正数，这里只做类型转换）。
//
// 参数 Parameters:
//   - ids ([]int64): ID 列表。
//
// 返回 Returns:
//   - values ([]uint): 主键列表。
func uintIDs(ids []int64) []uint {
	values := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			values = append(values, uint(id))
		}
	}
	return values
}

// Endpoint 处理 GET /api/v1/monitor/agents/:id/endpoint：查询连接配置（不含明文）。
//
// protocol 查询参数取 ssh / rdp / vnc：同一台资产可以并存多条配置，必须指明取哪一条。
// 不传时按 ssh → rdp → vnc 顺序返回第一条，兼容旧前端（改造前一台机器只有一条配置）。
func (h *RemoteAPI) Endpoint(c *gin.Context) {
	agentID := c.Param("id")
	var (
		view *remote.EndpointView
		err  error
	)
	if strings.TrimSpace(c.Query("protocol")) == "" {
		view, err = h.remote.FirstEndpoint(agentID)
	} else {
		protocol, parseErr := remote.ParseProtocol(c.Query("protocol"))
		if parseErr != nil {
			BadRequest(c, parseErr.Error())
			return
		}
		view, err = h.remote.Endpoint(agentID, protocol)
	}
	if err != nil {
		Fail(c, err, "查询连接配置失败")
		return
	}
	if view == nil {
		OK(c, gin.H{"configured": false})
		return
	}
	OK(c, gin.H{"configured": true, "endpoint": view})
}

// SaveEndpoint 处理 PUT /api/v1/monitor/agents/:id/endpoint：保存连接配置。
//
// 协议取自请求体：写入的是「该资产 + 该协议」这一条，不影响同机其它协议的配置。
func (h *RemoteAPI) SaveEndpoint(c *gin.Context) {
	var form remote.EndpointForm
	if !BindJSON(c, &form) {
		return
	}
	operator := CurrentUser(c)
	var operatorID int64
	if operator != nil {
		operatorID = operator.ID
	}
	if err := h.remote.SaveEndpoint(c.Param("id"), form, operatorID); err != nil {
		Fail(c, err, "保存连接配置失败")
		return
	}
	OK(c, nil)
}

// DeleteEndpoint 处理 DELETE /api/v1/monitor/agents/:id/endpoint：删除连接配置。
//
// protocol 查询参数必传（ssh / rdp / vnc）：不带会把该资产所有协议的配置一起删掉。
func (h *RemoteAPI) DeleteEndpoint(c *gin.Context) {
	protocol, err := remote.ParseProtocol(c.Query("protocol"))
	if err != nil {
		BadRequest(c, err.Error())
		return
	}
	if err := h.remote.DeleteEndpoint(c.Param("id"), protocol); err != nil {
		Fail(c, err, "删除连接配置失败")
		return
	}
	OK(c, nil)
}

// SSHTicket 处理 POST /api/v1/monitor/ssh/ticket：换取一次性 SSH 连接票据。
func (h *RemoteAPI) SSHTicket(c *gin.Context) {
	var body struct {
		AgentID string `json:"agentId"`
	}
	if !BindJSON(c, &body) {
		return
	}
	token, err := h.remote.IssueSSHTicket(body.AgentID, operatorName(c))
	if err != nil {
		Fail(c, err, "获取 SSH 连接票据失败")
		return
	}
	OK(c, gin.H{"ticket": token})
}

// remoteDesktopWSPath 是远程桌面（RDP / VNC）的 WebSocket 端点路径。
//
// 与 SSH 的 /monitor/ssh/ws 同层：都位于需登录组之外，靠一次性票据鉴权
// （浏览器发起 WS 握手时无法自定义 Authorization 头）。
const remoteDesktopWSPath = "/api/v1/monitor/rdp/ws"

// RDPToken 处理 POST /api/v1/monitor/rdp/token：为资产的远程桌面连接签发一次性 WS 票据。
//
// 路径沿用 rdp 前缀以兼容既有前端与权限标识（monitor:rdp:connect），实际同时承载 RDP 与 VNC。
// 请求体的 protocol 指明要连哪一种：同一台机器可以两条都配，不指定就按 rdp → vnc 探测，
// 可能与操作者点击的入口不符（点 VNC 却进了 RDP）。协议在出参中回传，前端据此渲染。
func (h *RemoteAPI) RDPToken(c *gin.Context) {
	var body struct {
		AgentID string `json:"agentId"`
		// Protocol 期望协议：rdp / vnc；留空时由服务端探测（兼容旧前端）。
		Protocol string `json:"protocol"`
	}
	if !BindJSON(c, &body) {
		return
	}
	ticket, protocol, err := h.remote.IssueRemoteTicket(body.AgentID, body.Protocol, operatorName(c))
	if err != nil {
		Fail(c, err, "发起远程桌面连接失败")
		return
	}
	// 出参只有票据与 WS 路径，不含任何连接凭据：地址、账号、密码都由代理层在握手期
	// 从数据库读取并权威注入，浏览器拿不到也改不了。
	OK(c, gin.H{
		"ticket":   ticket,
		"protocol": protocol,
		"wsPath":   remoteDesktopWSPath,
		// 录像开关以服务端配置为准，前端据此提示「本次操作正在被记录」。
		"recording": h.remote.RecordingEnabled(),
	})
}

// RDPWebSocket 处理 GET /api/v1/monitor/rdp/ws：远程桌面（RDP / VNC）WebSocket。
//
// 与 SSH 终端同一套鉴权模型：票据一次性、取用即焚，所以它挂在全局 engine 层而不是登录组里。
// 连接地址与凭据都不出现在 URL 与出参中，由代理层在握手期从数据库读取后注入 guacd。
func (h *RemoteAPI) RDPWebSocket(c *gin.Context) {
	ticket, ok := h.remote.ConsumeTicket(c.Query("ticket"))
	if !ok {
		write(c, http.StatusForbidden, CodeDenied, "连接票据无效或已过期", nil)
		return
	}
	if ticket.Protocol != remote.ProtocolRDP && ticket.Protocol != remote.ProtocolVNC {
		write(c, http.StatusForbidden, CodeDenied, "连接票据协议不匹配", nil)
		return
	}
	conn, err := rdpUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade 失败时已自行写入响应，这里只记录。
		slog.Error("升级远程桌面 WebSocket 失败", "agent_id", ticket.AgentID, "error", err)
		return
	}
	h.remote.ServeRemoteDesktop(conn, ticket, ClientIP(c), remote.DisplayHint{
		Width:  c.Query("width"),
		Height: c.Query("height"),
		DPI:    c.Query("dpi"),
	})
}

// RemoteSessions 处理 GET /api/v1/monitor/rdp/sessions：远程桌面（RDP / VNC）会话审计列表。
//
// 支持按协议、操作人、资产与开始时间范围筛选，默认按开始时间倒序。
func (h *RemoteAPI) RemoteSessions(c *gin.Context) {
	page, size := PageParams(c)
	startFrom, startTo := util.ParseTimePair(c.QueryArray("startedAt"))
	rows, total, err := h.remote.ListRemoteSessions(remote.RemoteSessionQuery{
		Protocol:  c.Query("protocol"),
		Operator:  c.Query("operator"),
		AgentID:   c.Query("agentId"),
		StartFrom: startFrom,
		StartTo:   startTo,
		Page:      page,
		Size:      size,
	})
	if err != nil {
		Fail(c, err, "查询远程会话失败")
		return
	}
	OK(c, gin.H{"list": rows, "total": total})
}

// RDPRecording 处理 GET /api/v1/monitor/rdp/sessions/:id/recording：下载会话录像。
//
// 录像是指令流（.guac），前端用与实时会话同一份客户端库回放。这里用 http.ServeContent
// 流式返回并天然支持 Range 请求（回放拖动进度条需要），避免一次性读进内存。
// 路径解析与目录穿越校验由 Service.ResolveRecording 负责。
func (h *RemoteAPI) RDPRecording(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		BadRequest(c, "会话 ID 无效")
		return
	}
	session, err := h.remote.GetRemoteSession(uint(id))
	if err != nil {
		Fail(c, err, "查询会话失败")
		return
	}
	if session.RecordingPath == "" {
		Fail(c, errors.New("该会话没有录像"), "该会话没有录像")
		return
	}
	file, info, err := h.remote.OpenRecording(session.RecordingPath)
	if err != nil {
		Fail(c, err, "读取录像失败")
		return
	}
	defer func() { _ = file.Close() }()

	filename := strconv.FormatUint(uint64(session.ID), 10) + ".guac"
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", "application/octet-stream")
	http.ServeContent(c.Writer, c.Request, filename, info.ModTime(), file)
}

// ListSessions 处理 GET /api/v1/monitor/audit/sessions：会话审计列表。
//
// 支持按操作人、登录用户、目标机、来源 IP、状态与开始时间范围筛选，默认按开始时间倒序。
// 参数命名与远程桌面会话审计保持一致（startedAt 用「起,止」两段式），前端两页可共用同一套写法。
func (h *RemoteAPI) ListSessions(c *gin.Context) {
	page, size := PageParams(c)
	status, err := remote.ParseSessionStatus(c.Query("status"))
	if err != nil {
		BadRequest(c, err.Error())
		return
	}
	startFrom, startTo := util.ParseTimePair(c.QueryArray("startedAt"))
	rows, total, err := h.remote.ListSessions(remote.SessionQuery{
		Operator:  c.Query("operator"),
		Username:  c.Query("username"),
		AgentID:   c.Query("agentId"),
		ClientIP:  c.Query("clientIp"),
		Status:    status,
		StartFrom: startFrom,
		StartTo:   startTo,
		Page:      page,
		Size:      size,
	})
	if err != nil {
		Fail(c, err, "查询会话记录失败")
		return
	}
	OK(c, gin.H{"list": rows, "total": total})
}

// SessionCommands 处理 GET /api/v1/monitor/audit/sessions/:id/commands：命令流。
func (h *RemoteAPI) SessionCommands(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		BadRequest(c, "会话 ID 无效")
		return
	}
	rows, err := h.remote.SessionCommands(uint(id))
	if err != nil {
		Fail(c, err, "查询命令记录失败")
		return
	}
	OK(c, rows)
}

// Recording 处理 GET /api/v1/monitor/audit/sessions/:id/recording：返回 asciinema 录像内容。
func (h *RemoteAPI) Recording(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		BadRequest(c, "会话 ID 无效")
		return
	}
	var session remote.SSHSession
	if err := h.remote.GetSession(uint(id), &session); err != nil {
		Fail(c, err, "查询会话记录失败")
		return
	}
	if session.RecordingPath == "" {
		NotFound(c, "该会话没有录像")
		return
	}
	if strings.Contains(session.RecordingPath, "..") {
		NotFound(c, "录像路径无效")
		return
	}
	path := h.remote.AbsRecordingPath(session.RecordingPath)
	data, err := os.ReadFile(path)
	if err != nil {
		// fallback：兼容早期保存在工作目录根部的录像
		fallback := filepath.Join(".", session.RecordingPath)
		data, err = os.ReadFile(fallback)
		if err != nil {
			NotFound(c, "录像文件不存在")
			return
		}
	}
	c.Data(http.StatusOK, "application/x-asciicast; charset=utf-8", data)
}

// SSHWebSocket 处理 GET /api/v1/monitor/ssh/ws：SSH 终端 WebSocket（ticket 鉴权，不挂登录组）。
//
// 处理顺序有意设计为「先升级、再建 SSH 会话」，让两类故障能被清晰区分：
//   - 握手前就能判定的失败（票据非法、凭据读取失败、会话记录写库失败）返回 JSON，
//     浏览器报「WebSocket handshake 失败」，指向鉴权或网关问题；
//   - 只有升级成功后才能发现的失败（SSH 拨号、认证、录像文件创建）通过 WebSocket 文本
//     推送到终端里显示——浏览器 WebSocket API 拿不到握手阶段的响应体，这是唯一能让
//     用户看到真实原因的途径，同时写一条 error 日志到 logs/app.log。
func (h *RemoteAPI) SSHWebSocket(c *gin.Context) {
	ticket, ok := h.remote.ConsumeTicket(c.Query("ticket"))
	if !ok {
		write(c, http.StatusForbidden, CodeDenied, "连接票据无效或已过期", nil)
		return
	}
	if ticket.Protocol != remote.ProtocolSSH {
		write(c, http.StatusForbidden, CodeDenied, "连接票据协议不匹配", nil)
		return
	}
	clientIP := ClientIP(c)
	host, port, username, authType, secret, passphrase, err := h.remote.SSHCredentials(ticket.AgentID)
	if err != nil {
		slog.Error("ssh 终端读取连接凭据失败",
			"agent_id", ticket.AgentID, "operator", ticket.Operator, "client_ip", clientIP, "err", err)
		Fail(c, err, "读取连接凭据失败")
		return
	}

	session := &remote.SSHSession{
		AgentID:   ticket.AgentID,
		Operator:  ticket.Operator,
		Username:  username,
		ClientIP:  clientIP,
		Status:    remote.SessionActive,
		StartedAt: time.Now(),
	}
	if err := h.remote.CreateSession(session); err != nil {
		slog.Error("ssh 终端创建会话记录失败",
			"agent_id", ticket.AgentID, "operator", ticket.Operator, "err", err)
		Fail(c, err, "创建会话记录失败")
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// gorilla 在升级失败时已写出 HTTP 错误响应（多为 400 not a websocket handshake），
		// 最常见原因是反向代理没有透传 Upgrade / Connection 头，此处不再重复写响应。
		_ = h.remote.FinishSession(session.ID, remote.SessionFailed, "WebSocket 升级失败: "+err.Error(), 0, 0)
		slog.Error("ssh 终端 WebSocket 升级失败：请检查反向代理是否透传 Upgrade/Connection 头",
			"agent_id", ticket.AgentID, "operator", ticket.Operator, "client_ip", clientIP, "err", err)
		return
	}
	defer conn.Close()

	terminal, err := h.remote.OpenTerminal(session.ID, host, port, username, authType, secret, passphrase, 80, 24)
	if err != nil {
		_ = h.remote.FinishSession(session.ID, remote.SessionFailed, err.Error(), 0, 0)
		slog.Error("ssh 终端会话建立失败",
			"session_id", session.ID, "agent_id", ticket.AgentID, "operator", ticket.Operator,
			"host", host, "port", port, "username", username, "auth_type", authType, "err", err)
		writeTerminalFailure(conn, err.Error())
		return
	}
	_ = h.remote.SetSessionRecording(session.ID, h.remote.RecordingPath(session.ID))

	var bytesIn, bytesOut int64
	outDone := make(chan struct{})
	go func() {
		defer close(outDone)
		buf := make([]byte, 32*1024)
		for {
			n, readErr := terminal.Output().Read(buf)
			if n > 0 {
				if writeErr := conn.WriteMessage(websocket.TextMessage, buf[:n]); writeErr != nil {
					return
				}
				terminal.RecordOutput(buf[:n])
				bytesOut += int64(n)
			}
			if readErr != nil {
				return
			}
		}
	}()

	closeReason := "正常断开"
	var inputBuf []byte
	for {
		messageType, data, readErr := conn.ReadMessage()
		if readErr != nil {
			break
		}
		if messageType == websocket.TextMessage {
			var resize struct {
				Type string `json:"type"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if json.Unmarshal(data, &resize) == nil && resize.Type == "resize" && resize.Cols > 0 && resize.Rows > 0 {
				_ = terminal.Resize(resize.Cols, resize.Rows)
				continue
			}
		}

		// 高危命令拦截：对文本输入做行缓冲检测。
		var blocked bool
		if messageType == websocket.TextMessage {
			inputBuf = append(inputBuf, data...)
			for {
				idx := bytes.IndexAny(inputBuf, "\r\n")
				if idx < 0 {
					break
				}
				line := string(inputBuf[:idx])
				inputBuf = inputBuf[idx+1:]
				if line == "" {
					continue
				}
				result := h.remote.CheckDangerousCommand(line)
				if result.Matched {
					warn := "\r\n\x1b[31m[高危命令拦截] 该命令已被禁止执行（规则：" + result.Name + "）\x1b[0m\r\n"
					_ = conn.WriteMessage(websocket.TextMessage, []byte(warn))
					// 把警告也写入录像，便于审计回溯。
					terminal.RecordOutput([]byte(warn))
					blocked = true
					continue
				}
			}
		}
		if blocked {
			if err := terminal.CancelPendingInput(); err != nil {
				closeReason = "SSH 会话已结束"
				break
			}
			continue
		}

		if writeErr := terminal.WriteInput(data); writeErr != nil {
			closeReason = "SSH 会话已结束"
			break
		}
		bytesIn += int64(len(data))
	}
	terminal.Close()
	<-outDone
	_ = h.remote.FinishSession(session.ID, remote.SessionClosed, closeReason, bytesIn, bytesOut)
}

// writeTerminalFailure 把 SSH 会话建立失败的原因推送到终端页面。
//
// 为什么用 WS 文本而不是 HTTP 响应：WebSocket 握手一旦成功，浏览器侧就无法再读取
// HTTP 响应体；反过来，握手阶段的失败又拿不到状态码。所以「升级后」的错误必须走
// 消息通道，用户才能在终端里直接看到原因，而不是只看到「连接已关闭」。
//
// 参数 Parameters:
//   - conn (*websocket.Conn): 已升级的 WebSocket 连接。
//   - reason (string): 失败原因（底层错误信息）。
func writeTerminalFailure(conn *websocket.Conn, reason string) {
	if len(reason) > terminalFailureMaxReasonLength {
		reason = reason[:terminalFailureMaxReasonLength] + "..."
	}
	text := "\r\n\x1b[31m[连接失败] " + reason + "\x1b[0m\r\n" +
		"\x1b[33m排查方向：1) 目标机 sshd 是否可连（地址与端口是否正确、防火墙是否放行）；" +
		"2) 账号密码或私钥是否正确（私钥需为 PEM 或 OpenSSH 格式，加密私钥要填口令）；" +
		"3) 后端 logs/app.log 中搜「ssh 终端会话建立失败」查看完整错误。\x1b[0m\r\n"
	_ = conn.WriteMessage(websocket.TextMessage, []byte(text))
}

// AgentProcesses 处理 GET /api/v1/monitor/agents/:id/processes：查询主机进程列表。
//
// 走独立 SSH 连接（每次请求建连、用完即关），与终端会话无关——终端连接是
// WebSocket 请求私有的，无法被其它请求复用（详见 internal/remote/process.go）。
//
// 结果在服务端有短 TTL 缓存（见 internal/remote/listcache.go）。带 refresh=1 可跳过
// 缓存强制重新采集，供界面的「刷新」按钮使用；普通打开不带该参数，以便快速重开时
// 不必再付一次 SSH 握手 + ps。
func (h *RemoteAPI) AgentProcesses(c *gin.Context) {
	agentID := c.Param("id")
	processes, err := h.remote.ListProcesses(agentID, c.Query("refresh") == "1")
	if err != nil {
		slog.Error("查询进程列表失败",
			"agent_id", agentID, "operator", operatorName(c), "client_ip", ClientIP(c), "err", err)
		Fail(c, err, "获取进程列表失败: "+err.Error())
		return
	}
	OK(c, processes)
}

// KillAgentProcess 处理 POST /api/v1/monitor/agents/:id/processes/kill：结束远程进程。
//
// 高危操作，三重约束：需要独立的权限标识（不复用 monitor:ssh:connect，避免所有能开终端的人
// 都能杀进程）；进程号必须是整数（命令拼接因此没有注入面）且 PID 1/2 被显式拒绝；
// 请求会被操作日志中间件自动审计（非 GET 请求）。
func (h *RemoteAPI) KillAgentProcess(c *gin.Context) {
	agentID := c.Param("id")
	var payload struct {
		// PID 进程号。
		PID int `json:"pid"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		BadRequest(c, "请求参数格式错误")
		return
	}
	if err := h.remote.KillProcess(agentID, payload.PID); err != nil {
		slog.Error("结束远程进程失败",
			"agent_id", agentID, "pid", payload.PID, "operator", operatorName(c),
			"client_ip", ClientIP(c), "err", err)
		Fail(c, err, "结束进程失败: "+err.Error())
		return
	}
	slog.Info("已结束远程进程",
		"agent_id", agentID, "pid", payload.PID, "operator", operatorName(c), "client_ip", ClientIP(c))
	OK(c, nil)
}

// RestartAgentProcess 处理 POST /api/v1/monitor/agents/:id/processes/restart：重启远程进程。
//
// 只支持由 systemd 托管的进程：后端通过 /proc/<pid>/cgroup 反查 unit 后执行
// systemctl restart。不受 systemd 托管时返回明确原因，而不是"假装重启"。
// 与结束进程同属高危操作，使用独立权限标识（重启同样会终止目标进程）。
func (h *RemoteAPI) RestartAgentProcess(c *gin.Context) {
	agentID := c.Param("id")
	var payload struct {
		// PID 进程号。
		PID int `json:"pid"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		BadRequest(c, "请求参数格式错误")
		return
	}
	result, err := h.remote.RestartProcess(agentID, payload.PID)
	if err != nil {
		slog.Error("重启远程进程失败",
			"agent_id", agentID, "pid", payload.PID, "operator", operatorName(c),
			"client_ip", ClientIP(c), "err", err)
		Fail(c, err, "重启进程失败: "+err.Error())
		return
	}
	slog.Info("已重启远程进程",
		"agent_id", agentID, "unit", result.Unit, "previous_pid", payload.PID,
		"main_pid", result.MainPID, "operator", operatorName(c), "client_ip", ClientIP(c))
	// 重启后主进程号理应变化。没变说明这次 restart 没有真正换掉进程，
	// 单独记一条警告——否则「提示成功、进程还是老 PID」会成为一个查不出原因的怪现象。
	if result.MainPID > 0 && result.MainPID == payload.PID {
		slog.Warn("重启后主进程号未变化，可能未真正重启",
			"agent_id", agentID, "unit", result.Unit, "pid", payload.PID)
	}
	OK(c, nil)
}

// AgentClock 处理 GET /api/v1/monitor/agents/:id/clock：查询主机时区、时钟与机器标识。
//
// 供 SSH 控制台左侧栏显示「时区 / 时钟 / 时钟偏移 / 发行版 / 架构」。其中发行版与架构
// 只能从目标机取——agents 表里只有内核串（"Linux 5.15.0-139-generic"）与 Go 的 GOARCH，
// 而界面要的是「Ubuntu 20.04.3 LTS / x86_64」。这两项顺路搭在同一条命令上返回，
// 不为它们单独再开一个接口（那等于多付一次 SSH 握手）。
//
// 与进程列表同样是独立 SSH 连接（每次请求建连、用完即关）——终端会话持有的连接是
// WebSocket 请求私有的，无法复用。
//
// 前端不要高频轮询本接口：每次请求都要付一次 SSH 握手，这些信息的变化又都很慢，
// 打开时取一次、之后低频刷新即可。
func (h *RemoteAPI) AgentClock(c *gin.Context) {
	agentID := c.Param("id")
	clock, err := h.remote.HostClock(agentID)
	if err != nil {
		slog.Error("查询主机时钟失败",
			"agent_id", agentID, "operator", operatorName(c), "client_ip", ClientIP(c), "err", err)
		Fail(c, err, "获取主机时钟失败: "+err.Error())
		return
	}
	OK(c, clock)
}

// AgentPorts 处理 GET /api/v1/monitor/agents/:id/ports：查询主机监听端口。
//
// 供 SSH 控制台右上角的「系统端口」抽屉展示。与进程列表同样是独立 SSH 连接
// （每次请求建连、用完即关），因此界面按需刷新即可，不要轮询。
func (h *RemoteAPI) AgentPorts(c *gin.Context) {
	agentID := c.Param("id")
	ports, err := h.remote.ListeningPorts(agentID)
	if err != nil {
		slog.Error("查询监听端口失败",
			"agent_id", agentID, "operator", operatorName(c), "client_ip", ClientIP(c), "err", err)
		Fail(c, err, "获取端口列表失败: "+err.Error())
		return
	}
	OK(c, ports)
}

// AgentServices 处理 GET /api/v1/monitor/agents/:id/services：查询主机 systemd 服务清单。
//
// 返回项里带 protected / blockReason：由服务端判定哪些服务禁止操作，界面据此禁用入口。
// 判定放在服务端而不是前端，是为了让「界面能不能点」与「请求是否放行」共用同一份规则，
// 不存在绕过前端就绕过限制的可能。
// 结果在服务端有短 TTL 缓存（见 internal/remote/listcache.go）：这条查询要跑两条
// systemctl，是 SSH 控制台里最慢的一次采集。带 refresh=1 可跳过缓存强制重新采集。
func (h *RemoteAPI) AgentServices(c *gin.Context) {
	agentID := c.Param("id")
	units, err := h.remote.ServiceUnits(agentID, c.Query("refresh") == "1")
	if err != nil {
		slog.Error("查询服务清单失败",
			"agent_id", agentID, "operator", operatorName(c), "client_ip", ClientIP(c), "err", err)
		Fail(c, err, "获取服务列表失败: "+err.Error())
		return
	}
	OK(c, units)
}

// OperateAgentService 处理 POST /api/v1/monitor/agents/:id/services/operate：启停/重启/设置自启。
//
// 高危操作，三重约束（与「结束进程」同一思路）：
//  1. 独立权限标识 monitor:ssh:service:manage，不复用 monitor:ssh:connect——
//     能打开终端不等于应该能停服务；
//  2. 服务端硬拦截：ssh / systemd / dbus / 网络 / 防火墙 / 审计类单元一律拒绝。
//     这些操作可能让主机失联或失去防护，而按钮按下去就没有补救机会；
//  3. 非 GET 请求由操作日志中间件自动审计，留痕到人。
func (h *RemoteAPI) OperateAgentService(c *gin.Context) {
	agentID := c.Param("id")
	var payload struct {
		// Unit 服务单元名，形如 ssh.service。
		Unit string `json:"unit"`
		// Action 操作：start / stop / restart / enable / disable。
		Action string `json:"action"`
	}
	if !BindJSON(c, &payload) {
		return
	}
	if strings.TrimSpace(payload.Unit) == "" || strings.TrimSpace(payload.Action) == "" {
		BadRequest(c, "unit 与 action 均不能为空")
		return
	}
	if err := h.remote.OperateService(agentID, payload.Unit, payload.Action); err != nil {
		slog.Error("服务操作失败",
			"agent_id", agentID, "unit", payload.Unit, "action", payload.Action,
			"operator", operatorName(c), "client_ip", ClientIP(c), "err", err)
		Fail(c, err, "服务操作失败: "+err.Error())
		return
	}
	slog.Info("已执行服务操作",
		"agent_id", agentID, "unit", payload.Unit, "action", payload.Action,
		"operator", operatorName(c), "client_ip", ClientIP(c))
	OK(c, nil)
}

// AgentServiceDetail 处理 GET /api/v1/monitor/agents/:id/services/detail：查询服务详情。
//
// 供系统服务列表点击行后展开的二级抽屉使用。与其它 SSH 类接口相同：每次请求独立建连。
func (h *RemoteAPI) AgentServiceDetail(c *gin.Context) {
	agentID := c.Param("id")
	unit := strings.TrimSpace(c.Query("unit"))
	if unit == "" {
		BadRequest(c, "unit 不能为空")
		return
	}
	detail, err := h.remote.ServiceDetail(agentID, unit)
	if err != nil {
		slog.Error("查询服务详情失败",
			"agent_id", agentID, "unit", unit, "operator", operatorName(c), "client_ip", ClientIP(c), "err", err)
		Fail(c, err, "获取服务详情失败: "+err.Error())
		return
	}
	OK(c, detail)
}

// AgentServiceLogs 处理 GET /api/v1/monitor/agents/:id/services/logs：查询服务日志。
//
// lines 由前端选择；解析失败时传 0，由 remote.ServiceLogs 落到默认值并夹紧上限，
// 避免默认值散落在「接口层」与「实现层」两处、日后改一处漏一处。
func (h *RemoteAPI) AgentServiceLogs(c *gin.Context) {
	agentID := c.Param("id")
	unit := strings.TrimSpace(c.Query("unit"))
	if unit == "" {
		BadRequest(c, "unit 不能为空")
		return
	}
	lines, _ := strconv.Atoi(strings.TrimSpace(c.Query("lines")))

	logs, err := h.remote.ServiceLogs(agentID, unit, lines)
	if err != nil {
		slog.Error("查询服务日志失败",
			"agent_id", agentID, "unit", unit, "lines", lines,
			"operator", operatorName(c), "client_ip", ClientIP(c), "err", err)
		Fail(c, err, "获取服务日志失败: "+err.Error())
		return
	}
	OK(c, logs)
}

// AgentProcessDetail 处理 GET /api/v1/monitor/agents/:id/processes/detail：查询进程详情。
//
// 供进程列表点击行后展开的二级抽屉使用，数据取自 /proc/<pid>（仅 Linux）。
func (h *RemoteAPI) AgentProcessDetail(c *gin.Context) {
	agentID := c.Param("id")
	pid, err := strconv.Atoi(strings.TrimSpace(c.Query("pid")))
	if err != nil || pid <= 0 {
		BadRequest(c, "pid 必须是正整数")
		return
	}
	detail, err := h.remote.ProcessDetail(agentID, pid)
	if err != nil {
		slog.Error("查询进程详情失败",
			"agent_id", agentID, "pid", pid, "operator", operatorName(c), "client_ip", ClientIP(c), "err", err)
		Fail(c, err, "获取进程详情失败: "+err.Error())
		return
	}
	OK(c, detail)
}

// SFTPLs 处理 GET /api/v1/monitor/sftp/ls：列出远程目录内容。
func (h *RemoteAPI) SFTPLs(c *gin.Context) {
	agentID := c.Query("agentId")
	if agentID == "" {
		BadRequest(c, "agentId 不能为空")
		return
	}
	dir := c.Query("path")
	if dir == "" {
		dir = "/"
	}
	items, err := h.remote.SFTPListDir(agentID, dir)
	if err != nil {
		slog.Error("sftp list dir failed",
			"agent_id", agentID, "path", dir, "operator", operatorName(c), "err", err)
		Fail(c, err, "获取文件列表失败: "+err.Error())
		return
	}
	OK(c, items)
}

// SFTPUpload 处理 POST /api/v1/monitor/sftp/upload：上传文件到远程服务器。
func (h *RemoteAPI) SFTPUpload(c *gin.Context) {
	agentID := c.PostForm("agentId")
	if agentID == "" {
		BadRequest(c, "agentId 不能为空")
		return
	}
	remotePath := c.PostForm("path")
	if remotePath == "" {
		BadRequest(c, "path 不能为空")
		return
	}
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		BadRequest(c, "缺少上传文件")
		return
	}
	defer file.Close()

	host, port, username, authType, secret, passphrase, err := h.remote.SSHCredentials(agentID)
	if err != nil {
		slog.Error("sftp upload read credentials failed", "agent_id", agentID, "path", remotePath, "err", err)
		Fail(c, err, "读取连接凭据失败")
		return
	}
	client, err := remote.DialSFTP(host, port, username, authType, secret, passphrase)
	if err != nil {
		slog.Error("sftp upload dial failed", "agent_id", agentID, "path", remotePath, "err", err)
		Fail(c, err, "SFTP 连接失败")
		return
	}
	defer client.Close()
	if err := client.UploadFile(remotePath, file); err != nil {
		slog.Error("sftp upload failed", "agent_id", agentID, "path", remotePath, "err", err)
		Fail(c, err, "文件上传失败")
		return
	}
	OK(c, nil)
}

// SFTPDownload 处理 GET /api/v1/monitor/sftp/download：从远程服务器下载文件。
func (h *RemoteAPI) SFTPDownload(c *gin.Context) {
	agentID := c.Query("agentId")
	if agentID == "" {
		BadRequest(c, "agentId 不能为空")
		return
	}
	remotePath := c.Query("path")
	if remotePath == "" {
		BadRequest(c, "path 不能为空")
		return
	}
	if strings.Contains(remotePath, "..") {
		BadRequest(c, "路径包含非法字符")
		return
	}

	host, port, username, authType, secret, passphrase, err := h.remote.SSHCredentials(agentID)
	if err != nil {
		slog.Error("sftp download read credentials failed", "agent_id", agentID, "path", remotePath, "err", err)
		Fail(c, err, "读取连接凭据失败")
		return
	}
	client, err := remote.DialSFTP(host, port, username, authType, secret, passphrase)
	if err != nil {
		slog.Error("sftp download dial failed", "agent_id", agentID, "path", remotePath, "err", err)
		Fail(c, err, "SFTP 连接失败")
		return
	}
	defer client.Close()

	c.Header("Content-Disposition", "attachment; filename=\""+filepath.Base(remotePath)+"\"")
	c.Header("Content-Type", "application/octet-stream")
	if err := client.DownloadFile(remotePath, c.Writer); err != nil {
		// gin 已写入部分数据时无法改状态码，只能记录日志。
		slog.Error("sftp download failed", "agent_id", agentID, "path", remotePath, "err", err)
		return
	}
}

// SFTPRename 处理 POST /api/v1/monitor/sftp/rename：重命名远程文件或目录。
func (h *RemoteAPI) SFTPRename(c *gin.Context) {
	var body struct {
		AgentID string `json:"agentId"`
		OldPath string `json:"oldPath"`
		NewPath string `json:"newPath"`
	}
	if !BindJSON(c, &body) {
		return
	}
	if body.AgentID == "" || body.OldPath == "" || body.NewPath == "" {
		BadRequest(c, "agentId、oldPath、newPath 均不能为空")
		return
	}
	if err := h.remote.SFTPRename(body.AgentID, body.OldPath, body.NewPath); err != nil {
		slog.Error("sftp rename failed", "agent_id", body.AgentID, "old", body.OldPath, "new", body.NewPath, "err", err)
		Fail(c, err, "重命名失败")
		return
	}
	OK(c, nil)
}

// SFTPDelete 处理 POST /api/v1/monitor/sftp/delete：删除远程文件或空目录。
func (h *RemoteAPI) SFTPDelete(c *gin.Context) {
	var body struct {
		AgentID string `json:"agentId"`
		Path    string `json:"path"`
	}
	if !BindJSON(c, &body) {
		return
	}
	if body.AgentID == "" || body.Path == "" {
		BadRequest(c, "agentId、path 均不能为空")
		return
	}
	if strings.Contains(body.Path, "..") {
		BadRequest(c, "路径包含非法字符")
		return
	}
	if err := h.remote.SFTPRemove(body.AgentID, body.Path); err != nil {
		slog.Error("sftp delete failed", "agent_id", body.AgentID, "path", body.Path, "err", err)
		Fail(c, err, "删除失败: "+err.Error())
		return
	}
	OK(c, nil)
}

// SFTPRead 处理 GET /api/v1/monitor/sftp/read：读取远程文件文本内容。
func (h *RemoteAPI) SFTPRead(c *gin.Context) {
	agentID := c.Query("agentId")
	if agentID == "" {
		BadRequest(c, "agentId 不能为空")
		return
	}
	remotePath := c.Query("path")
	if remotePath == "" {
		BadRequest(c, "path 不能为空")
		return
	}
	if strings.Contains(remotePath, "..") {
		BadRequest(c, "路径包含非法字符")
		return
	}

	data, err := h.remote.SFTPRead(agentID, remotePath)
	if err != nil {
		slog.Error("sftp read failed",
			"agent_id", agentID, "path", remotePath, "operator", operatorName(c), "err", err)
		Fail(c, err, "读取文件失败: "+err.Error())
		return
	}
	OK(c, gin.H{"content": string(data)})
}

// SFTPWrite 处理 POST /api/v1/monitor/sftp/write：将文本内容写回远程文件。
func (h *RemoteAPI) SFTPWrite(c *gin.Context) {
	var body struct {
		AgentID string `json:"agentId"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if !BindJSON(c, &body) {
		return
	}
	if body.AgentID == "" || body.Path == "" {
		BadRequest(c, "agentId、path 均不能为空")
		return
	}
	if strings.Contains(body.Path, "..") {
		BadRequest(c, "路径包含非法字符")
		return
	}

	if err := h.remote.SFTPWrite(body.AgentID, body.Path, []byte(body.Content)); err != nil {
		slog.Error("sftp write failed",
			"agent_id", body.AgentID, "path", body.Path, "operator", operatorName(c), "err", err)
		Fail(c, err, "写入文件失败: "+err.Error())
		return
	}
	OK(c, nil)
}

// operatorName 返回当前登录用户的账号（未登录时为空）。
func operatorName(c *gin.Context) string {
	operator := CurrentUser(c)
	if operator == nil {
		return ""
	}
	return operator.Username
}
