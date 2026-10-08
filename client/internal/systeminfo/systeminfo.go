package systeminfo

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Process struct {
	PID    int    `json:"pid"`
	Name   string `json:"name"`
	Memory uint64 `json:"memory_bytes"`
}

type Temperature struct {
	Name    string  `json:"name"`
	Celsius float64 `json:"celsius"`
}

type Port struct {
	LocalAddress string `json:"local_address"`
	PID          int    `json:"pid"`
}

type Snapshot struct {
	OSVersion          string        `json:"os_version"`
	SystemTime         time.Time     `json:"system_time"`
	MemoryTotalBytes   uint64        `json:"memory_total_bytes"`
	MemoryUsedBytes    uint64        `json:"memory_used_bytes"`
	ProcessCount       uint64        `json:"process_count"`
	ListeningPortCount uint64        `json:"listening_port_count"`
	Load1              float64       `json:"load1"`
	UptimeSeconds      uint64        `json:"uptime_seconds"`
	CPUModel           string        `json:"cpu_model"`
	PhysicalCPUCores   uint64        `json:"physical_cpu_cores"`
	LogicalCPUCores    uint64        `json:"logical_cpu_cores"`
	HardwareTempC      float64       `json:"hardware_temperature_c"`
	Processes          []Process     `json:"processes,omitempty"`
	Temperatures       []Temperature `json:"temperatures,omitempty"`
	ListeningPorts     []Port        `json:"listening_ports,omitempty"`
	LastError          string        `json:"last_error,omitempty"`
}

type Monitor struct {
	mu    sync.RWMutex
	state Snapshot
}

// PrimaryIPv4 returns the most suitable active non-loopback IPv4 address.
func PrimaryIPv4() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	type candidate struct {
		ip    string
		score int
	}
	var best candidate
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		interfaceScore := 0
		for _, excluded := range []string{"docker", "vEthernet", "virtual", "hyper-v", "wsl"} {
			if strings.Contains(name, strings.ToLower(excluded)) {
				interfaceScore -= 25
			}
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			ip = ip.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			score := interfaceScore
			if ip.IsPrivate() {
				score += 50
			} else {
				score += 200 // public IP preferred
			}
			if best.ip == "" || score > best.score {
				best = candidate{ip: ip.String(), score: score}
			}
		}
	}
	return best.ip
}

func New() *Monitor {
	return &Monitor{state: Snapshot{SystemTime: time.Now().UTC()}}
}

func (m *Monitor) Run(ctx context.Context) {
	m.Refresh(ctx)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Refresh(ctx)
		}
	}
}

func (m *Monitor) Refresh(ctx context.Context) {
	snapshot := collect(ctx)
	m.mu.Lock()
	m.state = snapshot
	m.mu.Unlock()
}

func (m *Monitor) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snapshot := m.state
	snapshot.Processes = append([]Process(nil), snapshot.Processes...)
	snapshot.ListeningPorts = append([]Port(nil), snapshot.ListeningPorts...)
	return snapshot
}

func collect(ctx context.Context) Snapshot {
	snapshot := Snapshot{
		OSVersion:  runtime.GOOS + " " + runtime.GOARCH,
		SystemTime: time.Now().UTC(),
	}
	if runtime.GOOS == "windows" {
		snapshot.OSVersion = command(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
			"$o=Get-CimInstance Win32_OperatingSystem; Write-Output ('Windows ' + $o.Version)")
		collectWindowsMemory(ctx, &snapshot)
		collectWindowsHardware(ctx, &snapshot)
		snapshot.Processes = windowsProcesses(ctx)
		snapshot.ListeningPorts = windowsPorts(ctx)
	} else {
		snapshot.OSVersion = strings.TrimSpace(command(ctx, "uname", "-sr"))
		collectLinuxMemory(&snapshot)
		collectLinuxHardware(&snapshot)
		snapshot.Processes = linuxProcesses(ctx)
		snapshot.ListeningPorts = linuxPorts(ctx)
	}
	snapshot.ProcessCount = uint64(len(snapshot.Processes))
	snapshot.ListeningPortCount = uint64(len(snapshot.ListeningPorts))
	if uptime, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(uptime))
		if len(fields) > 0 {
			if seconds, parseErr := strconv.ParseFloat(fields[0], 64); parseErr == nil {
				snapshot.UptimeSeconds = uint64(seconds)
			}
		}
	}
	if load, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(load))
		if len(fields) > 0 {
			snapshot.Load1, _ = strconv.ParseFloat(fields[0], 64)
		}
	}
	if snapshot.OSVersion == "" {
		snapshot.OSVersion = runtime.GOOS + " " + runtime.GOARCH
	}
	return snapshot
}

