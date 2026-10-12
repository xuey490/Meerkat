package monitor

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// DockerSnapshot 是 Docker 状态的当前值快照。
type DockerSnapshot struct {
	// Summary Docker 主机级汇总统计。
	Summary DockerSummary `json:"summary"`
	// Containers 各容器的最新资源使用，按名称升序。
	Containers []DockerContainerStat `json:"containers"`
	// CollectedAt 快照生成时间（UTC）。
	CollectedAt time.Time `json:"collectedAt"`
}

// DockerSummary 是 Docker 守护进程级别的汇总计数。
type DockerSummary struct {
	// TotalContainers 容器总数。
	TotalContainers int `json:"totalContainers"`
	// RunningContainers 运行中容器数。
	RunningContainers int `json:"runningContainers"`
	// PausedContainers 暂停容器数。
	PausedContainers int `json:"pausedContainers"`
	// StoppedContainers 停止容器数。
	StoppedContainers int `json:"stoppedContainers"`
	// TotalImages 镜像总数。
	TotalImages int `json:"totalImages"`
	// TotalNetworks 网络总数。
	TotalNetworks int `json:"totalNetworks"`
	// TotalVolumes 卷总数。
	TotalVolumes int `json:"totalVolumes"`
}

// DockerContainerStat 是单个容器的最新资源使用。
type DockerContainerStat struct {
	// Name 容器名。
	Name string `json:"name"`
	// Image 镜像名。
	Image string `json:"image"`
	// State 容器状态（来自 docker_container_status 的 container_status 标签）。
	State string `json:"state"`
	// CPUUsage CPU 使用率（百分比）。
	CPUUsage float64 `json:"cpuUsage"`
	// MemUsage 内存使用量（字节）。
	MemUsage float64 `json:"memUsage"`
	// MemLimit 内存限制（字节）。
	MemLimit float64 `json:"memLimit"`
	// MemPercent 内存使用率（百分比）。
	MemPercent float64 `json:"memPercent"`
	// NetRx 网络接收速率（字节/秒）。
	NetRx float64 `json:"netRx"`
	// NetTx 网络发送速率（字节/秒）。
	NetTx float64 `json:"netTx"`
	// BlkioRead 块设备读取速率（字节/秒）。
	BlkioRead float64 `json:"blkioRead"`
	// BlkioWrite 块设备写入速率（字节/秒）。
	BlkioWrite float64 `json:"blkioWrite"`
	// Restarts 重启次数。
	Restarts float64 `json:"restarts"`
}

// dockerMeasurements 是 Docker 快照需要查询的 InfluxDB measurement 列表。
var dockerMeasurements = []string{
	"docker",
	"docker_container_cpu",
	"docker_container_mem",
	"docker_container_net",
	"docker_container_blkio",
	"docker_container_status",
}

// DockerSnapshot 查询指定 Agent 的 Docker 状态快照。
//
// 参数 Parameters:
//   - agentID (string): 服务器 ID。
//
// 返回 Returns:
//   - snapshot (DockerSnapshot): Docker 快照；无 Docker 数据时返回零值结构。
//   - err (error): 资产不存在返回 gorm.ErrRecordNotFound；InfluxDB 不可用返回 ErrInfluxUnavailable。
func (s *Service) DockerSnapshot(agentID string) (DockerSnapshot, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" || agentID == "undefined" || agentID == "null" {
		return DockerSnapshot{}, ErrInvalidAgent
	}
	var agent Agent
	if err := s.db.First(&agent, "id = ?", agentID).Error; err != nil {
		return DockerSnapshot{}, err
	}
	if s.influx == nil {
		return DockerSnapshot{}, ErrInfluxUnavailable
	}

	series := make(map[string][]MetricPoint, len(dockerMeasurements))
	var (
		mutex    sync.Mutex
		wait     sync.WaitGroup
		failures int
		lastErr  error
	)
	for _, measurement := range dockerMeasurements {
		wait.Add(1)
		go func(name string) {
			defer wait.Done()
			points, err := s.Metrics(MetricQuery{
				Measurement: name,
				AgentID:     agentID,
				Start:       snapshotLookback,
				Limit:       snapshotLimit,
			})
			if err != nil {
				logSnapshotQueryFailure(name, agentID, err)
				mutex.Lock()
				failures++
				lastErr = err
				mutex.Unlock()
				return
			}
			mutex.Lock()
			series[name] = points
			mutex.Unlock()
		}(measurement)
	}
	wait.Wait()
	if failures == len(dockerMeasurements) && lastErr != nil {
		return DockerSnapshot{}, lastErr
	}
	return buildDockerSnapshot(series), nil
}

