package remote

import (
	"strings"
	"testing"
)

// TestParseProcessList 用真实 ps 输出验证进程列表解析。
//
// 关键点是「命令行含空格」的行不能被拆碎：ps 的列之间是对齐用的连续空格，
// 而启动命令本身也含空格，按空格切分会取不到完整命令。
func TestParseProcessList(t *testing.T) {
	output := "    1 root      0.0  0.1 168376 /sbin/init splash\n" +
		"   10 root      0.0  0.0      0 [ksoftirqd/0]\n" +
		" 1234 www-data  0.5  1.2 204800 nginx: worker process\n" +
		" 1580 root      1.2  0.3  12345 sshd: /usr/sbin/sshd -D [listener] 0 of 10-100 startups\n" +
		"\n" +
		"这不是一行合法输出\n"

	processes := ParseProcessList(output)
	if len(processes) != 4 {
		t.Fatalf("应解析出 4 条进程，实际 %d：%#v", len(processes), processes)
	}

	init := processes[0]
	if init.PID != 1 || init.User != "root" {
		t.Errorf("首条应为 pid=1 user=root，实际 pid=%d user=%s", init.PID, init.User)
	}
	if init.Name != "init" {
		t.Errorf("进程名应从 /sbin/init 取 basename，实际 %q", init.Name)
	}
	if init.Command != "/sbin/init splash" {
		t.Errorf("完整命令应保留含空格的参数，实际 %q", init.Command)
	}
	if init.RSSBytes != 168376*1024 {
		t.Errorf("rss 应换算为字节（KB×1024），实际 %d", init.RSSBytes)
	}

	// 内核线程：rss 为 0、命令带方括号，且名字要能取出来。
	kernel := processes[1]
	if kernel.RSSBytes != 0 || kernel.Name != "[ksoftirqd/0]" {
		t.Errorf("内核线程解析异常：%#v", kernel)
	}

	// 用户名含点（www-data）与命令含冒号、方括号都不能影响解析。
	nginx := processes[2]
	if nginx.User != "www-data" || nginx.Name != "nginx:" || nginx.CPUPerc != 0.5 || nginx.MemPerc != 1.2 {
		t.Errorf("nginx 进程解析异常：%#v", nginx)
	}
	if processes[3].PID != 1580 || !strings.HasPrefix(processes[3].Command, "sshd: /usr/sbin/sshd") {
		t.Errorf("sshd 进程解析异常：%#v", processes[3])
	}
}

// TestParseTasklistProcessList 验证 Windows tasklist 的 CSV 输出解析。
func TestParseTasklistProcessList(t *testing.T) {
	output := "\"System Idle Process\",\"0\",\"Services\",\"0\",\"8 K\"\n" +
		"\"svchost.exe\",\"1234\",\"Services\",\"0\",\"12,345 K\"\n" +
		"\"notepad.exe\",\"5678\",\"Console\",\"1\",\"N/A\"\n"

	processes := ParseTasklistProcessList(output)
	if len(processes) != 3 {
		t.Fatalf("应解析出 3 条进程，实际 %d：%#v", len(processes), processes)
	}
	if processes[1].PID != 1234 || processes[1].Name != "svchost.exe" {
		t.Errorf("svchost 解析异常：%#v", processes[1])
	}
	// 千位分隔符要去掉，单位是 KB。
	if processes[1].RSSBytes != 12345*1024 {
		t.Errorf("内存应解析为 12345KB，实际 %d", processes[1].RSSBytes)
	}
	// 内存列为 N/A 时不能报错，按 0 处理。
	if processes[2].RSSBytes != 0 {
		t.Errorf("N/A 内存应记为 0，实际 %d", processes[2].RSSBytes)
	}
}

// TestProcessName 验证从启动命令推导进程名。
func TestProcessName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"/sbin/init splash", "init"},
		{"/usr/bin/python3 /opt/app/main.py", "python3"},
		{"nginx: worker process", "nginx:"},
		{`C:\Windows\System32\svchost.exe -k netsvcs`, "svchost.exe"},
		// 内核线程：整体是名字，不能按路径取 basename（否则会得到 "0]"）。
		{"[ksoftirqd/0]", "[ksoftirqd/0]"},
		{"[kworker/0:1]", "[kworker/0:1]"},
	}
	for _, c := range cases {
		if got := processName(c.in); got != c.want {
			t.Errorf("processName(%q)=%q，期望 %q", c.in, got, c.want)
		}
	}
}

