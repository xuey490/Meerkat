// 本文件提供附件模块的本地文件存储能力：把上传流写入「按日期分层」的目录，
// 并计算出可直接落库的存储路径、访问地址、大小、类型与指纹。
package util

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// StoredFile 描述一次本地落盘的结果。
type StoredFile struct {
	// StoragePath 相对上传根目录的存储路径（upload/2026/10/06/x.jpg）。
	StoragePath string
	// URL 可直接访问的地址（/upload/2026/10/06/x.jpg）。
	URL string
	// ObjectName 落盘文件名（不含目录）。
	ObjectName string
	// Suffix 文件后缀，不含点。
	Suffix string
	// SizeByte 字节数。
	SizeByte int64
	// SizeInfo 可读大小（如 8.57KB）。
	SizeInfo string
	// MimeType 资源类型。
	MimeType string
	// Hash 内容 MD5。
	Hash string
}

// SaveLocalFile 把上传内容写入 <rootDir>/<yyyy>/<MM>/<dd>/ 并返回落盘信息。
//
// 参数 Parameters:
//   - rootDir (string): 上传根目录（如 webserver/upload）。
//   - urlPrefix (string): 静态访问前缀（如 /upload）；为空时按 "/upload" 处理。
//   - originalName (string): 客户端原始文件名，仅用于取后缀与展示。
//   - src (io.Reader): 上传内容；大小上限由调用方（HTTP 层）负责校验。
//
// 返回 Returns:
//   - stored (StoredFile): 落盘结果。
//   - err (error): 读取失败或写盘失败时返回原始错误。
func SaveLocalFile(rootDir, urlPrefix, originalName string, src io.Reader) (StoredFile, error) {
	data, err := io.ReadAll(src)
	if err != nil {
		return StoredFile{}, err
	}
	now := time.Now()
	dateParts := []string{now.Format("2006"), now.Format("01"), now.Format("02")}
	dir := filepath.Join(append([]string{rootDir}, dateParts...)...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return StoredFile{}, err
	}

	suffix := SuffixOf(originalName)
	objectName := fmt.Sprintf("%d_%s%s", now.UnixMilli(), randomHex(8), DotSuffix(suffix))
	if err := os.WriteFile(filepath.Join(dir, objectName), data, 0o644); err != nil {
		return StoredFile{}, err
	}

	sum := md5.Sum(data)
	// storage_path 与 url 统一使用斜杠分隔，避免 Windows 反斜杠落库后难以拼接。
	storagePath := strings.Join(append([]string{path.Base(filepath.ToSlash(rootDir))}, dateParts...), "/") +
		"/" + objectName
	prefix := strings.TrimRight(strings.TrimSpace(urlPrefix), "/")
	if prefix == "" {
		prefix = "/upload"
	}
	return StoredFile{
		StoragePath: storagePath,
		URL:         prefix + "/" + strings.Join(dateParts, "/") + "/" + objectName,
		ObjectName:  objectName,
		Suffix:      suffix,
		SizeByte:    int64(len(data)),
		SizeInfo:    HumanSize(int64(len(data))),
		MimeType:    DetectMimeType(data, originalName),
		Hash:        hex.EncodeToString(sum[:]),
	}, nil
}

// RemoveLocalFile 删除 storage_path 指向的物理文件（不存在时视为成功）。
//
// 参数 Parameters:
//   - rootDir (string): 上传根目录。
//   - storagePath (string): 落库的存储路径（upload/2026/10/06/x.jpg）。
//
// 返回 Returns:
//   - err (error): 删除失败（除「文件不存在」外）时返回原始错误。
func RemoveLocalFile(rootDir, storagePath string) error {
	filePath := LocalFilePath(rootDir, storagePath)
	if filePath == "" {
		return nil
	}
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// LocalFilePath 把落库的 storage_path 还原为物理路径。
//
// storage_path 的首段是上传根目录名（upload）、其余为日期目录与文件名，
// 因此这里丢弃首段后再拼接到 rootDir，避免 rootDir 为绝对路径时重复拼接。
//
// 参数 Parameters:
//   - rootDir (string): 上传根目录。
//   - storagePath (string): 落库的存储路径。
//
// 返回 Returns:
//   - filePath (string): 物理路径；storagePath 为空时返回空串。
func LocalFilePath(rootDir, storagePath string) string {
	trimmed := strings.Trim(strings.TrimSpace(filepath.ToSlash(storagePath)), "/")
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) > 1 {
		parts = parts[1:]
	}
	return filepath.Join(append([]string{rootDir}, parts...)...)
}

