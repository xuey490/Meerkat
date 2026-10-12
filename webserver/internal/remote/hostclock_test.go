package remote

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// TestParseHostClock 验证完整输出下的全部字段与时钟偏移计算。
//
// 偏移的构造是关键：ParseHostClock 取「发起前」与「返回后」的中点为基准，
// 所以把远端时间设成「中点 + 0.7s」，期望偏移就正好是 +0.7。
func TestParseHostClock(t *testing.T) {
	started := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	finished := started.Add(120 * time.Millisecond)
	midpoint := started.Add(60 * time.Millisecond)
	epoch := float64(midpoint.UnixMilli())/1000 + 0.7

	output := fmt.Sprintf("EPOCH=%.3f\nOFFSET=-0700\nTZ=America/Los_Angeles\n"+
		"OS=Ubuntu 20.04.3 LTS\nMACHINE=x86_64\n", epoch)

	clock, err := ParseHostClock(output, started, finished)
	if err != nil {
		t.Fatalf("正常输出不应报错：%v", err)
	}
	if !clock.Supported {
		t.Error("Linux 正常采集时 Supported 应为 true")
	}
	if clock.Timezone != "America/Los_Angeles" {
		t.Errorf("时区应为 America/Los_Angeles，实际 %q", clock.Timezone)
	}
	if clock.UTCOffset != "-0700" {
		t.Errorf("UTC 偏移应为 -0700，实际 %q", clock.UTCOffset)
	}
	if clock.OSName != "Ubuntu 20.04.3 LTS" {
		t.Errorf("发行版名称应保留空格，实际 %q", clock.OSName)
	}
	if clock.Machine != "x86_64" {
		t.Errorf("架构应为 x86_64，实际 %q", clock.Machine)
	}
	if math.Abs(clock.OffsetSeconds-0.7) > 1e-9 {
		t.Errorf("时钟偏移应为 +0.7s，实际 %v", clock.OffsetSeconds)
	}
	if clock.RoundTripMs != 120 {
		t.Errorf("往返耗时应为 120ms，实际 %d", clock.RoundTripMs)
	}
	if math.Abs(clock.RemoteEpoch-epoch) > 1e-9 {
		t.Errorf("RemoteEpoch 应原样回传，实际 %v", clock.RemoteEpoch)
	}

	// 钟面时间必须按远端自己的偏移渲染，而不是换算到 UTC 或本机时区。
	if !strings.HasSuffix(clock.RemoteTime, "-07:00") {
		t.Errorf("RemoteTime 应带远端偏移 -07:00，实际 %q", clock.RemoteTime)
	}
	parsed, err := time.Parse(time.RFC3339, clock.RemoteTime)
	if err != nil {
		t.Fatalf("RemoteTime 应是合法 RFC3339：%v", err)
	}
	if parsed.Unix() != int64(epoch) {
		t.Errorf("RemoteTime 换算回 Unix 应与 RemoteEpoch 一致：%d vs %d", parsed.Unix(), int64(epoch))
	}
}

// TestParseHostClockNegativeOffset 验证远端比本地慢时偏移为负。
func TestParseHostClockNegativeOffset(t *testing.T) {
	started := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	finished := started.Add(200 * time.Millisecond)
	midpoint := started.Add(100 * time.Millisecond)
	epoch := float64(midpoint.UnixMilli())/1000 - 2.5

	clock, err := ParseHostClock(
		fmt.Sprintf("EPOCH=%.3f\nOFFSET=+0800\nMACHINE=aarch64\n", epoch), started, finished)
	if err != nil {
		t.Fatalf("不应报错：%v", err)
	}
	if math.Abs(clock.OffsetSeconds+2.5) > 1e-9 {
		t.Errorf("远端更慢时偏移应为 -2.5s，实际 %v", clock.OffsetSeconds)
	}
	if !strings.HasSuffix(clock.RemoteTime, "+08:00") {
		t.Errorf("RemoteTime 应带 +08:00，实际 %q", clock.RemoteTime)
	}
}

