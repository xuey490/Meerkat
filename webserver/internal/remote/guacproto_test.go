package remote

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// TestInstructionRoundTrip 验证指令写入与解析互为逆运算。
func TestInstructionRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		opcode string
		args   []string
	}{
		{"select 协议", opSelect, []string{ProtocolRDP}},
		{"connect 参数对", opConnect, []string{"hostname", "10.0.0.1", "port", "3389"}},
		{"空参数", opReady, nil},
		{"含逗号的参数值", opError, []string{"a,b"}},
		{"含中文", opError, []string{"连接被目标机拒绝"}},
		{"含分号的参数值", opError, []string{"a;b"}},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		if err := writeInstruction(&buf, c.opcode, c.args...); err != nil {
			t.Fatalf("%s: writeInstruction 失败: %v", c.name, err)
		}
		// 协议要求以分号结尾。
		if !strings.HasSuffix(buf.String(), ";") {
			t.Errorf("%s: 指令未以分号结尾: %q", c.name, buf.String())
		}
		opcode, args, err := readInstruction(bufio.NewReader(&buf))
		if err != nil {
			t.Fatalf("%s: readInstruction 失败: %v", c.name, err)
		}
		if opcode != c.opcode {
			t.Errorf("%s: opcode=%q，期望 %q", c.name, opcode, c.opcode)
		}
		if len(args) != len(c.args) {
			t.Fatalf("%s: 参数个数=%d，期望 %d", c.name, len(args), len(c.args))
		}
		for i := range args {
			if args[i] != c.args[i] {
				t.Errorf("%s: 参数[%d]=%q，期望 %q", c.name, i, args[i], c.args[i])
			}
		}
	}
}

// TestWriteInstructionUsesByteLength 验证长度前缀是字节数而不是字符数。
//
// 这是协议正确性的关键：中文的 UTF-8 编码是 3 字节，若按字符数写长度，
// guacd 会读到错位的字节，表现为「连接莫名失败」。
func TestWriteInstructionUsesByteLength(t *testing.T) {
	var buf bytes.Buffer
	if err := writeInstruction(&buf, opError, "断开"); err != nil {
		t.Fatalf("writeInstruction 失败: %v", err)
	}
	// "断开" 是 6 字节。
	if !strings.Contains(buf.String(), "6.断开") {
		t.Errorf("长度前缀应为字节数 6，实际输出: %q", buf.String())
	}
}

// TestReadInstructionRejectsMalformed 验证畸形输入被拒绝且不会撑爆内存。
func TestReadInstructionRejectsMalformed(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"长度前缀为空", ".abc;"},
		{"长度前缀含字母", "1a.abc;"},
		{"长度前缀过长", "1234567890.abc;"},
		{"分隔符非法", "3.abc|3.def;"},
		{"内容短于声明长度", "99.abc;"},
	}
	for _, c := range cases {
		_, _, err := readInstruction(bufio.NewReader(strings.NewReader(c.input)))
		if err == nil {
			t.Errorf("%s: 应返回错误，实际通过了", c.name)
		}
	}
}

// TestReadInstructionRejectsHugeLength 验证超长声明被拦下（防内存膨胀）。
func TestReadInstructionRejectsHugeLength(t *testing.T) {
	// 声明 999999999 字节（约 1GB），应立即报错而不是分配这么大内存。
	_, _, err := readInstruction(bufio.NewReader(strings.NewReader("999999999.x;")))
	if err == nil {
		t.Fatal("超大长度前缀应被拒绝")
	}
	if !strings.Contains(err.Error(), "超出上限") {
		t.Errorf("错误信息应说明超限，实际: %v", err)
	}
}

// TestConnectArgsFromNames 验证 connect 与 args 等长，版本槽原样回写，其余按名取值。
func TestConnectArgsFromNames(t *testing.T) {
	authority := map[string]string{
		"hostname": "10.0.0.9",
		"port":     "3389",
		"username": "administrator",
		"password": "s3cret",
	}
	inject := map[string]string{
		"recording-path": "/mnt/c/monitor/guac-recordings/sessions/20261009",
		"recording-name": "7-1759999999.guac",
	}
	names := []string{"VERSION_1_5_0", "hostname", "port", "password", "recording-path", "read-only", "username"}
	got := connectArgsFromNames(names, authority, inject)
	want := []string{
		"VERSION_1_5_0",
		"10.0.0.9",
		"3389",
		"s3cret",
		"/mnt/c/monitor/guac-recordings/sessions/20261009",
		"",
		"administrator",
	}
	if len(got) != len(names) {
		t.Fatalf("connect 个数=%d，必须与 args 相同 %d", len(got), len(names))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("参数 %s = %q，期望 %q", names[i], got[i], want[i])
		}
	}
}