func collectWindowsHardware(ctx context.Context, snapshot *Snapshot) {
	processorJSON := command(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		"@(Get-CimInstance Win32_Processor | Select-Object -First 1 Name,NumberOfCores,NumberOfLogicalProcessors) | ConvertTo-Json -Compress")
	var processor struct {
		Name                      string
		NumberOfCores             uint64
		NumberOfLogicalProcessors uint64
	}
	if json.Unmarshal([]byte(processorJSON), &processor) == nil {
		snapshot.CPUModel = strings.TrimSpace(processor.Name)
		snapshot.PhysicalCPUCores = processor.NumberOfCores
		snapshot.LogicalCPUCores = processor.NumberOfLogicalProcessors
	}
	if snapshot.LogicalCPUCores == 0 {
		snapshot.LogicalCPUCores = uint64(runtime.NumCPU())
	}

	temperatureJSON := command(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		"try { @(Get-CimInstance -Namespace root/wmi -ClassName MSAcpi_ThermalZoneTemperature | ForEach-Object { [pscustomobject]@{Name=$_.InstanceName; Celsius=(($_.CurrentTemperature / 10) - 273.15)} }) | ConvertTo-Json -Compress } catch { '[]' }")
	var temperatures []Temperature
	if json.Unmarshal([]byte(temperatureJSON), &temperatures) == nil {
		snapshot.Temperatures = temperatures
		for _, temperature := range temperatures {
			if temperature.Celsius > snapshot.HardwareTempC {
				snapshot.HardwareTempC = temperature.Celsius
			}
		}
	}
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

func collectWindowsMemory(ctx context.Context, snapshot *Snapshot) {
	output := command(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		"$m=Get-CimInstance Win32_OperatingSystem; Write-Output ($m.TotalVisibleMemorySize*1024); Write-Output (($m.TotalVisibleMemorySize-$m.FreePhysicalMemory)*1024)")
	lines := strings.Fields(output)
	if len(lines) >= 2 {
		snapshot.MemoryTotalBytes, _ = strconv.ParseUint(lines[0], 10, 64)
		snapshot.MemoryUsedBytes, _ = strconv.ParseUint(lines[1], 10, 64)
	}
}

func collectLinuxHardware(snapshot *Snapshot) {
	// CPU model
	if cpuinfo, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(cpuinfo), "\n") {
			if strings.HasPrefix(line, "model name\t:") || strings.HasPrefix(line, "model name:") {
				snapshot.CPUModel = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
				break
			}
		}
	}
	// Physical cores: unique physical id × cores per socket; fallback to cpu count
	if snapshot.PhysicalCPUCores == 0 {
		physicalIDs := map[string]struct{}{}
		coresPerSocket := uint64(1)
		if cpuinfo, err := os.ReadFile("/proc/cpuinfo"); err == nil {
			for _, line := range strings.Split(string(cpuinfo), "\n") {
				if strings.HasPrefix(line, "physical id\t:") || strings.HasPrefix(line, "physical id:") {
					id := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
					physicalIDs[id] = struct{}{}
				}
				if strings.HasPrefix(line, "cpu cores\t:") || strings.HasPrefix(line, "cpu cores:") {
					if v, err := strconv.ParseUint(strings.TrimSpace(strings.SplitN(line, ":", 2)[1]), 10, 64); err == nil {
						coresPerSocket = v
					}
				}
			}
		}
		if len(physicalIDs) > 0 {
			snapshot.PhysicalCPUCores = uint64(len(physicalIDs)) * coresPerSocket
		}
	}
	// Logical cores
	if snapshot.LogicalCPUCores == 0 {
		snapshot.LogicalCPUCores = uint64(runtime.NumCPU())
	}
	// Temperatures
	if temps := linuxTemperatures(); len(temps) > 0 {
		snapshot.Temperatures = temps
		for _, t := range temps {
			if t.Celsius > snapshot.HardwareTempC {
				snapshot.HardwareTempC = t.Celsius
			}
		}
	}
}