// TestParseSystemdUnit 验证从 /proc/<pid>/cgroup 解析 systemd unit。
//
// 这里同时是安全边界测试：cgroup 内容来自被监控机（进程可以影响自己的 cgroup 归属），
// 而解析结果会被拼进 `systemctl restart <unit>` 交给远端 shell，必须挡掉一切注入尝试。
func TestParseSystemdUnit(t *testing.T) {
	cases := []struct {
		name   string
		cgroup string
		want   string
	}{
		{"cgroup v2", "0::/system.slice/nginx.service\n", "nginx.service"},
		{"cgroup v1", "1:name=systemd:/system.slice/telegraf.service\n", "telegraf.service"},
		{"模板服务带实例名", "0::/system.slice/getty@tty1.service\n", "getty@tty1.service"},
		// 用户级应用服务：中间的 user@1000.service 后面还跟着路径段，说明它不是进程所在 unit，
		// 因此应取末尾的应用服务（重启它只影响该用户的应用，是安全的）。
		{"用户级应用服务", "0::/user.slice/user-1000.slice/user@1000.service/app.slice/app-1.service\n",
			"app-1.service"},
		// 而 cgroup 本身就是用户会话管理器时，取到的 unit 会被 systemdRestartBlocked 拒绝。
		{"用户会话管理器本身", "0::/user.slice/user-1000.slice/user@1000.service\n", "user@1000.service"},
		{"容器内没有 unit", "0::/\n", ""},
		{"空内容", "", ""},
		{"只有 slice 没有 service", "0::/system.slice/\n", ""},
		// 注入尝试：全部应被 unitNameValid 白名单挡掉
		{"分号", "0::/system.slice/nginx.service; rm -rf /\n", ""},
		{"反引号", "0::/system.slice/`id`.service\n", ""},
		{"空格", "0::/system.slice/nginx service\n", ""},
		{"路径穿越", "0::/system.slice/../../etc/passwd.service\n", ""},
	}
	for _, c := range cases {
		if got := parseSystemdUnit(c.cgroup); got != c.want {
			t.Errorf("%s: parseSystemdUnit(%q)=%q，期望 %q", c.name, c.cgroup, got, c.want)
		}
	}
}

// TestSystemdRestartBlocked 验证整机/整会话级别的 unit 不允许通过界面重启。
func TestSystemdRestartBlocked(t *testing.T) {
	blocked := []string{"user@1000.service", "systemd-journald.service", "systemd-logind.service", "dbus.service"}
	for _, unit := range blocked {
		if reason := systemdRestartBlocked(unit); reason == "" {
			t.Errorf("%s 应被禁止重启", unit)
		}
	}
	allowed := []string{"nginx.service", "telegraf.service", "mysqld.service", "getty@tty1.service"}
	for _, unit := range allowed {
		if reason := systemdRestartBlocked(unit); reason != "" {
			t.Errorf("%s 应允许重启，却被拒绝：%s", unit, reason)
		}
	}
}

// TestParseMainPID 验证 `systemctl show -p MainPID --value` 的输出解析。
func TestParseMainPID(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   int
	}{
		{"正常带换行", "9123\n", 9123},
		{"带前后空白", "  9123  \n", 9123},
		{"服务已退出", "0\n", 0},
		{"无输出", "", 0},
		{"systemd 未记录", "--\n", 0},
		{"负数", "-1\n", 0},
		{"非数字", "abc\n", 0},
	}
	for _, c := range cases {
		if got := parseMainPID(c.output); got != c.want {
			t.Errorf("%s: parseMainPID(%q)=%d，期望 %d", c.name, c.output, got, c.want)
		}
	}
}

// TestRestartProcessRejectsUnsafePID 验证重启的本地校验与杀进程一致。
func TestRestartProcessRejectsUnsafePID(t *testing.T) {
	service := &Service{} // 校验先行，不会走到需要数据库/SSH 的分支
	for _, pid := range []int{0, -1, 1, 2} {
		if _, err := service.RestartProcess("any-agent", pid); err == nil {
			t.Errorf("PID %d 应被拒绝重启", pid)
		}
	}
}

// TestKillProcessRejectsUnsafePID 验证杀进程的本地校验。
//
// 这些校验发生在建立 SSH 连接之前，因此无需真实主机即可测试；同时也是安全边界：
// PID 非法或指向 init/kthreadd 时必须直接拒绝，绝不落到远程执行。
func TestKillProcessRejectsUnsafePID(t *testing.T) {
	service := &Service{} // 校验先行，不会走到需要数据库/SSH 的分支
	cases := []struct {
		name string
		pid  int
	}{
		{"零", 0},
		{"负数", -1},
		{"init/systemd", 1},
		{"kthreadd", 2},
	}
	for _, c := range cases {
		err := service.KillProcess("any-agent", c.pid)
		if err == nil {
			t.Errorf("%s：PID %d 应被拒绝", c.name, c.pid)
			continue
		}
		// 拒绝原因里应该带上 PID，便于前端直接展示给操作者。
		if !strings.Contains(err.Error(), "进程") && !strings.Contains(err.Error(), "不允许") {
			t.Errorf("%s：拒绝原因不够明确：%v", c.name, err)
		}
	}
}
