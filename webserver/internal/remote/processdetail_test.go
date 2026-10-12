package remote

import (
	"strings"
	"testing"
)

// processDetailSample 是一份接近真实的复合命令输出（字段取自 atd 这类普通服务进程）。
const processDetailSample = `===LSTART===
Thu Oct 11 02:57:22 2026

===STATUS===
Name:	atd
Umask:	0022
State:	S (sleeping)
Tgid:	802
Pid:	802
PPid:	1
Uid:	0	0	0	0
Threads:	1
VmSize:	    3804 kB
VmRSS:	    2140 kB
voluntary_ctxt_switches:	7
nonvoluntary_ctxt_switches:	1

===IO===
rchar: 16179
wchar: 59
syscr: 10
syscw: 2
read_bytes: 32768
write_bytes: 0

===STAT===
802 (atd) S 1 802 802 0 -1 4194560 100 0 0 0 3 5 0 0 20 0 1 0 93862 3895296 535 18446744073709551615 1 1 0 0 0 0 0 0 0

===CMDLINE===
/usr/sbin/atd -f 

===EXE===
/usr/sbin/atd

===CWD===
/var/spool/cron/atjobs

===FD===
/dev/null
/dev/pts/0
/dev/sda
/var/spool/cron/atjobs/atd.1
/tmp/report.csv (deleted)
socket:[12345]
pipe:[999]
anon_inode:[eventfd]
weird-target
`

// TestParseProcessDetail 验证进程详情的整体解析。
func TestParseProcessDetail(t *testing.T) {
	detail, err := ParseProcessDetail(processDetailSample)
	if err != nil {
		t.Fatalf("正常输出不应报错：%v", err)
	}
	if detail.Name != "atd" {
		t.Errorf("进程名应为 atd，实际 %q", detail.Name)
	}
	if detail.State != "S (sleeping)" {
		t.Errorf("状态应原样透传，实际 %q", detail.State)
	}
	if detail.PPID != 1 || detail.Threads != 1 {
		t.Errorf("父进程/线程数解析异常：ppid=%d threads=%d", detail.PPID, detail.Threads)
	}
	if detail.Exe != "/usr/sbin/atd" {
		t.Errorf("可执行文件解析异常：%q", detail.Exe)
	}
	if detail.Cwd != "/var/spool/cron/atjobs" {
		t.Errorf("工作目录解析异常：%q", detail.Cwd)
	}
	if detail.Command != "/usr/sbin/atd -f" {
		t.Errorf("命令行应去掉尾部空白，实际 %q", detail.Command)
	}
	if detail.StartedAt != "2026-10-11 02:57:22" {
		t.Errorf("启动时间应被规范化，实际 %q", detail.StartedAt)
	}
	if detail.RSSBytes != 2140*1024 {
		t.Errorf("物理内存应为 %d 字节，实际 %d", 2140*1024, detail.RSSBytes)
	}
	if detail.VSZBytes != 3804*1024 {
		t.Errorf("虚拟内存应为 %d 字节，实际 %d", 3804*1024, detail.VSZBytes)
	}
	// stat 字段 14/15 分别是 utime=3、stime=5。
	if detail.CPUTicks != 8 {
		t.Errorf("CPU 时间片应为 8（3+5），实际 %d", detail.CPUTicks)
	}
	if detail.VoluntaryCtxSwitches != 7 || detail.NonVoluntaryCtxSwitches != 1 {
		t.Errorf("上下文切换解析异常：%d / %d",
			detail.VoluntaryCtxSwitches, detail.NonVoluntaryCtxSwitches)
	}
	if detail.IO.RChar != 16179 || detail.IO.WChar != 59 ||
		detail.IO.ReadBytes != 32768 || detail.IO.WriteBytes != 0 {
		t.Errorf("IO 统计解析异常：%#v", detail.IO)
	}

	// 逐个对照样本：3 个 /dev/* 是设备；atd.1 与 report.csv(deleted) 是普通文件
	//（已删除但仍被打开的文件仍以 / 开头，依旧算文件）；其余各一类。
	want := ProcessFDCounts{Total: 9, File: 2, Socket: 1, Pipe: 1, Anon: 1, Device: 3, Other: 1}
	if detail.FDs != want {
		t.Errorf("文件描述符分类应为 %#v，实际 %#v", want, detail.FDs)
	}
	if detail.FDTruncated {
		t.Error("样本未达上限，不应标记截断")
	}
}