// TestBuildConnectPlanRDP 验证 RDP 的连接参数与录像参数构造。
func TestBuildConnectPlanRDP(t *testing.T) {
	client := NewGuacdClient("127.0.0.1:4822", 0, true, "/mnt/c/monitor/guac-recordings/sessions")
	endpoint := &RemoteEndpoint{
		AgentID:  "loca-windows",
		Protocol: ProtocolRDP,
		Host:     "192.168.1.20",
		Port:     3389,
		Username: `DESKTOP-TEST\administrator`,
	}
	now := time.Date(2026, 10, 9, 15, 30, 0, 0, time.UTC)
	plan := client.BuildConnectPlan(endpoint, "p@ss", 7, now)

	if plan.Authority["hostname"] != "192.168.1.20" || plan.Authority["port"] != "3389" {
		t.Errorf("地址与端口注入错误: %v", plan.Authority)
	}
	if plan.Authority["security"] != "nla" || plan.Authority["ignore-cert"] != "true" {
		t.Errorf("RDP 安全参数缺失: %v", plan.Authority)
	}
	if plan.Authority["domain"] != "DESKTOP-TEST" || plan.Authority["username"] != "administrator" {
		t.Errorf("RDP 域与用户名拆分错误: %v", plan.Authority)
	}
	wantRelative := fmt.Sprintf("20261009/7-%d.guac", now.Unix())
	if plan.RecordingRelative != wantRelative {
		t.Errorf("录像相对路径=%q，期望 %q", plan.RecordingRelative, wantRelative)
	}
	if plan.Inject["recording-path"] != "/mnt/c/monitor/guac-recordings/sessions/20261009" {
		t.Errorf("录像目录错误: %q", plan.Inject["recording-path"])
	}
}

// TestBuildConnectPlanVNCHasNoUsername 验证 VNC 不带 username 参数。
func TestBuildConnectPlanVNC(t *testing.T) {
	client := NewGuacdClient("127.0.0.1:4822", 0, true, "/var/lib/guacd/recordings")
	endpoint := &RemoteEndpoint{
		AgentID:  "loca-ubuntu16",
		Protocol: ProtocolVNC,
		Host:     "192.168.1.30",
		Port:     5900,
		Username: "root",
	}
	plan := client.BuildConnectPlan(endpoint, "vncpass", 9, time.Now())

	if _, ok := plan.Authority["username"]; ok {
		t.Error("VNC 不应注入 username 参数")
	}
	if plan.Authority["password"] != "vncpass" {
		t.Errorf("VNC 密码注入错误: %q", plan.Authority["password"])
	}
}

// TestBuildConnectPlanRecordingDisabled 验证关闭录像时不注入任何录像参数。
func TestBuildConnectPlanRecordingDisabled(t *testing.T) {
	client := NewGuacdClient("127.0.0.1:4822", 0, false, "/tmp/rec")
	endpoint := &RemoteEndpoint{Protocol: ProtocolRDP, Host: "h", Port: 3389}
	plan := client.BuildConnectPlan(endpoint, "p", 1, time.Now())

	if plan.Inject != nil || plan.RecordingRelative != "" {
		t.Errorf("关闭录像时不应产生录像参数: %+v", plan)
	}
}

// TestConnectPlanShieldsPassword 验证密码只来自服务端的连接配置。
func TestConnectPlanShieldsPassword(t *testing.T) {
	client := NewGuacdClient("127.0.0.1:4822", 0, true, "/tmp/rec")
	endpoint := &RemoteEndpoint{Protocol: ProtocolRDP, Host: "10.0.0.1", Port: 3389, Username: "admin"}
	plan := client.BuildConnectPlan(endpoint, "REAL-PASSWORD", 1, time.Now())

	got := connectArgsFromNames([]string{"VERSION_1_5_0", "hostname", "password"}, plan.Authority, plan.Inject)
	if len(got) != 3 || got[0] != "VERSION_1_5_0" || got[1] != "10.0.0.1" || got[2] != "REAL-PASSWORD" {
		t.Errorf("密码必须由服务端注入，实际 %#v", got)
	}
}

