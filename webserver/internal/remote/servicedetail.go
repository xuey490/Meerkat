package remote

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ServiceDetail 是一个 systemd 服务单元的详细信息。
//
// 字段只取「一眼能判断服务状态」的那些：运行状态、自启、主进程、内存、单元文件与启动命令。
// 刻意不做 systemctl show 的全量透传——它有几百个属性，全丢给界面等于把噪声当功能。
type ServiceDetail struct {
	// Unit 单元名。
	Unit string `json:"unit"`
	// Description 单元描述。
	Description string `json:"description"`
	// LoadState 加载状态：loaded / not-found / masked / error。
	LoadState string `json:"loadState"`
	// ActiveState 运行状态：active / inactive / failed / activating。
	ActiveState string `json:"activeState"`
	// SubState 细分状态：running / exited / dead / failed。
	SubState string `json:"subState"`
	// UnitFileState 自启状态：enabled / disabled / static / masked；取不到时为空串。
	UnitFileState string `json:"unitFileState"`
	// MainPID 主进程号；0 表示当前没有主进程（服务已停止）。
	MainPID int `json:"mainPID"`
	// MemoryBytes 当前内存占用（字节）；未知时为 0。
	MemoryBytes uint64 `json:"memoryBytes"`
	// MemoryUnlimited 内存是否不受限（MemoryCurrent=infinity）。
	//
	// 要单独区分出来：它和「占用 0 字节」在界面上必须长得不一样，
	// 否则「未限制」会被显示成「内存 0 B」，看起来像坏了。
	MemoryUnlimited bool `json:"memoryUnlimited"`
	// FragmentPath 单元文件路径；未落盘（generated）时为空串。
	FragmentPath string `json:"fragmentPath"`
	// ExecStart 启动命令（systemd 的原始描述串）。
	ExecStart string `json:"execStart"`
	// StartedAt 主进程启动时间，格式 2006-01-02 15:04:05；未知时为空串。
	StartedAt string `json:"startedAt"`
	// Restarts 已自动重启次数（NRestarts）。
	Restarts int `json:"restarts"`
	// Protected 是否禁止在界面上操作（与列表同一判定，见 ServiceOperationBlocked）。
	Protected bool `json:"protected"`
	// BlockReason 禁止操作的原因；可操作时为空串。
	BlockReason string `json:"blockReason"`
}

// serviceDetailProperties 是 serviceDetailCommand 要取的属性列表。
//
// 显式列出而不是取全量：systemctl show 默认输出几百行，多取一行就多一处解析面。
var serviceDetailProperties = []string{
	"Description", "LoadState", "ActiveState", "SubState", "UnitFileState",
	"MainPID", "MemoryCurrent", "FragmentPath", "ExecStart", "ExecMainStartTimestamp", "NRestarts",
}

// serviceDetailCommand 生成查询单元详情的命令。
//
// systemctl show 的输出本身就是 `Key=Value`（每行一个属性），可直接复用 parseKeyValueLines，
// 不必自己定义分隔格式。--no-pager 防止远端分页器挂住进程。
func serviceDetailCommand(unit string) string {
	parts := make([]string, 0, len(serviceDetailProperties))
	for _, property := range serviceDetailProperties {
		parts = append(parts, "-p "+property)
	}
	return "systemctl show --no-pager " + strings.Join(parts, " ") + " " + unit
}

// serviceSystemdTimestampLayout 是 systemctl 输出时间戳的格式，如 "Sun 2026-10-11 02:57:22 PDT"。
//
// 末尾的时区缩写（PDT/CST）与星期都被丢弃：面板要的是「什么时候起的」，
// 时区在「主机状态」里已经单独展示过，重复带缩写反而更容易引起误读。
const serviceSystemdTimestampLayout = "Mon 2006-01-02 15:04:05 MST"

