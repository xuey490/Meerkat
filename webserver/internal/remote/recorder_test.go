package remote

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestNewRecorderCreatesParentDir 锁定一个真实踩过的坑：
// 录像路径带日期子目录（20261007/000001.cast），而 os.Create 不会创建中间目录，
// 启动时也只建了录像根目录。缺了 MkdirAll，第一条 SSH 会话就会因 ENOENT 失败，
// 对外表现为 WebSocket 握手 500。
func TestNewRecorderCreatesParentDir(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "20261007", "000001.cast")

	recorder, err := NewRecorder(path, 120, 30)
	if err != nil {
		t.Fatalf("NewRecorder 应自动创建父目录，实际报错: %v", err)
	}
	if err := recorder.Output([]byte("hello")); err != nil {
		t.Fatalf("写入输出事件失败: %v", err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("关闭录像失败: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("录像文件未生成: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("录像应含 1 行头 + 1 行事件，实际 %d 行: %q", len(lines), string(raw))
	}
	if !strings.Contains(lines[0], `"version":2`) || !strings.Contains(lines[0], `"width":120`) {
		t.Errorf("asciinema 头不符合预期: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"o"`) || !strings.Contains(lines[1], "hello") {
		t.Errorf("输出事件不符合预期: %s", lines[1])
	}
}

// TestCommandCaptureAggregates 校验命令流捕获：按回车聚合、忽略空命令、忽略退格、
// Flush 兜底未以回车结束的输入。
func TestCommandCaptureAggregates(t *testing.T) {
	var captured []string
	capture := NewCommandCapture(func(_ int, command string) {
		captured = append(captured, command)
	})

	capture.Write([]byte("ls -al\r"))
	capture.Write([]byte("pwd\n"))
	capture.Write([]byte("   \r"))        // 全空白：丢弃
	capture.Write([]byte("halt\x15"))     // Ctrl+U 清空当前行：不应在 Flush 时落库
	capture.Write([]byte("shutdown\x03")) // Ctrl+C 取消当前行：不应在 Flush 时落库
	capture.Write([]byte("id\x7f"))       // 退格忽略
	capture.Flush()                       // 未回车：兜底

	want := []string{"ls -al", "pwd", "id"}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("命令捕获不符合预期\n实际: %v\n期望: %v", captured, want)
	}
}

// TestRecordingPathLayout 校验录像相对路径的目录结构契约（日期子目录 + 零填充文件名）。
func TestRecordingPathLayout(t *testing.T) {
	root := t.TempDir()
	service := New(nil, "test-secret", root, nil)

	relative := service.RecordingPath(42)
	if filepath.Dir(relative) == "." {
		t.Fatalf("录像路径应含日期子目录，实际: %s", relative)
	}
	if filepath.Base(relative) != "00000042.cast" {
		t.Errorf("录像文件名应为零填充的 00000042.cast，实际: %s", filepath.Base(relative))
	}
	if got, want := service.AbsRecordingPath(relative), filepath.Join(root, relative); got != want {
		t.Errorf("绝对路径解析错误\n实际: %s\n期望: %s", got, want)
	}
}