// TestParseProcessDetailMissing 验证进程不存在时明确报错。
//
// 命令里所有读取都做了 2>/dev/null，所以"没有 STATUS 节"本身就是"进程已不存在"的结论。
func TestParseProcessDetailMissing(t *testing.T) {
	if _, err := ParseProcessDetail(""); err == nil {
		t.Error("空输出必须报错")
	}
	output := "===LSTART===\n\n===STATUS===\n\n===IO===\n\n===FD===\n"
	if _, err := ParseProcessDetail(output); err == nil {
		t.Error("STATUS 为空时必须报错（进程不存在）")
	}
}

// TestParseProcStatCommWithSpaces 验证进程名含空格或括号时字段不会错位。
//
// 这是 /proc/<pid>/stat 的经典陷阱：comm 在圆括号里且可以含空格，
// 因此必须从最后一个 ')' 之后开始数字段，不能整行切分。
func TestParseProcStatCommWithSpaces(t *testing.T) {
	// comm = "a b"，utime=11、stime=22。
	content := "900 (a b) S 1 900 900 0 -1 4194560 100 0 0 0 11 22 0 0 20 0 1 0 93862"
	utime, stime, ok := parseProcStat(content)
	if !ok || utime != 11 || stime != 22 {
		t.Errorf("含空格的进程名不应导致字段错位：utime=%d stime=%d ok=%v", utime, stime, ok)
	}

	// comm = "sd-pam"（systemd 的用户会话进程，名字里就有连字符与括号语义）。
	content = "805 (sd-pam) S 1 805 805 0 -1 4194560 57 0 0 0 4 9 0 0 20 0 1 0 93800"
	utime, stime, ok = parseProcStat(content)
	if !ok || utime != 4 || stime != 9 {
		t.Errorf("括号语义的进程名不应导致字段错位：utime=%d stime=%d ok=%v", utime, stime, ok)
	}

	if _, _, ok := parseProcStat("no parens here"); ok {
		t.Error("缺少圆括号时必须返回失败，而不是猜一组数字")
	}
	if _, _, ok := parseProcStat("1 (x) S 1"); ok {
		t.Error("字段明显不足时必须返回失败")
	}
}

// TestClassifyFDList 验证描述符按类型归类。
func TestClassifyFDList(t *testing.T) {
	cases := map[string]string{
		"socket:[12345]":             "socket",
		"pipe:[999]":                 "pipe",
		"anon_inode:[eventfd]":       "anon",
		"/dev/null":                  "device",
		"/var/log/app.log":           "file",
		"/tmp/x (deleted)":           "file",
		"memfd:name (deleted)":       "other",
		"/dev/shm/segment (deleted)": "device",
	}
	for target, want := range cases {
		if got := classifyFD(target); got != want {
			t.Errorf("classifyFD(%q) = %q，期望 %q", target, got, want)
		}
	}
}

// TestParsePsLStart 验证 ps 时间戳规范化，含个位数日期的空格补齐写法。
func TestParsePsLStart(t *testing.T) {
	cases := map[string]string{
		"Thu Oct 11 02:57:22 2026": "2026-10-11 02:57:22",
		"Sun Oct  5 23:59:59 2025": "2025-10-05 23:59:59", // 个位数日期为空格补齐
		"":                         "",
		"n/a":                      "",
		"garbage":                  "",
	}
	for raw, want := range cases {
		if got := parsePsLStart(raw); got != want {
			t.Errorf("parsePsLStart(%q) = %q，期望 %q", raw, got, want)
		}
	}
}

// TestSplitSections 验证分节解析：标记前的内容被丢弃，各节互不串味。
func TestSplitSections(t *testing.T) {
	output := "Welcome to Ubuntu 20.04.3 LTS\n" + // 登录横幅，应被丢弃
		"===A===\nline-a1\nline-a2\n" +
		"===B===\nline-b\n"

	sections := splitSections(output)
	if _, found := sections["Welcome"]; found {
		t.Error("标记之前的内容不应成为分节")
	}
	if strings.TrimSpace(sections["A"]) != "line-a1\nline-a2" {
		t.Errorf("A 节内容异常：%q", sections["A"])
	}
	if strings.TrimSpace(sections["B"]) != "line-b" {
		t.Errorf("B 节内容异常：%q", sections["B"])
	}
	if _, found := sections["C"]; found {
		t.Error("不存在的分节不应出现")
	}
}