// SafeFileName 清洗客户端文件名：去掉目录与非法字符，并限制长度。
//
// 参数 Parameters:
//   - name (string): 客户端提交的文件名。
//
// 返回 Returns:
//   - safe (string): 可安全落库与展示的文件名；输入为空时返回 "未命名文件"。
func SafeFileName(name string) string {
	trimmed := strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	cleaned := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, trimmed)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return "未命名文件"
	}
	if len([]rune(cleaned)) > 200 {
		runes := []rune(cleaned)
		return string(runes[:200])
	}
	return cleaned
}

// SuffixOf 取文件后缀（不含点，小写）。
//
// 参数 Parameters:
//   - name (string): 文件名。
//
// 返回 Returns:
//   - suffix (string): 后缀；无后缀时返回空串。
func SuffixOf(name string) string {
	ext := strings.TrimPrefix(filepath.Ext(filepath.Base(strings.TrimSpace(name))), ".")
	if len(ext) > 10 {
		return strings.ToLower(ext[:10])
	}
	return strings.ToLower(ext)
}

// DotSuffix 把后缀还原为带点的形式。
//
// 参数 Parameters:
//   - suffix (string): 不含点的后缀。
//
// 返回 Returns:
//   - dotted (string): 带点的后缀；输入为空时返回空串。
func DotSuffix(suffix string) string {
	if suffix == "" {
		return ""
	}
	return "." + suffix
}

// DetectMimeType 识别上传内容的资源类型：优先按内容嗅探，嗅探不出时按扩展名推断。
//
// 参数 Parameters:
//   - data ([]byte): 文件内容（可为空）。
//   - name (string): 文件名，用于按扩展名兜底。
//
// 返回 Returns:
//   - mimeType (string): 资源类型；均无法判断时返回 application/octet-stream。
func DetectMimeType(data []byte, name string) string {
	sniffLen := len(data)
	if sniffLen > 512 {
		sniffLen = 512
	}
	if sniffLen > 0 {
		if detected := http.DetectContentType(data[:sniffLen]); detected != "" &&
			!strings.HasPrefix(detected, "application/octet-stream") && !strings.HasPrefix(detected, "text/plain") {
			return detected
		}
	}
	if suffix := SuffixOf(name); suffix != "" {
		if byExt := mime.TypeByExtension("." + suffix); byExt != "" {
			return strings.Split(byExt, ";")[0]
		}
	}
	if sniffLen > 0 {
		return http.DetectContentType(data[:sniffLen])
	}
	return "application/octet-stream"
}

// HumanSize 把字节数格式化为可读大小（与 init.sql 样例数据一致，保留两位小数）。
//
// 参数 Parameters:
//   - size (int64): 字节数。
//
// 返回 Returns:
//   - text (string): 形如 8776B / 8.57KB / 677.04KB 的文本。
func HumanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}
	value := float64(size)
	for _, suffix := range []string{"KB", "MB", "GB", "TB"} {
		value /= unit
		if value < unit || suffix == "TB" {
			return fmt.Sprintf("%.2f%s", value, suffix)
		}
	}
	return fmt.Sprintf("%dB", size)
}

// randomHex 生成 n 字节随机数的十六进制文本。
//
// 参数 Parameters:
//   - n (int): 字节数。
//
// 返回 Returns:
//   - text (string): 十六进制文本；随机源异常时退化为时间戳片段。
func randomHex(n int) string {
	buffer := make([]byte, n)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())[:n*2]
	}
	return hex.EncodeToString(buffer)
}
