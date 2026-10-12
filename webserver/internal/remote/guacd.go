package remote

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"path"
	"strconv"
	"strings"
	"time"
)

// GuacdClient 是 guacd（Apache Guacamole 的协议代理守护进程）的连接工厂。
//
// 我们只使用 guacd，不部署 guacamole-client：
// 连接参数由本服务在服务端权威注入，前端只负责收发 Guacamole 指令与渲染画面。
type GuacdClient struct {
	// address guacd 地址，形如 127.0.0.1:4822。
	address string
	// dialTimeout 拨号超时。
	dialTimeout time.Duration
	// recordingEnabled 是否录制会话。
	recordingEnabled bool
	// recordingDirInGuacd 传给 guacd 的录像目录（guacd 所在文件系统的视角）。
	//
	// 开发机上 guacd 跑在 WSL2 内，该值是 /mnt/c/... 形态；生产上按容器/宿主实际路径配置。
	recordingDirInGuacd string
}

// NewGuacdClient 创建 guacd 连接工厂。
//
// 参数 Parameters:
//   - address (string): guacd 地址（host:port）。
//   - dialTimeout (time.Duration): 拨号超时；<=0 时取 5 秒。
//   - recordingEnabled (bool): 是否录制会话。
//   - recordingDirInGuacd (string): 传给 guacd 的录像目录。
//
// 返回 Returns:
//   - client (*GuacdClient): 连接工厂。
func NewGuacdClient(address string, dialTimeout time.Duration, recordingEnabled bool, recordingDirInGuacd string) *GuacdClient {
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}
	return &GuacdClient{
		address:             address,
		dialTimeout:         dialTimeout,
		recordingEnabled:    recordingEnabled,
		recordingDirInGuacd: recordingDirInGuacd,
	}
}

// Addr 返回 guacd 地址（用于日志）。
func (c *GuacdClient) Addr() string { return c.address }

// RecordingEnabled 返回是否开启会话录像。
func (c *GuacdClient) RecordingEnabled() bool { return c.recordingEnabled }

// Dial 与 guacd 建立 TCP 连接。握手由调用方驱动（见 guacproxy.go）。
//
// 参数 Parameters:
//   - ctx (context.Context): 用于取消拨号。
//
// 返回 Returns:
//   - conn (*GuacdConn): 已建立的连接。
//   - err (error): 拨号失败时返回非 nil。
func (c *GuacdClient) Dial(ctx context.Context) (*GuacdConn, error) {
	dialer := &net.Dialer{Timeout: c.dialTimeout}
	raw, err := dialer.DialContext(ctx, "tcp", c.address)
	if err != nil {
		return nil, fmt.Errorf("连接 guacd(%s) 失败: %w", c.address, err)
	}
	return &GuacdConn{conn: raw, reader: bufio.NewReaderSize(raw, 32<<10)}, nil
}

// GuacdConn 是一条与 guacd 的连接。
//
// 握手阶段用它收发 Guacamole 指令；握手完成后交给 RelayOnce/Relay 做纯字节中继。
type GuacdConn struct {
	// conn 底层连接。
	conn net.Conn
	// reader 带缓冲的读取端（指令解析需要）。
	reader *bufio.Reader
}

// ReadInstruction 读出一条 Guacamole 指令（握手期使用）。
func (c *GuacdConn) ReadInstruction() (string, []string, error) {
	return readInstruction(c.reader)
}

// WriteInstruction 写出一条 Guacamole 指令（握手期使用）。
func (c *GuacdConn) WriteInstruction(opcode string, args ...string) error {
	if err := writeInstruction(c.conn, opcode, args...); err != nil {
		return fmt.Errorf("向 guacd 写入 %s 指令失败: %w", opcode, err)
	}
	return nil
}

// Buffered 返回带缓冲的读取端（握手完成后中继直接从它读，避免丢失已缓冲的字节）。
func (c *GuacdConn) Buffered() *bufio.Reader { return c.reader }

// Write 原始写入（中继期使用）。
func (c *GuacdConn) Write(p []byte) (int, error) { return c.conn.Write(p) }

// Close 关闭连接。
func (c *GuacdConn) Close() error { return c.conn.Close() }

// SetReadDeadline 设置读取超时（中继期用于空闲回收）。
func (c *GuacdConn) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }

