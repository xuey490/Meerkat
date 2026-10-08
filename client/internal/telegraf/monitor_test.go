package telegraf

import "testing"

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