// TestHandshakeGuacdPositionalConnect 用一对管道假装 guacd，确认握手顺序和 connect 参数形态。
func TestHandshakeGuacdPositionalConnect(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(server)
		opcode, args, err := readInstruction(reader)
		if err != nil || opcode != opSelect || len(args) != 1 || args[0] != ProtocolVNC {
			errCh <- fmt.Errorf("select: opcode=%s args=%v err=%v", opcode, args, err)
			return
		}
		if err := writeInstruction(server, opArgs, "hostname", "port", "password"); err != nil {
			errCh <- err
			return
		}
		for {
			opcode, args, err = readInstruction(reader)
			if err != nil {
				errCh <- err
				return
			}
			if opcode != opConnect {
				continue
			}
			if len(args) != 3 || args[0] != "192.168.1.18" || args[1] != "5900" || args[2] != "secret" {
				errCh <- fmt.Errorf("connect 参数 = %#v", args)
				return
			}
			errCh <- writeInstruction(server, opReady, "1")
			return
		}
	}()

	gateway := &GuacdConn{conn: client, reader: bufio.NewReader(client)}
	err := handshakeGuacd(gateway, ProtocolVNC, DisplayHint{Width: "1024", Height: "768", DPI: "96"},
		map[string]string{"hostname": "192.168.1.18", "port": "5900", "password": "secret"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

// TestHandshakeGuacdStripsProtocolVersion 验证 1.5 的 args 版本前缀不会被当成连接参数。
func TestHandshakeGuacdStripsProtocolVersion(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(server)
		if _, _, err := readInstruction(reader); err != nil {
			errCh <- err
			return
		}
		if err := writeInstruction(server, opArgs, "VERSION_1_5_0", "hostname", "port", "password"); err != nil {
			errCh <- err
			return
		}
		for {
			opcode, args, err := readInstruction(reader)
			if err != nil {
				errCh <- err
				return
			}
			if opcode != opConnect {
				continue
			}
			if len(args) != 4 || args[0] != "VERSION_1_5_0" || args[1] != "127.0.0.1" || args[2] != "5902" || args[3] != "secret" {
				errCh <- fmt.Errorf("connect 参数 = %#v", args)
				return
			}
			errCh <- writeInstruction(server, opReady, "1")
			return
		}
	}()

	gateway := &GuacdConn{conn: client, reader: bufio.NewReader(client)}
	err := handshakeGuacd(gateway, ProtocolVNC, DisplayHint{},
		map[string]string{"hostname": "127.0.0.1", "port": "5902", "password": "secret"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

// TestGuacdHandshakeOptional 在 guacd 可达时做一次真实协议握手（不可达则跳过）。
//
// 它验证的不只是「端口通」：guacd 会用合法的 Guacamole 指令应答 select，
// 因此能证明协议编解码与本地 guacd 是匹配的。
func TestGuacdHandshakeOptional(t *testing.T) {
	address := "127.0.0.1:4822"
	if conn, err := net.DialTimeout("tcp", address, 500*time.Millisecond); err == nil {
		_ = conn.Close()
	} else {
		t.Skipf("guacd 未运行（%s），跳过握手测试: %v", address, err)
	}

	client := NewGuacdClient(address, 3*time.Second, false, "")
	conn, err := client.Dial(t.Context())
	if err != nil {
		t.Fatalf("拨号 guacd 失败: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteInstruction(opSelect, ProtocolRDP); err != nil {
		t.Fatalf("发送 select 指令失败: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	opcode, args, err := conn.ReadInstruction()
	if err != nil {
		t.Fatalf("读取 guacd 应答失败: %v", err)
	}
	if opcode != opArgs {
		t.Fatalf("guacd 应答 opcode=%q，期望 %q（参数 %v）", opcode, opArgs, args)
	}
	// args 指令携带 「参数名, 默认值...」，RDP 至少应包含 hostname 与 port。
	joined := strings.Join(args, ",")
	for _, want := range []string{"hostname", "port"} {
		if !strings.Contains(joined, want) {
			t.Errorf("guacd 应答缺少参数 %q：%v", want, args)
		}
	}
	t.Logf("guacd 握手成功，args 参数个数=%d", len(args))
}