// buildDockerSnapshot 把原始数据点换算为 Docker 快照。
func buildDockerSnapshot(series map[string][]MetricPoint) DockerSnapshot {
	return DockerSnapshot{
		Summary:     buildDockerSummary(series["docker"]),
		Containers:  buildDockerContainerStats(series),
		CollectedAt: time.Now().UTC(),
	}
}

// buildDockerSummary 汇总 Docker 守护进程级别的计数。
func buildDockerSummary(points []MetricPoint) DockerSummary {
	var s DockerSummary
	if total, ok := latestValue(points, "n_containers"); ok {
		s.TotalContainers = int(total)
	}
	if running, ok := latestValue(points, "n_containers_running"); ok {
		s.RunningContainers = int(running)
	}
	if paused, ok := latestValue(points, "n_containers_paused"); ok {
		s.PausedContainers = int(paused)
	}
	if stopped, ok := latestValue(points, "n_containers_stopped"); ok {
		s.StoppedContainers = int(stopped)
	}
	if images, ok := latestValue(points, "n_images"); ok {
		s.TotalImages = int(images)
	}
	if networks, ok := latestValue(points, "n_networks"); ok {
		s.TotalNetworks = int(networks)
	}
	if volumes, ok := latestValue(points, "n_volumes"); ok {
		s.TotalVolumes = int(volumes)
	}
	return s
}

// buildDockerContainerStats 汇总各容器的最新资源使用。
func buildDockerContainerStats(series map[string][]MetricPoint) []DockerContainerStat {
	cpuPoints := series["docker_container_cpu"]
	memPoints := series["docker_container_mem"]
	netPoints := series["docker_container_net"]
	blkioPoints := series["docker_container_blkio"]
	statusPoints := series["docker_container_status"]

	// 收集所有出现过的容器名称。
	names := containerNames(cpuPoints, memPoints, netPoints, blkioPoints, statusPoints)

	stats := make([]DockerContainerStat, 0, len(names))
	for _, name := range names {
		stat := DockerContainerStat{Name: name}

		// CPU
		cpuScoped := filterTag(cpuPoints, "container_name", name)
		if v, ok := latestValue(cpuScoped, "usage_percent"); ok {
			stat.CPUUsage = clampPercent(v)
		}

		// Memory
		memScoped := filterTag(memPoints, "container_name", name)
		if v, ok := latestValue(memScoped, "usage"); ok {
			stat.MemUsage = v
		}
		if v, ok := latestValue(memScoped, "limit"); ok && v > 0 {
			stat.MemLimit = v
		}
		if stat.MemLimit > 0 {
			stat.MemPercent = clampPercent(stat.MemUsage / stat.MemLimit * 100)
		}

		// Network 速率（字节/秒）
		netScoped := filterTag(netPoints, "container_name", name)
		rxRates := rateByTag(netScoped, "rx_bytes", "network")
		txRates := rateByTag(netScoped, "tx_bytes", "network")
		for _, v := range rxRates {
			stat.NetRx += v
		}
		for _, v := range txRates {
			stat.NetTx += v
		}

		// BlkIO 速率（字节/秒）
		blkioScoped := filterTag(blkioPoints, "container_name", name)
		readRates := rateByTag(blkioScoped, "io_service_bytes_recursive_read", "device")
		writeRates := rateByTag(blkioScoped, "io_service_bytes_recursive_write", "device")
		for _, v := range readRates {
			stat.BlkioRead += v
		}
		for _, v := range writeRates {
			stat.BlkioWrite += v
		}

		// Status
		statusScoped := filterTag(statusPoints, "container_name", name)
		if v, ok := latestValue(statusScoped, "restarts"); ok {
			stat.Restarts = v
		}
		// 状态取最新点的 container_status 标签
		if len(statusScoped) > 0 {
			latest := statusScoped[0]
			for _, p := range statusScoped {
				if p.Time.After(latest.Time) {
					latest = p
				}
			}
			if s, ok := latest.Tags["container_status"].(string); ok && s != "" {
				stat.State = s
			}
		}
		// 镜像名取 mem 或 cpu 点的 container_image 标签
		stat.Image = firstContainerImage(cpuScoped, memScoped)

		stats = append(stats, stat)
	}

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Name < stats[j].Name
	})
	return stats
}

// containerNames 从多组数据点中提取所有容器名称（去重、升序）。
func containerNames(pointGroups ...[]MetricPoint) []string {
	seen := make(map[string]bool)
	for _, points := range pointGroups {
		for _, p := range points {
			name := tagValue(p, "container_name")
			if name != "" {
				seen[name] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// firstContainerImage 从数据点中读取 container_image 标签的首个非空值。
func firstContainerImage(pointGroups ...[]MetricPoint) string {
	for _, points := range pointGroups {
		for _, p := range points {
			img := tagValue(p, "container_image")
			if img != "" {
				return img
			}
		}
	}
	return ""
}