// ServiceDetail 通过 SSH 查询指定 systemd 单元的详细信息。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//   - unit (string): 服务单元名，形如 acpid.service。
//
// 返回 Returns:
//   - detail (ServiceDetail): 单元详情。
//   - err (error): 单元名非法、系统不支持、连接或执行失败时返回非 nil。
func (s *Service) ServiceDetail(agentID, unit string) (ServiceDetail, error) {
	if !unitNameValid(unit) {
		return ServiceDetail{}, fmt.Errorf("服务名不合法：%q", unit)
	}
	osType, err := s.agentOS(agentID)
	if err != nil {
		return ServiceDetail{}, err
	}
	if osType == "" {
		return ServiceDetail{}, errors.New("主机不存在或尚未上报操作系统，无法查询服务详情")
	}
	if osType != "linux" {
		return ServiceDetail{}, fmt.Errorf("暂仅支持查询 Linux 主机的 systemd 服务（该主机为 %s）", osType)
	}

	output, err := s.RunCommand(agentID, serviceDetailCommand(unit), commandRunTimeout)
	if err != nil {
		return ServiceDetail{}, err
	}
	detail, err := ParseServiceDetail(output)
	if err != nil {
		return ServiceDetail{}, err
	}
	detail.Unit = unit
	if reason := ServiceOperationBlocked(unit); reason != "" {
		detail.Protected = true
		detail.BlockReason = reason
	}
	return detail, nil
}

// ParseServiceDetail 解析 `systemctl show` 的输出。
//
// 参数 Parameters:
//   - output (string): systemctl show 的原始输出。
//
// 返回 Returns:
//   - detail (ServiceDetail): 解析结果；Unit 由调用方补齐。
//   - err (error): 输出里完全没有属性行时返回非 nil（通常是单元不存在）。
func ParseServiceDetail(output string) (ServiceDetail, error) {
	values := parseKeyValueLines(output)
	if len(values) == 0 {
		return ServiceDetail{}, errors.New("未取到任何单元属性，可能是该单元不存在")
	}

	detail := ServiceDetail{
		Description:   values["Description"],
		LoadState:     values["LoadState"],
		ActiveState:   values["ActiveState"],
		SubState:      values["SubState"],
		UnitFileState: strings.ToLower(values["UnitFileState"]),
		MainPID:       parseMainPID(values["MainPID"]),
		FragmentPath:  values["FragmentPath"],
		ExecStart:     values["ExecStart"],
		Restarts:      parseIntOrZero(values["NRestarts"]),
	}
	detail.MemoryBytes, detail.MemoryUnlimited = parseSystemdMemory(values["MemoryCurrent"])
	detail.StartedAt = parseSystemdTimestamp(values["ExecMainStartTimestamp"])
	return detail, nil
}

// parseSystemdMemory 解析 MemoryCurrent。
//
// 三个取值要区别对待：具体字节数、"infinity"（未限制）、"[not set]"（该 cgroup 未统计）。
// 后两者都返回 0，但只有 infinity 会置 unlimited——界面对它们的说法不同。
//
// 参数 Parameters:
//   - raw (string): MemoryCurrent 的原始取值。
//
// 返回 Returns:
//   - bytes (uint64): 字节数；非数值时返回 0。
//   - unlimited (bool): 是否为 infinity。
func parseSystemdMemory(raw string) (uint64, bool) {
	trimmed := strings.TrimSpace(raw)
	if strings.EqualFold(trimmed, "infinity") {
		return 0, true
	}
	bytes, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil {
		return 0, false
	}
	return bytes, false
}

// parseSystemdTimestamp 把 systemctl 的时间戳规范化为 2006-01-02 15:04:05。
//
// 解析失败（systemd 未记录、服务从未启动过）时返回空串，由界面显示短横线——
// 直接透传 "n/a" 之类的内容会让界面出现看不懂的字段值。
//
// 参数 Parameters:
//   - raw (string): ExecMainStartTimestamp 的原始取值。
//
// 返回 Returns:
//   - text (string): 规范化时间；无法解析时为空串。
func parseSystemdTimestamp(raw string) string {
	trimmed := strings.TrimSpace(raw)
	// 常见的空值形态：systemd 会写 "n/a" 或直接留空。
	if trimmed == "" || strings.EqualFold(trimmed, "n/a") {
		return ""
	}
	parsed, err := time.Parse(serviceSystemdTimestampLayout, trimmed)
	if err != nil {
		return ""
	}
	return parsed.Format("2006-01-02 15:04:05")
}

// parseIntOrZero 解析整数，失败时返回 0。
//
// 参数 Parameters:
//   - raw (string): 原始文本。
//
// 返回 Returns:
//   - value (int): 解析结果；失败时为 0。
func parseIntOrZero(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return value
}

// ServiceLogLine 是服务日志的一行。
type ServiceLogLine struct {
	// Time 时间戳（目标机本地时间，形如 2026-10-06T23:02:15-0700）；无时间戳的行（如 -- Reboot --）为空串。
	//
	// 刻意保留目标机的时区偏移而不换算到浏览器时区：排障时看的是「目标机上那会儿发生了什么」，
	// 换算会让日志时间和目标机上的其它记录对不上。
	Time string `json:"time"`
	// Message 日志正文（不含时间戳）。
	Message string `json:"message"`
}

