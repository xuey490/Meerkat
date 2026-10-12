package remote

import (
	"bytes"
	"net"
	"testing"
	"time"
)

// TestGuacdHandshakeDiagnostic 逐步发送握手指令，定位 guacd 从哪一步开始响应。
//
// 这是一个诊断用例，不是常规回归测试：它需要本机 guacd 在 4822 监听，
// 且每一步都会等待并打印网关的原始应答。
//
// 用法：
//
//	go test ./internal/remote -run TestGuacdHandshakeDiagnostic -v
//
// 为什么需要它：正常握手在「select 之后应立刻收到 args」。如果这一步没有响应，
// 说明问题在 guacd 侧（插件加载、版本、绑定地址），而不是我们的编解码；
// 若后续步骤才有响应，则说明握手顺序需要调整。
func TestGuacdHandshakeDiagnostic(t *testing.T) {
	const address = "127.0.0.1:4822"

	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		t.Skipf("guacd 未在 %s 监听，跳过诊断: %v", address, err)
	}
	defer conn.Close()
	t.Logf("已连接 %s", address)

	// send 发送一条指令并尝试读取应答；无论是否有数据都会记录。
	send := func(opcode string, args []string) {
		var buf bytes.Buffer
		if err := writeInstruction(&buf, opcode, args...); err != nil {
			t.Fatalf("构造 %s 指令失败: %v", opcode, err)
		}
		if _, err := conn.Write(buf.Bytes()); err != nil {
			t.Fatalf("发送 %s 失败: %v", opcode, err)
		}
		t.Logf("→ 发送: %s", buf.String())

		time.Sleep(1500 * time.Millisecond)
		_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		reply := make([]byte, 8192)
		n, readErr := conn.Read(reply)
		if readErr != nil {
			t.Logf("← 无应答: %v", readErr)
			return
		}
		t.Logf("← 收到 %d 字节: %q", n, string(reply[:n]))
	}

	// 按 Guacamole 协议的握手顺序逐步推进，观察哪一步开始有回应。
	send(opSelect, []string{ProtocolRDP})
	send(opSize, []string{"1024", "768"})
	send(opAudio, nil)
	send(opImage, nil)
	send(opConnect, []string{
		"hostname", "127.0.0.1",
		"port", "3389",
		"username", "diagnostic",
		"password", "diagnostic",
		"width", "1024",
		"height", "768",
	})

	t.Log("诊断完成：请对照上方日志判断 guacd 从哪一步开始响应")
}
