//go:build !windows

package app

import (
	"os"
	"runtime"
	"strings"
)

// enableConsole 在非 Windows 平台无需额外处理（终端默认支持 ANSI）。
func enableConsole() {}

// hostOSName 返回操作系统名称与版本，例如 Linux 6.8.0。
//
// 返回 Returns:
//   - name (string): 操作系统名称。
func hostOSName() string {
	osName := strings.Title(runtime.GOOS) //nolint:staticcheck // 仅用于展示，无需 i18n
	if release := kernelRelease(); release != "" {
		return osName + " " + release
	}
	return osName
}

// kernelRelease 读取 /proc/sys/kernel/osrelease，失败时返回空串。
func kernelRelease() string {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