// 服务日志的行数取值边界。
const (
	// serviceLogDefaultLines 默认返回的日志行数。
	serviceLogDefaultLines = 200
	// serviceLogMaxLines 单次返回的日志行数上限。
	//
	// 日志正文会随 SSH 通道整体传回，行数不设上限时一个刷屏的服务足以拖垮这次请求。
	serviceLogMaxLines = 2000
)

// serviceLogTimestampPattern 匹配 `journalctl -o short-iso` 的时间戳前缀。
//
// 形如 2026-10-06T23:02:15-0700，部分版本会带毫秒/微秒小数部分，因此小数位可选。
var serviceLogTimestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?[+-]\d{4}$`)

// ServiceLogs 通过 SSH 查询指定 systemd 单元的运行日志。
//
// 用 `journalctl -u <unit> -n <lines> --no-pager -o short-iso`：
//   - -n 只取最近若干行，避免把历史全量日志拉回来；
//   - --no-pager 必须加，否则 journalctl 在远端会等分页器，表现为请求一直挂住；
//   - -o short-iso 让每行都以 ISO 时间戳开头，便于拆成「时间 + 正文」两段，
//     界面上的「时间戳」开关才能即时生效，而不必为了隐藏时间戳再查一次。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//   - unit (string): 服务单元名。
//   - lines (int): 期望的行数；<=0 用默认值，超过上限会被压到上限。
//
// 返回 Returns:
//   - lines ([]ServiceLogLine): 日志行（按时间正序，与 journalctl 输出一致）。
//   - err (error): 单元名非法、系统不支持、连接或执行失败时返回非 nil。
func (s *Service) ServiceLogs(agentID, unit string, lines int) ([]ServiceLogLine, error) {
	if !unitNameValid(unit) {
		return nil, fmt.Errorf("服务名不合法：%q", unit)
	}
	osType, err := s.agentOS(agentID)
	if err != nil {
		return nil, err
	}
	if osType == "" {
		return nil, errors.New("主机不存在或尚未上报操作系统，无法查询服务日志")
	}
	if osType != "linux" {
		return nil, fmt.Errorf("暂仅支持查询 Linux 主机的 systemd 日志（该主机为 %s）", osType)
	}

	// 行数先夹到合法区间再拼命令：它会被拼进 shell 命令，必须保证是纯数字。
	if lines <= 0 {
		lines = serviceLogDefaultLines
	}
	if lines > serviceLogMaxLines {
		lines = serviceLogMaxLines
	}
	command := fmt.Sprintf("journalctl -u %s -n %d --no-pager -o short-iso", unit, lines)

	output, err := s.RunCommand(agentID, command, commandRunTimeout)
	if err != nil {
		// journalctl 的报错（无权限读日志、单元不存在）在输出里，带上便于界面直说原因。
		if detail := strings.TrimSpace(output); detail != "" {
			return nil, fmt.Errorf("查询 %s 日志失败: %w（%s）", unit, err, detail)
		}
		return nil, fmt.Errorf("查询 %s 日志失败: %w", unit, err)
	}
	return ParseServiceLogs(output), nil
}

// ParseServiceLogs 解析 `journalctl -o short-iso` 的输出。
//
// 时间戳与正文拆开返回，是因为界面上的「时间戳」开关需要在本地即时切换；
// 若返回整行文本，隐藏时间戳就只能重新查一次日志。
//
// 参数 Parameters:
//   - output (string): journalctl 的原始输出。
//
// 返回 Returns:
//   - lines ([]ServiceLogLine): 日志行；空行会被跳过。
func ParseServiceLogs(output string) []ServiceLogLine {
	lines := make([]ServiceLogLine, 0, 256)
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		stamp, message, found := strings.Cut(line, " ")
		// 不是「时间戳 + 正文」的行原样保留：journalctl 会插入 "-- Reboot --"、
		// "-- No entries --" 这类分隔/提示行，它们本身就是要看的内容，不能当成异常丢掉。
		if !found || !serviceLogTimestampPattern.MatchString(stamp) {
			lines = append(lines, ServiceLogLine{Message: line})
			continue
		}
		lines = append(lines, ServiceLogLine{Time: stamp, Message: strings.TrimSpace(message)})
	}
	return lines
}
