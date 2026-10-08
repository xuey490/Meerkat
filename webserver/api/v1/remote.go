package v1

import (
	"bytes"
	"encoding/json"
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
	group.GET("/monitor/audit/sessions", guard("monitor:audit:view"), h.ListSessions)
	group.GET("/monitor/audit/sessions/:id/commands", guard("monitor:audit:view"), h.SessionCommands)
	group.GET("/monitor/audit/sessions/:id/recording", guard("monitor:audit:view"), h.Recording)
}

// Endpoint 处理 GET /api/v1/monitor/agents/:id/endpoint：查询连接配置（不含明文）。
func (h *RemoteAPI) Endpoint(c *gin.Context) {
	view, err := h.remote.Endpoint(c.Param("id"))
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
func (h *RemoteAPI) DeleteEndpoint(c *gin.Context) {
	if err := h.remote.DeleteEndpoint(c.Param("id")); err != nil {
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

// RDPToken 处理 POST /api/v1/monitor/rdp/token：换取 Guacamole 一次性嵌入地址。
func (h *RemoteAPI) RDPToken(c *gin.Context) {
	var body struct {
		AgentID string `json:"agentId"`
	}
	if !BindJSON(c, &body) {
		return
	}
	url, err := h.remote.IssueRDPToken(body.AgentID, operatorName(c))
	if err != nil {
		Fail(c, err, "发起 RDP 连接失败")
		return
	}
	OK(c, gin.H{"url": url})
}

// ListSessions 处理 GET /api/v1/monitor/audit/sessions：会话审计列表。
func (h *RemoteAPI) ListSessions(c *gin.Context) {
	page, size := PageParams(c)
	rows, total, err := h.remote.ListSessions(page, size)
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

// operatorName 返回当前登录用户的账号（未登录时为空）。
func operatorName(c *gin.Context) string {
	operator := CurrentUser(c)
	if operator == nil {
		return ""
	}
	return operator.Username
}