// TestParseHostClockMissingFields 验证单个字段缺失时不会让其它字段错位。
//
// 这是 KEY=VALUE 前缀设计所防的回归：时区与发行版都走"可用才输出"的回退链，
// 早期按输出行号取值的实现会在时区为空时把发行版当成时区（实测踩过）。
func TestParseHostClockMissingFields(t *testing.T) {
	started := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	finished := started.Add(40 * time.Millisecond)
	epoch := float64(started.UnixMilli()) / 1000

	// 只有 EPOCH 与 OFFSET：TZ / OS / MACHINE 三行整体缺席。
	clock, err := ParseHostClock(fmt.Sprintf("EPOCH=%.3f\nOFFSET=+0000\n", epoch), started, finished)
	if err != nil {
		t.Fatalf("字段缺失不应报错：%v", err)
	}
	if clock.Timezone != "" || clock.OSName != "" || clock.Machine != "" {
		t.Errorf("缺席的字段应留空，实际 %#v", clock)
	}
	if clock.UTCOffset != "+0000" {
		t.Errorf("OFFSET 应照常解析，实际 %q", clock.UTCOffset)
	}
	if !clock.Supported {
		t.Error("字段缺失不代表采集失败，Supported 应为 true")
	}

	// 空值形式（键在、值为空）同样要落到留空，而不是留下空字符串以外的噪声。
	clock, err = ParseHostClock(
		fmt.Sprintf("EPOCH=%.3f\nOFFSET=\nTZ=\nOS=\nMACHINE=\n", epoch), started, finished)
	if err != nil {
		t.Fatalf("空值不应报错：%v", err)
	}
	if clock.UTCOffset != "" || clock.Timezone != "" || clock.OSName != "" || clock.Machine != "" {
		t.Errorf("空值字段应留空，实际 %#v", clock)
	}
}

// TestParseHostClockRejectsNoise 验证噪声内容不会被当成有效取值。
func TestParseHostClockRejectsNoise(t *testing.T) {
	started := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	finished := started.Add(30 * time.Millisecond)
	epoch := float64(started.UnixMilli()) / 1000

	// TZ 为占位值 / shell 告警；MACHINE 为乱码；另有一行完全不含等号。
	output := fmt.Sprintf("EPOCH=%.3f\nOFFSET=+0800\nTZ=Unknown\n"+
		"OS=Ubuntu 22.04 LTS\nMACHINE=x86_64 -o something\n"+
		"-bash: timedatectl: command not found\n", epoch)

	clock, err := ParseHostClock(output, started, finished)
	if err != nil {
		t.Fatalf("噪声不应导致报错：%v", err)
	}
	if clock.Timezone != "" {
		t.Errorf("Unknown 是占位值，不应作为时区，实际 %q", clock.Timezone)
	}
	if clock.Machine != "" {
		t.Errorf("含空格的架构串应被拒绝，实际 %q", clock.Machine)
	}
	// 其余合法字段不受噪声影响。
	if clock.OSName != "Ubuntu 22.04 LTS" || clock.UTCOffset != "+0800" {
		t.Errorf("合法字段不应被噪声影响：%#v", clock)
	}
}

// TestParseHostClockInvalidEpoch 验证 EPOCH 无法解析时返回错误。
//
// 典型场景是 busybox 的 date 不支持 %N，会原样吐出 "1760157237.N"。
// 此时必须报错——显示一个假的时钟比不显示更糟。
func TestParseHostClockInvalidEpoch(t *testing.T) {
	started := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	cases := []string{
		"EPOCH=1760157237.N\nOFFSET=+0800\n",
		"",
		"OFFSET=+0800\nTZ=Asia/Shanghai\n",
	}
	for _, output := range cases {
		if _, err := ParseHostClock(output, started, started); err == nil {
			t.Errorf("EPOCH 缺失或非法时必须返回错误，输入 %q", output)
		}
	}
}

// TestUTCOffsetSeconds 验证 UTC 偏移文本到秒数的换算。
func TestUTCOffsetSeconds(t *testing.T) {
	cases := map[string]int{
		"+0800": 8 * 3600,
		"-0700": -7 * 3600,
		"-0330": -(3*3600 + 30*60),
		"+0000": 0,
		"":      0, // 取不到
		"0800":  0, // 缺正负号，不合法
		"+8:00": 0, // 带冒号，不合法
		"+08":   0, // 位数不足
	}
	for raw, want := range cases {
		if got := UTCOffsetSeconds(raw); got != want {
			t.Errorf("UTCOffsetSeconds(%q) = %d，期望 %d", raw, got, want)
		}
	}
}
