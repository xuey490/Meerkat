package remote

import "testing"

// TestParseListeningPorts 用真实 `ss -lntupH` 输出验证端口解析。
//
// 覆盖几个容易踩的点：IPv6 地址（[::]:22）、通配地址（*:888）、
// 带作用域后缀的地址（127.0.0.53%lo:53）、以及同一进程多个 fd 的去重。
func TestParseListeningPorts(t *testing.T) {
	output := "udp   UNCONN 0      0          127.0.0.53%lo:53        0.0.0.0:*    users:((\"systemd-resolve\",pid=693,fd=13))\n" +
		"udp   UNCONN 0      0          0.0.0.0:5353            0.0.0.0:*    users:((\"avahi-daemon\",pid=743,fd=12))\n" +
		"tcp   LISTEN 0      128        0.0.0.0:22              0.0.0.0:*    users:((\"sshd\",pid=99113,fd=3),(\"sshd\",pid=99113,fd=9))\n" +
		"tcp   LISTEN 0      4096       *:888                   *:*          users:((\"httpd\",pid=1782,fd=4),(\"httpd\",pid=1781,fd=4),(\"httpd\",pid=1780,fd=4),(\"httpd\",pid=1778,fd=4))\n" +
		"tcp   LISTEN 0      128            [::]:22              [::]:*       users:((\"sshd\",pid=99113,fd=4))\n" +
		"tcp   LISTEN 0      70       127.0.0.1:3306             0.0.0.0:*    users:((\"mysqld\",pid=1745,fd=22))\n" +
		"tcp   LISTEN 0      128        0.0.0.0:8080            0.0.0.0:*\n" +
		"\n" +
		"这不是一行合法输出\n" +
		"tcp LISTEN 0\n"

	ports := ParseListeningPorts(output)
	if len(ports) != 7 {
		t.Fatalf("应解析出 7 条监听端口，实际 %d：%#v", len(ports), ports)
	}

	if ports[0].Protocol != "udp" || ports[0].Local != "127.0.0.53%lo:53" {
		t.Errorf("首条应为 udp 127.0.0.53%%lo:53，实际 %s %s", ports[0].Protocol, ports[0].Local)
	}
	if ports[0].Process != "693/systemd-resolve" {
		t.Errorf("进程应为 693/systemd-resolve，实际 %q", ports[0].Process)
	}

	// 同一 PID 的多个 fd 必须只保留一次。
	if ports[2].Process != "99113/sshd" {
		t.Errorf("同一 PID 多个 fd 应去重，实际 %q", ports[2].Process)
	}

	// SO_REUSEPORT：多个进程共享同一端口，按 ss 给出的顺序逗号拼接。
	if ports[3].Local != "*:888" {
		t.Errorf("通配地址应原样保留，实际 %q", ports[3].Local)
	}
	wantShared := "1782/httpd, 1781/httpd, 1780/httpd, 1778/httpd"
	if ports[3].Process != wantShared {
		t.Errorf("共享端口的进程列应为 %q，实际 %q", wantShared, ports[3].Process)
	}

	// IPv6：地址里的冒号不能让列切分错位。
	if ports[4].Local != "[::]:22" || ports[4].Protocol != "tcp" {
		t.Errorf("IPv6 监听解析异常：%#v", ports[4])
	}

	// 非 root 取不到进程信息：端口仍要返回，只是进程列为空。
	if ports[6].Local != "0.0.0.0:8080" || ports[6].Process != "" {
		t.Errorf("无进程信息时应保留端口且进程留空，实际 %#v", ports[6])
	}
}

// TestParseListeningPortsEmpty 验证空输出不会 panic 且返回空切片。
func TestParseListeningPortsEmpty(t *testing.T) {
	if ports := ParseListeningPorts(""); len(ports) != 0 {
		t.Errorf("空输出应得到空列表，实际 %#v", ports)
	}
	if ports := ParseListeningPorts("users:(())\n"); len(ports) != 0 {
		t.Errorf("无有效列的行应被跳过，实际 %#v", ports)
	}
}
