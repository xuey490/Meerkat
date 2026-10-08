//go:build windows

package app

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// enableConsole 在 Windows 控制台启用 ANSI 虚拟终端序列，使颜色输出生效。
func enableConsole() {
	handle := windows.Handle(os.Stdout.Fd())
	enableVT(handle)
}

// enableVT 开启指定控制台句柄的虚拟终端处理。
//
// 参数 Parameters:
//   - handle (windows.Handle): 控制台句柄。
func enableVT(handle windows.Handle) {
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return
	}
	const enableVirtualTerminalProcessing = 0x0004
	_ = windows.SetConsoleMode(handle, mode|enableVirtualTerminalProcessing)
	_ = syscall.SetEnvironmentVariable // 保留 syscall 引用，避免空导入
	_ = unsafe.Pointer(nil)
}

// hostOSName 返回带版本号的操作系统名称，例如 Windows 10.0.26100。
//
// 返回 Returns:
//   - name (string): 操作系统名称。
func hostOSName() string {
	return "Windows " + windowsVersion()
}

// windowsVersion 通过 RtlGetVersion 获取真实系统版本（GetVersionEx 会被兼容层伪造）。
//
// 返回 Returns:
//   - version (string): 形如 10.0.26100。
func windowsVersion() string {
	dll, err := windows.LoadDLL("ntdll.dll")
	if err != nil {
		return runtime.GOOS
	}
	proc, err := dll.FindProc("RtlGetVersion")
	if err != nil {
		return runtime.GOOS
	}
	var info struct {
		OSVersionInfoSize uint32
		MajorVersion      uint32
		MinorVersion      uint32
		BuildNumber       uint32
		PlatformID        uint32
		CSDVersion        [128]uint16
	}
	info.OSVersionInfoSize = uint32(unsafe.Sizeof(info))
	if _, _, _ = proc.Call(uintptr(unsafe.Pointer(&info))); info.MajorVersion == 0 {
		return runtime.GOOS
	}
	return formatVersion(info.MajorVersion, info.MinorVersion, info.BuildNumber)
}
