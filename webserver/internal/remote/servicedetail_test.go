package remote

import "testing"

// TestParseServiceDetail 用真实 `systemctl show` 输出验证详情解析。
func TestParseServiceDetail(t *testing.T) {
	output := "Description=ACPI event daemon\n" +
		"LoadState=loaded\n" +
		"ActiveState=active\n" +
		"SubState=running\n" +
		"UnitFileState=disabled\n" +
		"MainPID=739\n" +
		"MemoryCurrent=356352\n" +
		"FragmentPath=/lib/systemd/system/acpid.service\n" +
		"ExecStart={ path=/usr/sbin/acpid ; argv[]=/usr/sbin/acpid ; ignore_errors=no ; start_time=[n/a] ; pid=0 }\n" +
		"ExecMainStartTimestamp=Sun 2026-10-11 02:57:22 PDT\n" +
		"NRestarts=0\n"

	detail, err := ParseServiceDetail(output)
	if err != nil {
		t.Fatalf("正常输出不应报错：%v", err)
	}
	if detail.Description != "ACPI event daemon" {
		t.Errorf("描述解析异常：%q", detail.Description)
	}
	if detail.LoadState != "loaded" || detail.ActiveState != "active" || detail.SubState != "running" {
		t.Errorf("状态解析异常：%#v", detail)
	}
	if detail.UnitFileState != "disabled" {
		t.Errorf("自启状态应为 disabled，实际 %q", detail.UnitFileState)
	}
	if detail.MainPID != 739 {
		t.Errorf("MainPID 应为 739，实际 %d", detail.MainPID)
	}
	if detail.MemoryBytes != 356352 {
		t.Errorf("内存应为 356352 字节，实际 %d", detail.MemoryBytes)
	}
	if detail.MemoryUnlimited {
		t.Error("具体字节数不应标记为不受限")
	}
	if detail.StartedAt != "2026-10-11 02:57:22" {
		t.Errorf("启动时间应被规范化，实际 %q", detail.StartedAt)
	}
	// ExecStart 的值里本身含 "=" 与 ";"，必须整体作为值收下而不是被截断。
	if detail.ExecStart == "" || detail.ExecStart[0] != '{' {
		t.Errorf("ExecStart 应完整保留，实际 %q", detail.ExecStart)
	}
}

// TestParseServiceDetailEmpty 验证没有任何属性行时报错（通常是单元不存在）。
func TestParseServiceDetailEmpty(t *testing.T) {
	if _, err := ParseServiceDetail(""); err == nil {
		t.Error("空输出必须返回错误")
	}
	if _, err := ParseServiceDetail("Unit foo.service could not be found.\n"); err == nil {
		t.Error("只有报错文本时必须返回错误")
	}
}

// TestParseSystemdMemory 验证"不受限"与"未知"必须区分开。
//
// 二者都取不到字节数，但界面说法不同：未限制不能显示成「内存 0 B」。
func TestParseSystemdMemory(t *testing.T) {
	if bytes, unlimited := parseSystemdMemory("356352"); bytes != 356352 || unlimited {
		t.Errorf("普通字节数解析异常：%d %v", bytes, unlimited)
	}
	if bytes, unlimited := parseSystemdMemory("infinity"); bytes != 0 || !unlimited {
		t.Errorf("infinity 应标记为不受限：%d %v", bytes, unlimited)
	}
	if bytes, unlimited := parseSystemdMemory("[not set]"); bytes != 0 || unlimited {
		t.Errorf("未统计应既不取值也不标记不受限：%d %v", bytes, unlimited)
	}
	if bytes, unlimited := parseSystemdMemory(""); bytes != 0 || unlimited {
		t.Errorf("空值应既不取值也不标记不受限：%d %v", bytes, unlimited)
	}
}

// TestParseSystemdTimestamp 验证时间戳规范化与空值回退。
func TestParseSystemdTimestamp(t *testing.T) {
	cases := map[string]string{
		"Sun 2026-10-11 02:57:22 PDT": "2026-10-11 02:57:22",
		"Mon 2026-01-05 00:00:01 CST": "2026-01-05 00:00:01",
		"n/a":                         "",
		"":                            "",
		"不是时间":                        "",
	}
	for raw, want := range cases {
		if got := parseSystemdTimestamp(raw); got != want {
			t.Errorf("parseSystemdTimestamp(%q) = %q，期望 %q", raw, got, want)
		}
	}
}

// TestParseServiceLogs 验证日志按「时间戳 + 正文」拆分。
//
// 拆开是为了让界面的「时间戳」开关能本地即时切换，而不必为了隐藏时间戳再查一次日志。
func TestParseServiceLogs(t *testing.T) {
	output := "-- Logs begin at Tue 2026-10-06 21:36:22 PDT, end at Sun 2026-10-11T23:02:15-0700 --\n" +
		"2026-10-06T23:02:15-0700 systemd[1]: Stopping Deferred execution scheduler...\n" +
		"-- Reboot --\n" +
		"2026-10-06T23:07:46-0700 systemd[1]: Starting Deferred execution scheduler...\n" +
		"2026-10-11T23:02:15.123456-0700 systemd[1]: Started Deferred execution scheduler.\n" +
		"\n"

	lines := ParseServiceLogs(output)
	if len(lines) != 5 {
		t.Fatalf("应解析出 5 行（空行跳过），实际 %d：%#v", len(lines), lines)
	}

	// 首行是 journalctl 的引导提示，没有标准时间戳，整行作为正文保留。
	if lines[0].Time != "" || lines[0].Message == "" {
		t.Errorf("引导行应整行保留为正文，实际 %#v", lines[0])
	}
	if lines[1].Time != "2026-10-06T23:02:15-0700" {
		t.Errorf("时间戳解析异常：%q", lines[1].Time)
	}
	if lines[1].Message != "systemd[1]: Stopping Deferred execution scheduler..." {
		t.Errorf("正文不应包含时间戳：%q", lines[1].Message)
	}

	// 重启分隔行没有时间戳，且它本身就是要看的内容，不能丢掉。
	if lines[2].Time != "" || lines[2].Message != "-- Reboot --" {
		t.Errorf("重启分隔行应原样保留，实际 %#v", lines[2])
	}

	// 部分版本的 short-iso 带小数秒。
	if lines[4].Time != "2026-10-11T23:02:15.123456-0700" {
		t.Errorf("带小数秒的时间戳解析异常：%q", lines[4].Time)
	}
}

// TestParseServiceLogsEmpty 验证空输出与「无日志」提示不会 panic。
func TestParseServiceLogsEmpty(t *testing.T) {
	if lines := ParseServiceLogs(""); len(lines) != 0 {
		t.Errorf("空输出应得到空列表，实际 %#v", lines)
	}
	lines := ParseServiceLogs("-- No entries --\n")
	if len(lines) != 1 || lines[0].Message != "-- No entries --" {
		t.Errorf("无日志提示应保留，实际 %#v", lines)
	}
}
