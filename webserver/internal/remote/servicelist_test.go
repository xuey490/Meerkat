package remote

import (
	"strings"
	"testing"
)

// TestParseServiceUnits 验证运行状态与"自启状态"两个来源的正确合并。
//
// 关键点是**取并集**：只在 list-unit-files 里出现的单元（从未被加载）也必须列出，
// 否则界面会漏掉一批已安装但没跑过的服务。
func TestParseServiceUnits(t *testing.T) {
	output := "accounts-daemon.service loaded active   running Accounts Service\n" +
		"acpid.service           loaded active   running ACPI event daemon\n" +
		"alsa-state.service      loaded inactive dead    Manage Sound Card State (restore and store)\n" +
		"auditd.service          loaded active   running Security Auditing Service\n" +
		serviceListSplitMarker + "\n" +
		"accounts-daemon.service            enabled\n" +
		"alsa-state.service                 static\n" +
		"auditd.service                     enabled\n" +
		"extra-only.service                 disabled\n"

	units := ParseServiceUnits(output)
	if len(units) != 5 {
		t.Fatalf("应合并出 5 个单元，实际 %d：%#v", len(units), units)
	}

	// 必须按单元名排序，不依赖两条命令的输出顺序。
	wantOrder := []string{
		"accounts-daemon.service", "acpid.service", "alsa-state.service",
		"auditd.service", "extra-only.service",
	}
	for index, want := range wantOrder {
		if units[index].Unit != want {
			t.Errorf("第 %d 个单元应为 %s，实际 %s", index, want, units[index].Unit)
		}
	}

	first := units[0]
	if first.Active != "active" || first.Sub != "running" || first.Startup != "enabled" {
		t.Errorf("accounts-daemon 状态应为 active/running/enabled，实际 %#v", first)
	}
	if first.Description != "Accounts Service" {
		t.Errorf("描述应保留空格，实际 %q", first.Description)
	}
	if first.Protected {
		t.Error("accounts-daemon 不应被列为禁止操作")
	}

	// 描述里带括号、且状态是 inactive/dead。
	if alsa := units[2]; alsa.Active != "inactive" || alsa.Sub != "dead" || alsa.Startup != "static" {
		t.Errorf("alsa-state 状态解析异常：%#v", alsa)
	}
	if alsa := units[2]; alsa.Description != "Manage Sound Card State (restore and store)" {
		t.Errorf("描述里的括号不应影响解析，实际 %q", alsa.Description)
	}

	// 只在 list-unit-files 里出现：运行状态按 systemd 口径报 inactive/dead，描述为空。
	extra := units[4]
	if extra.Active != "inactive" || extra.Sub != "dead" || extra.Startup != "disabled" {
		t.Errorf("仅存在于单元文件的服务应报 inactive/dead 且带自启状态，实际 %#v", extra)
	}
	if extra.Description != "" {
		t.Errorf("无运行时信息时描述应为空，实际 %q", extra.Description)
	}

	// 高危服务必须在解析阶段就被标记，界面据此禁用操作入口。
	if auditd := units[3]; !auditd.Protected || auditd.BlockReason == "" {
		t.Errorf("auditd 应被标记为禁止操作并给出原因，实际 %#v", auditd)
	}
}

// TestParseServiceUnitsToleratesGarbage 验证缺段与噪声不会让解析崩掉。
func TestParseServiceUnitsToleratesGarbage(t *testing.T) {
	// 没有分隔标记（第二条 systemctl 失败时可能出现）：只有 list-units 的内容。
	onlyUnits := "nginx.service loaded active running A high performance web server\n"
	units := ParseServiceUnits(onlyUnits)
	if len(units) != 1 || units[0].Unit != "nginx.service" {
		t.Fatalf("缺段时应仍能解析出 list-units 的内容，实际 %#v", units)
	}
	if units[0].Startup != "" {
		t.Errorf("缺段时自启状态应留空，实际 %q", units[0].Startup)
	}
	if units := ParseServiceUnits(""); len(units) != 0 {
		t.Errorf("空输出应得到空列表，实际 %#v", units)
	}
}

