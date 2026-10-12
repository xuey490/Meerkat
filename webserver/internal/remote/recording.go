package remote

import (
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/company/monitor-webserver/internal/apperr"
)

// ResolveRecording 把录像相对路径解析为本进程视角的绝对路径。
//
// 录像路径来自数据库，理论上可控，但审计功能会把它拼成文件系统路径，
// 因此这里做两道校验：先拒绝绝对路径与含 ".." 的输入，再在拼接后用 Rel 复核
// 结果仍位于基准目录之内。这与现有 SSH 录像回放接口是同一套防护思路。
//
// 参数 Parameters:
//   - relative (string): 录像相对路径（以 / 分隔）。
//
// 返回 Returns:
//   - absPath (string): 绝对路径。
//   - err (error): 目录未配置或路径越界时返回非 nil。
func (s *Service) ResolveRecording(relative string) (string, error) {
	base := strings.TrimSpace(s.guac.LocalRecordingDir)
	if base == "" {
		return "", apperr.Unavailable("远程桌面录像目录未配置（guac.local_recording_dir）", nil)
	}
	if strings.TrimSpace(relative) == "" {
		return "", apperr.Invalid("录像路径为空")
	}

	cleaned := filepath.Clean(filepath.FromSlash(relative))
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", apperr.Invalid("录像路径非法")
	}

	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	full := filepath.Join(baseAbs, cleaned)
	relativeToBase, err := filepath.Rel(baseAbs, full)
	if err != nil || relativeToBase == ".." || strings.HasPrefix(relativeToBase, ".."+string(filepath.Separator)) {
		return "", apperr.Invalid("录像路径越界")
	}
	// 再解析一次软链接：词法校验挡不住「录像目录里放一个指向外部的符号链接」。
	// 文件尚不存在时跳过（首次使用还没产出录像，此时也谈不上越界）。
	if resolved, evalErr := filepath.EvalSymlinks(full); evalErr == nil {
		resolvedRel, relErr := filepath.Rel(baseAbs, resolved)
		if relErr != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) {
			return "", apperr.Invalid("录像路径越界（符号链接）")
		}
	}
	return full, nil
}

// OpenRecording 打开录像文件供下载或回放。
//
// 参数 Parameters:
//   - relative (string): 录像相对路径。
//
// 返回 Returns:
//   - file (*os.File): 已打开的文件（调用方负责关闭）。
//   - info (fs.FileInfo): 文件信息，用于设置 Content-Length 与文件名。
//   - err (error): 路径非法或文件不存在时返回非 nil。
func (s *Service) OpenRecording(relative string) (io.ReadSeekCloser, fs.FileInfo, error) {
	absPath, err := s.ResolveRecording(relative)
	if err != nil {
		return nil, nil, err
	}
	file, err := os.Open(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, apperr.NotFound("录像文件不存在或已被清理")
		}
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	return file, info, nil
}

// RemoveSessionRecording 删除一条 SSH 会话的录像文件（尽力而为，失败只告警）。
//
// 路径来自数据库，因此这里做与读取侧同强度的校验（拒绝绝对路径与 ".."，拼接后再用 Rel
// 复核落在录像目录内）——「清理记录」不该成为越权删文件的口子。
//
// 参数 Parameters:
//   - relative (string): 录像相对路径；为空表示该会话没有录像。
func (s *Service) RemoveSessionRecording(relative string) {
	value := strings.TrimSpace(relative)
	if value == "" || filepath.IsAbs(filepath.FromSlash(value)) || strings.Contains(value, "..") {
		return
	}
	s.removeFileWithin(s.recordingDir, value)
}

// RemoveGuacRecording 删除一条远程桌面（.guac）会话的录像文件。
//
// 复用 ResolveRecording 的路径校验（含符号链接复核），非法路径只告警不删除。
//
// 参数 Parameters:
//   - relative (string): 录像相对路径；为空表示该会话没有录像。
func (s *Service) RemoveGuacRecording(relative string) {
	if strings.TrimSpace(relative) == "" {
		return
	}
	absPath, err := s.ResolveRecording(relative)
	if err != nil {
		slog.Warn("远程桌面录像路径非法，跳过删除", "path", relative, "error", err)
		return
	}
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		slog.Warn("删除远程桌面录像失败", "path", absPath, "error", err)
	}
}

// removeFileWithin 删除基准目录内的某个相对文件。
//
// 参数 Parameters:
//   - base (string): 基准目录（绝对路径）。
//   - relative (string): 相对路径。
func (s *Service) removeFileWithin(base, relative string) {
	if strings.TrimSpace(base) == "" {
		return
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return
	}
	full := filepath.Join(baseAbs, filepath.Clean(filepath.FromSlash(relative)))
	relativeToBase, err := filepath.Rel(baseAbs, full)
	if err != nil || relativeToBase == ".." || strings.HasPrefix(relativeToBase, ".."+string(filepath.Separator)) {
		slog.Warn("会话录像路径越界，跳过删除", "path", relative)
		return
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		slog.Warn("删除会话录像失败", "path", full, "error", err)
	}
}

// CleanupRecordings 删除超过保留期的录像文件，并清空对应会话的录像引用。
//
// 由计划任务定期调用。保留天数由 guac.retention_days 决定，<=0 表示不清理。
//
// 参数 Parameters:
//   - retentionDays (int): 保留天数。
//
// 返回 Returns:
//   - removed (int): 删除的文件数。
//   - err (error): 遍历失败时返回非 nil（单文件删除失败只记日志，不影响整体）。
func (s *Service) CleanupRecordings(retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	base := strings.TrimSpace(s.guac.LocalRecordingDir)
	if base == "" {
		return 0, nil
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return 0, err
	}
	if _, statErr := os.Stat(baseAbs); os.IsNotExist(statErr) {
		return 0, nil
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	removed := 0
	removedRelative := make([]string, 0, 32)

	walkErr := filepath.WalkDir(baseAbs, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil || info.ModTime().After(cutoff) {
			return nil
		}
		if removeErr := os.Remove(path); removeErr != nil {
			slog.Warn("删除过期远程桌面录像失败", "path", path, "error", removeErr)
			return nil
		}
		removed++
		if relative, relErr := filepath.Rel(baseAbs, path); relErr == nil {
			removedRelative = append(removedRelative, filepath.ToSlash(relative))
		}
		return nil
	})
	if walkErr != nil {
		return removed, walkErr
	}

	// 清空被删录像的会话引用：否则审计列表仍显示「回放」，点开才发现文件没了。
	if len(removedRelative) > 0 {
		if err := s.db.Model(&RDPSession{}).
			Where("recording_path IN ?", removedRelative).
			Update("recording_path", "").Error; err != nil {
			slog.Warn("清空已删除录像的会话引用失败", "error", err)
		}
	}
	pruneEmptyDirs(baseAbs)
	return removed, nil
}

// pruneEmptyDirs 清理录像目录下的空日期子目录（尽力而为，失败不影响清理结果）。
func pruneEmptyDirs(baseAbs string) {
	entries, err := os.ReadDir(baseAbs)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(baseAbs, entry.Name())
		children, readErr := os.ReadDir(dir)
		if readErr != nil || len(children) > 0 {
			continue
		}
		_ = os.Remove(dir)
	}
}