func linuxTemperatures() []Temperature {
	var temps []Temperature
	// hwmon paths
	entries, _ := os.ReadDir("/sys/class/hwmon")
	for _, entry := range entries {
		dir := "/sys/class/hwmon/" + entry.Name()
		labelBytes, _ := os.ReadFile(dir + "/name")
		label := strings.TrimSpace(string(labelBytes))
		if label == "" {
			label = entry.Name()
		}
		for i := 0; i < 10; i++ {
			inputPath := fmt.Sprintf("%s/temp%d_input", dir, i)
			data, err := os.ReadFile(inputPath)
			if err != nil {
				continue
			}
			milli, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			if err != nil {
				continue
			}
			temps = append(temps, Temperature{
				Name:    fmt.Sprintf("%s_temp%d", label, i),
				Celsius: float64(milli) / 1000.0,
			})
		}
	}
	return temps
}

func collectLinuxMemory(snapshot *Snapshot) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer file.Close()
	values := map[string]uint64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			value, _ := strconv.ParseUint(fields[1], 10, 64)
			values[strings.TrimSuffix(fields[0], ":")] = value * 1024
		}
	}
	snapshot.MemoryTotalBytes = values["MemTotal"]
	available := values["MemAvailable"]
	if snapshot.MemoryTotalBytes > available {
		snapshot.MemoryUsedBytes = snapshot.MemoryTotalBytes - available
	}
}

func windowsProcesses(ctx context.Context) []Process {
	output := command(ctx, "tasklist", "/FO", "CSV", "/NH")
	var processes []Process
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\",\"")
		if len(fields) < 5 {
			continue
		}
		pid, err := strconv.Atoi(strings.Trim(fields[1], "\""))
		if err != nil {
			continue
		}
		memory := strings.NewReplacer(",", "", ".", "").Replace(strings.Trim(fields[4], "\""))
		memoryFields := strings.Fields(memory)
		if len(memoryFields) == 0 {
			continue
		}
		memoryValue, _ := strconv.ParseUint(memoryFields[0], 10, 64)
		processes = append(processes, Process{PID: pid, Name: strings.Trim(fields[0], "\""), Memory: memoryValue * 1024})
	}
	return processes
}

func linuxProcesses(ctx context.Context) []Process {
	output := command(ctx, "ps", "-eo", "pid,comm,rss", "--no-headers")
	var processes []Process
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		memory, _ := strconv.ParseUint(fields[2], 10, 64)
		processes = append(processes, Process{PID: pid, Name: fields[1], Memory: memory * 1024})
	}
	return processes
}

func windowsPorts(ctx context.Context) []Port {
	return parseNetstat(command(ctx, "netstat", "-ano"), "LISTENING")
}

func linuxPorts(ctx context.Context) []Port {
	output := command(ctx, "ss", "-lntp")
	if output == "" {
		output = command(ctx, "netstat", "-lnt")
	}
	return parseNetstat(output, "LISTEN")
}

func parseNetstat(output, state string) []Port {
	var ports []Port
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, state) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid := 0
		for _, field := range fields {
			if strings.Contains(field, "pid=") {
				pidText := strings.TrimPrefix(field[strings.Index(field, "pid="):], "pid=")
				pidText = strings.Split(pidText, ",")[0]
				pid, _ = strconv.Atoi(pidText)
			}
		}
		local := fields[len(fields)-2]
		if runtime.GOOS == "windows" && len(fields) >= 2 {
			local = fields[1]
			pid, _ = strconv.Atoi(fields[len(fields)-1])
		}
		ports = append(ports, Port{LocalAddress: local, PID: pid})
	}
	return ports
}
