package telegraf

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTasklistOutput(t *testing.T) {
	cases := []struct {
		in      string
		pid     uint32
		running bool
	}{
		{"", 0, false},
		{"INFO: No tasks are running which match the specified criteria.", 0, false},
		{"信息: 没有运行的任务匹配指定标准。", 0, false},
		{`"telegraf.exe","4321","Console","1","12,345 K"`, 4321, true},
	}
	for _, c := range cases {
		pid, running := parseTasklistOutput(c.in)
		if pid != c.pid || running != c.running {
			t.Fatalf("parseTasklistOutput(%q)=(%d,%v) want (%d,%v)", c.in, pid, running, c.pid, c.running)
		}
	}
}

// TestBinaryCandidates 验证版本探测的候选命令：配置路径优先，并且总是兜底 PATH 里的 telegraf。
//
// 这条兜底是「跨平台拷贝 agent.yaml 导致 Telegraf 版本一栏空白」的修复核心：
// Windows 包里的 "Telegraf/telegraf.exe" 直接在 Linux 上执行必然失败，
// 兜底尝试 PATH 里的 telegraf 就能取到版本，不必逐台改配置。
func TestBinaryCandidates(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		want       []string
	}{
		{"未配置时只用 PATH", "", []string{"telegraf"}},
		{"空白配置按未配置处理", "   ", []string{"telegraf"}},
		{"配置的路径优先", "/usr/bin/telegraf", []string{"/usr/bin/telegraf", "telegraf"}},
		{"跨平台拷贝的 Windows 路径也能兜底", "Telegraf/telegraf.exe", []string{"Telegraf/telegraf.exe", "telegraf"}},
		{"配置本身就是 telegraf 时不重复", "telegraf", []string{"telegraf"}},
	}
	for _, c := range cases {
		got := New(Options{BinaryPath: c.configured}).binaryCandidates()
		if len(got) != len(c.want) {
			t.Fatalf("%s: binaryCandidates()=%v，期望 %v", c.name, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s: binaryCandidates()=%v，期望 %v", c.name, got, c.want)
			}
		}
	}
}

// TestProbeVersionFallsBackToPATH 验证「配置里的路径不可用」时能兜底到 PATH 里的 telegraf。
//
// 这是真实故障的回归测试：Linux 上用的是从 Windows 包拷来的 agent.yaml，里面写着
// "Telegraf/telegraf.exe"，直接执行必然失败，服务器列表的 Telegraf 版本一栏因此一直空白。
// 这里用仓库自带的 telegraf 二进制模拟「PATH 里有可用的 telegraf」，配置则故意指向一个
// 测试工作目录下不存在的相对路径。
func TestProbeVersionFallsBackToPATH(t *testing.T) {
	binary, err := filepath.Abs(filepath.Join("..", "..", "..", "Telegraf", "telegraf.exe"))
	if err != nil {
		t.Fatalf("解析 Telegraf 路径失败: %v", err)
	}
	if _, err := os.Stat(binary); err != nil {
		t.Skipf("仓库内没有 Telegraf 二进制（%s），跳过该用例", binary)
	}
	// 只影响本进程：把它所在目录放进 PATH，模拟发行版把 telegraf 装进系统 PATH 的情况。
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))

	// 相对路径在测试工作目录（client/internal/telegraf）下不存在，必须靠兜底才能取到版本。
	m := New(Options{BinaryPath: "Telegraf/telegraf.exe"})
	version := m.version(context.Background())
	if version == "" {
		t.Fatal("配置路径不可用时应兜底到 PATH 中的 telegraf，实际没取到版本")
	}
	if !strings.HasPrefix(version, "Telegraf") {
		t.Errorf("版本信息格式异常: %q", version)
	}
	// 取到之后应走缓存（再次调用不再 fork 进程，返回值一致）。
	if again := m.version(context.Background()); again != version {
		t.Errorf("缓存后的版本应保持一致：%q != %q", again, version)
	}
}

// TestFirstLine 验证版本输出只取第一行非空内容。
func TestFirstLine(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"\n\n  \n", ""},
		{"Telegraf 1.40.1 (git: HEAD@26b8f1d)\n", "Telegraf 1.40.1 (git: HEAD@26b8f1d)"},
		{"\nTelegraf 1.29.0\n额外输出\n", "Telegraf 1.29.0"},
	}
	for _, c := range cases {
		if got := firstLine([]byte(c.in)); got != c.want {
			t.Fatalf("firstLine(%q)=%q，期望 %q", c.in, got, c.want)
		}
	}
}