// TestServiceOperationBlocked 验证高危服务的拦截规则。
//
// 规则表本身是安全边界，因此逐条断言：既要拦住必须拦的，也不能把普通应用误拦成"不可操作"。
func TestServiceOperationBlocked(t *testing.T) {
	blocked := []string{
		// 切断我们自身接入
		"ssh.service", "sshd.service",
		// systemd / dbus 底座
		"systemd-journald.service", "systemd-logind.service", "systemd-resolved.service",
		"systemd-networkd.service", "dbus.service", "dbus-broker.service",
		"dbus-org.freedesktop.systemd1.service",
		// 网络
		"networking.service", "NetworkManager.service", "networkd-dispatcher.service",
		"dhcpcd.service", "wpa_supplicant.service",
		// 防火墙与审计
		"ufw.service", "firewalld.service", "nftables.service", "iptables.service",
		"fail2ban.service", "auditd.service", "apparmor.service", "polkit.service",
		// 大小写不敏感
		"SSHD.SERVICE",
	}
	for _, unit := range blocked {
		if reason := ServiceOperationBlocked(unit); reason == "" {
			t.Errorf("%s 应被禁止操作", unit)
		}
	}

	allowed := []string{
		"nginx.service", "redis-server.service", "mysqld.service", "docker.service",
		"containerd.service", "cron.service", "telegraf.service", "monitor-agent.service",
		"guacd.service", "vncserver-2.service", "unattended-upgrades.service",
		// D-Bus 的服务激活别名（蓝牙 / WiFi）只是"某服务的别名"，不是系统总线本身，
		// 不该被 systemd-/dbus 这类族前缀误伤——拦错了理由也是错的。
		"dbus-org.bluez.service", "dbus-fi.w1.wpa_supplicant1.service",
	}
	for _, unit := range allowed {
		if reason := ServiceOperationBlocked(unit); reason != "" {
			t.Errorf("%s 不应被禁止操作，却给出原因：%s", unit, reason)
		}
	}

	if ServiceOperationBlocked("") == "" {
		t.Error("空服务名应被拒绝")
	}
}

// TestOperateServiceValidation 验证三道校验都在建立 SSH 连接之前完成。
//
// 这里刻意只测校验分支：校验通过后会去查主机并拨号，需要真实数据库与远端，
// 不适合放进单元测试。校验先于连线执行，本身就是设计意图（便宜的先做，
// 也让非法请求根本不会产生 SSH 连接）。
func TestOperateServiceValidation(t *testing.T) {
	service := &Service{}

	if err := service.OperateService("agent", "nginx.service", "rm -rf /"); err == nil ||
		!strings.Contains(err.Error(), "不支持的服务操作") {
		t.Errorf("非白名单动作应被拒绝，实际 err=%v", err)
	}

	if err := service.OperateService("agent", "nginx.service; rm -rf /", ServiceActionStop); err == nil ||
		!strings.Contains(err.Error(), "服务名不合法") {
		t.Errorf("含 shell 元字符的单元名应被拒绝，实际 err=%v", err)
	}

	if err := service.OperateService("agent", "nginx.socket", ServiceActionStop); err == nil ||
		!strings.Contains(err.Error(), "服务名不合法") {
		t.Errorf("非 .service 单元应被拒绝，实际 err=%v", err)
	}

	if err := service.OperateService("agent", "sshd.service", ServiceActionStop); err == nil ||
		!strings.Contains(err.Error(), "不允许操作") {
		t.Errorf("高危服务应被拦截，实际 err=%v", err)
	}

	if err := service.OperateService("agent", "systemd-journald.service", ServiceActionRestart); err == nil ||
		!strings.Contains(err.Error(), "不允许操作") {
		t.Errorf("systemd 组件应被拦截，实际 err=%v", err)
	}
}
