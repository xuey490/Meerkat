package systeminfo

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Snapshot struct {
	OSVersion          string
	SystemTime         time.Time
	MemoryTotalBytes   uint64
	MemoryUsedBytes    uint64
	ProcessCount       uint64
	ListeningPortCount uint64
}

func Collect(ctx context.Context) Snapshot {
	snapshot := Snapshot{
		OSVersion:  runtime.GOOS + " " + runtime.GOARCH,
		SystemTime: time.Now().UTC(),
	}
	if runtime.GOOS == "windows" {
		snapshot.OSVersion = command(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
			"$o=Get-CimInstance Win32_OperatingSystem; Write-Output ('Windows ' + $o.Version)")
		memory := command(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
			"$m=Get-CimInstance Win32_OperatingSystem; Write-Output ($m.TotalVisibleMemorySize*1024); Write-Output (($m.TotalVisibleMemorySize-$m.FreePhysicalMemory)*1024)")
		fields := strings.Fields(memory)
		if len(fields) >= 2 {
			snapshot.MemoryTotalBytes, _ = strconv.ParseUint(fields[0], 10, 64)
			snapshot.MemoryUsedBytes, _ = strconv.ParseUint(fields[1], 10, 64)
		}
		snapshot.ProcessCount = uint64(countLines(command(ctx, "tasklist", "/FO", "CSV", "/NH")))
		snapshot.ListeningPortCount = uint64(countContains(command(ctx, "netstat", "-ano"), "LISTENING"))
	} else {
		snapshot.OSVersion = strings.TrimSpace(command(ctx, "uname", "-sr"))
		meminfo, _ := os.ReadFile("/proc/meminfo")
		values := map[string]uint64{}
		for _, line := range strings.Split(string(meminfo), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				value, _ := strconv.ParseUint(fields[1], 10, 64)
				values[strings.TrimSuffix(fields[0], ":")] = value * 1024
			}
		}
		snapshot.MemoryTotalBytes = values["MemTotal"]
		if snapshot.MemoryTotalBytes > values["MemAvailable"] {
			snapshot.MemoryUsedBytes = snapshot.MemoryTotalBytes - values["MemAvailable"]
		}
		snapshot.ProcessCount = uint64(countLines(command(ctx, "ps", "-e", "-o", "pid=", "--no-headers")))
		ports := command(ctx, "ss", "-lnt")
		if ports == "" {
			ports = command(ctx, "netstat", "-lnt")
		}
		portLines := countLines(ports)
		if portLines > 0 {
			portLines--
		}
		snapshot.ListeningPortCount = uint64(portLines)
	}
	if snapshot.OSVersion == "" {
		snapshot.OSVersion = runtime.GOOS + " " + runtime.GOARCH
	}
	return snapshot
}

func command(ctx context.Context, name string, args ...string) string {
	commandCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(commandCtx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func countLines(value string) int {
	if strings.TrimSpace(value) == "" {
		return 0
	}
	return len(strings.Split(strings.TrimSpace(value), "\n"))
}

func countContains(value, token string) int {
	count := 0
	for _, line := range strings.Split(value, "\n") {
		if strings.Contains(line, token) {
			count++
		}
	}
	return count
}