// SetWriteDeadline 设置写入超时。
func (c *GuacdConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

// ConnectPlan 是一次远程桌面连接的服务端注入方案。
//
// 它把「客户端能决定什么」压缩到零：连接参数由 Authority 按 guacd 给出的参数名填值，
// 录像参数由 Inject 填值（见 connectArgsFromNames）。浏览器不参与这条 connect。
type ConnectPlan struct {
	// Authority 权威连接参数，非空值覆盖客户端同名字段。
	Authority map[string]string
	// Inject 强制注入的参数（录像）。
	Inject map[string]string
	// InjectOrder Inject 的稳定输出顺序。
	InjectOrder []string
	// RecordingRelative 录像相对路径（相对录像根目录，以 / 分隔）；未开启录像时为空。
	RecordingRelative string
	// RecordingDirInGuacd 录像目录（guacd 视角的绝对路径）；未开启录像时为空。
	RecordingDirInGuacd string
	// RecordingName 录像文件名；未开启录像时为空。
	RecordingName string
}

// BuildConnectPlan 构造连接参数注入方案。
//
// 参数 Parameters:
//   - endpoint (*RemoteEndpoint): 已校验的连接配置。
//   - password (string): 已解密的密码。
//   - sessionID (uint): 会话主键，用于生成录像文件名。
//   - now (time.Time): 当前时间，用于生成日期子目录。
//
// 返回 Returns:
//   - plan (ConnectPlan): 注入方案。
func (c *GuacdClient) BuildConnectPlan(endpoint *RemoteEndpoint, password string, sessionID uint, now time.Time) ConnectPlan {
	authority := map[string]string{
		"hostname": endpoint.Host,
		"port":     strconv.Itoa(endpoint.Port),
	}
	switch endpoint.Protocol {
	case ProtocolRDP:
		// FreeRDP 3 与 guacamole-server 1.6.0 支持通过 CredSSP/NLA 认证。
		// resize-method 让浏览器改窗口大小时走显示更新，而不是拆掉整条 RDP。
		username := endpoint.Username
		// guacd/FreeRDP 的 domain 与 username 是两个独立参数。Windows 常用的
		// DOMAIN\user 写法若整串塞进 username，NLA 会在登录前直接断开。
		if domain, user, ok := strings.Cut(username, `\`); ok {
			authority["domain"] = domain
			username = user
		}
		authority["username"] = username
		authority["password"] = password
		authority["security"] = "rdp"//rdp 默认不支持 nla，需要手动开启
		authority["ignore-cert"] = "true"
		authority["resize-method"] = "display-update"
	case ProtocolVNC:
		// VNC 只有密码。color-depth=24 避免 8 位色；cursor=local 让浏览器画指针
		// （空桌面/黑屏时至少能看见鼠标在动，远端嵌入光标经常是全透明）。
		authority["password"] = password
		authority["color-depth"] = "24"
		authority["cursor"] = "local"
	}

	plan := ConnectPlan{Authority: authority}
	if !c.recordingEnabled || c.recordingDirInGuacd == "" {
		return plan
	}

	// 会话标识只用数字主键与时间戳，天然不含路径分隔符，不存在目录穿越风险。
	name := fmt.Sprintf("%d-%d.guac", sessionID, now.Unix())
	relative := path.Join(now.Format("20060102"), name)
	dir := path.Join(c.recordingDirInGuacd, now.Format("20060102"))

	plan.RecordingRelative = relative
	plan.RecordingDirInGuacd = dir
	plan.RecordingName = name
	plan.Inject = map[string]string{
		"recording":             "true",
		"recording-path":        dir,
		"recording-name":        name,
		"create-recording-path": "true",
	}
	// 固定顺序，便于测试与日志比对。
	plan.InjectOrder = []string{
		"recording",
		"recording-path",
		"recording-name",
		"create-recording-path",
	}
	return plan
}

// DisplayHint 是浏览器希望的初始画面尺寸，来自 WebSocket 查询参数。
// 非法或空值在握手时回落到 1280x720、96 DPI。
type DisplayHint struct {
	Width  string
	Height string
	DPI    string
}

// handshakeGuacd 由服务端完成与 guacd 的握手。
//
// guacamole-common-js 不发 select/args/connect——那是 Java 版 guacamole-client 的工作。
// 浏览器只等画面指令（sync/img/...）。所以 select、能力协商、connect 必须在这里做完，
// 密码和录像参数也只在这一步注入，不会进浏览器。
//
// 返回 Returns:
//   - err (error): 握手失败时返回非 nil，文案可直接给操作人看。
func handshakeGuacd(conn *GuacdConn, protocol string, hint DisplayHint, authority, inject map[string]string) error {
	if err := conn.WriteInstruction(opSelect, protocol); err != nil {
		return err
	}
	opcode, argNames, err := conn.ReadInstruction()
	if err != nil {
		return fmt.Errorf("读取网关参数列表失败: %w", err)
	}
	if opcode == opError {
		return fmt.Errorf("%s", strings.Join(argNames, " "))
	}
	if opcode != opArgs {
		return fmt.Errorf("协议握手异常：期望 %s 指令，收到 %s", opArgs, opcode)
	}

	width, height, dpi := normalizeDisplay(hint)
	// 顺序与官方 Java 客户端一致：size、audio、video、image、timezone，然后 connect。
	steps := []struct {
		opcode string
		args   []string
	}{
		{opSize, []string{width, height, dpi}},
		{opAudio, nil},
		{opVideo, nil},
		{opImage, []string{"image/png", "image/jpeg"}},
		{opTimezone, []string{"Asia/Shanghai"}},
	}
	for _, step := range steps {
		if err := conn.WriteInstruction(step.opcode, step.args...); err != nil {
			return err
		}
	}

	// connect 的值列表必须和 args 里的名字等长（含第一条 VERSION_*）。
	connectArgs := connectArgsFromNames(argNames, authority, inject)
	slog.Info("远程桌面握手",
		"protocol", protocol,
		"args", len(argNames),
		"connect", len(connectArgs),
		"hostname", authority["hostname"],
		"port", authority["port"])
	if err := conn.WriteInstruction(opConnect, connectArgs...); err != nil {
		return err
	}

	opcode, readyArgs, err := conn.ReadInstruction()
	if err != nil {
		return fmt.Errorf("等待网关就绪失败: %w", err)
	}
	if opcode == opError {
		return fmt.Errorf("%s", strings.Join(readyArgs, " "))
	}
	if opcode != opReady {
		return fmt.Errorf("协议握手异常：期望 %s 指令，收到 %s", opReady, opcode)
	}
	return nil
}

// normalizeDisplay 把查询参数收成 guacd size 指令能用的三个正整数。
func normalizeDisplay(hint DisplayHint) (width, height, dpi string) {
	width, height, dpi = "1280", "720", "96"
	if value := positiveDecimal(hint.Width); value != "" {
		width = value
	}
	if value := positiveDecimal(hint.Height); value != "" {
		height = value
	}
	if value := positiveDecimal(hint.DPI); value != "" {
		dpi = value
	}
	return width, height, dpi
}

// positiveDecimal 只接受正整数的十进制写法，其它一律当没填。
func positiveDecimal(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}
