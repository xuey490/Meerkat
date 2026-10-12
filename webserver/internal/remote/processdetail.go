package remote

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ProcessIO 是进程的 IO 计数（取自 /proc/<pid>/io）。
type ProcessIO struct {
	// RChar 读出的字节总数（含缓存命中，不落盘也算）。
	RChar uint64 `json:"rchar"`
	// WChar 写入的字节总数（含缓存命中）。
	WChar uint64 `json:"wchar"`
	// ReadBytes 真正从存储层读到的字节数。
	ReadBytes uint64 `json:"readBytes"`
	// WriteBytes 真正写到存储层的字节数。
	WriteBytes uint64 `json:"writeBytes"`
}

// ProcessFDCounts 是进程文件描述符按类型的计数。
//
// 分类而不是只给总数：一个进程持有 800 个套接字和持有 800 个普通文件，
// 意味着完全不同的问题（连接泄漏 vs 文件泄漏）。
type ProcessFDCounts struct {
	// Total 文件描述符总数。
	Total int `json:"total"`
	// File 指向普通文件的描述符数。
	File int `json:"file"`
	// Socket 套接字数。
	Socket int `json:"socket"`
	// Pipe 管道数。
	Pipe int `json:"pipe"`
	// Anon 匿名 inode（事件通知、epoll 等内核对象）。
	Anon int `json:"anon"`
	// Device 设备文件数。
	Device int `json:"device"`
	// Other 无法归类（已删除但仍被打开的文件会出现 "(deleted)" 后缀等）。
	Other int `json:"other"`
}

// ProcessDetail 是某个进程的详细信息（取自 /proc/<pid>）。
type ProcessDetail struct {
	// PID 进程号。
	PID int `json:"pid"`
	// Name 进程名（/proc/<pid>/status 的 Name，内核视角的 comm，最长 15 字符）。
	Name string `json:"name"`
	// State 状态原文，如 "S (sleeping)"。原样透传，与 ps / top 的口径一致。
	State string `json:"state"`
	// PPID 父进程号。
	PPID int `json:"ppid"`
	// Threads 线程数。
	Threads int `json:"threads"`
	// Exe 可执行文件路径；内核线程或权限不足时为空串。
	Exe string `json:"exe"`
	// Cwd 工作目录；权限不足时为空串。
	Cwd string `json:"cwd"`
	// Command 完整命令行（/proc/<pid>/cmdline，NUL 已转为空格）。
	//
	// 内核线程没有 cmdline（为空），此时界面应回落到 Name。
	Command string `json:"command"`
	// StartedAt 启动时间，格式 2006-01-02 15:04:05；未知时为空串。
	StartedAt string `json:"startedAt"`
	// RSSBytes 物理内存占用（VmRSS）。
	RSSBytes uint64 `json:"rssBytes"`
	// VSZBytes 虚拟内存占用（VmSize）。
	VSZBytes uint64 `json:"vszBytes"`
	// CPUTicks 累计 CPU 时间片（utime + stime，单位是内核的时钟滴答）。
	CPUTicks int64 `json:"cpuTicks"`
	// VoluntaryCtxSwitches 主动上下文切换次数（等待资源而让出 CPU）。
	VoluntaryCtxSwitches int64 `json:"voluntaryCtxSwitches"`
	// NonVoluntaryCtxSwitches 被动上下文切换次数（被调度器抢占）。
	NonVoluntaryCtxSwitches int64 `json:"nonVoluntaryCtxSwitches"`
	// IO IO 计数。
	IO ProcessIO `json:"io"`
	// FDs 文件描述符分类计数。
	FDs ProcessFDCounts `json:"fds"`
	// FDTruncated 文件描述符统计是否被截断（达到上限）。
	//
	// 必须回传：截断后给出的是"至少这么多"而非精确值，界面不说明就等于报了假数。
	FDTruncated bool `json:"fdTruncated"`
}

// processFDListLimit 单次统计的文件描述符上限。
//
// 描述符很多的进程（连接池、消息队列）可能有上万个，逐个 readlink 既慢又把输出撑大，
// 而"按类型看分布"这个诉求在几千个样本上已经完全满足。
const processFDListLimit = 1000

// processDetailCommandTemplate 是取进程详情的复合命令（%d 依次是 PID 与描述符上限）。
//
// 一次会话取全部信息：RunCommand 每次调用都会新建 SSH 连接，
// 拆成七八条命令就是七八次握手，而这些数据全都在 /proc/<pid> 下，一次读完才合理。
//
// 两个细节：
//   - 先把 PID 存进变量 P，避免把同一个数字重复写十几遍；
//   - 每段都以 `2>/dev/null` 兜住错误。CombinedOutput 会把 stderr 并进 stdout，
//     不重定向的话「readlink: 权限不够」这类报错会混进数据里，被界面当成路径显示出来。
const processDetailCommandTemplate = `P=%d; ` +
	`echo '===LSTART==='; LC_ALL=C ps -o lstart= -p $P 2>/dev/null; echo; ` +
	`echo '===STATUS==='; cat /proc/$P/status 2>/dev/null; ` +
	`echo '===IO==='; cat /proc/$P/io 2>/dev/null; ` +
	`echo '===STAT==='; cat /proc/$P/stat 2>/dev/null; ` +
	`echo '===CMDLINE==='; cat /proc/$P/cmdline 2>/dev/null | tr '\0' ' '; echo; ` +
	`echo '===EXE==='; readlink /proc/$P/exe 2>/dev/null; echo; ` +
	`echo '===CWD==='; readlink /proc/$P/cwd 2>/dev/null; echo; ` +
	`echo '===FD==='; readlink /proc/$P/fd/* 2>/dev/null | head -n %d`

// processDetailSectionPattern 匹配复合命令里的分节标记行，如 `===STATUS===`。
var processDetailSectionPattern = regexp.MustCompile(`^===([A-Z]+)===$`)

// psLStartLayout 是 `ps -o lstart=` 的输出格式，如 "Sun Oct 11 02:57:22 2026"。
//
// 用 "_2" 而不是 "02"：个位数日期在 ps 输出里是空格补齐的（"Oct  5"），
// 用 "02" 会解析失败。调用时已加 LC_ALL=C，因此月份与星期一定是英文。
const psLStartLayout = "Mon Jan _2 15:04:05 2006"

// ProcessDetail 通过 SSH 查询指定进程的详细信息。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//   - pid (int): 进程号。
//
// 返回 Returns:
//   - detail (ProcessDetail): 进程详情。
//   - err (error): 进程号无效、系统不支持、连接或执行失败、进程不存在时返回非 nil。
func (s *Service) ProcessDetail(agentID string, pid int) (ProcessDetail, error) {
	if pid <= 0 {
		return ProcessDetail{}, errors.New("进程号无效")
	}
	osType, err := s.agentOS(agentID)
	if err != nil {
		return ProcessDetail{}, err
	}
	if osType == "" {
		return ProcessDetail{}, errors.New("主机不存在或尚未上报操作系统，无法查询进程详情")
	}
	if osType != "linux" {
		// Windows 的进程信息在 tasklist / WMI 里，字段与 /proc 完全不同，本版本不做。
		return ProcessDetail{}, fmt.Errorf("暂仅支持查询 Linux 主机的进程详情（该主机为 %s）", osType)
	}

	// pid 是 int，格式化后只可能是数字，拼进命令无注入面。
	command := fmt.Sprintf(processDetailCommandTemplate, pid, processFDListLimit)
	output, err := s.RunCommand(agentID, command, commandRunTimeout)
	if err != nil {
		return ProcessDetail{}, err
	}
	detail, err := ParseProcessDetail(output)
	if err != nil {
		return ProcessDetail{}, fmt.Errorf("%w（PID %d）", err, pid)
	}
	detail.PID = pid
	return detail, nil
}

// ParseProcessDetail 解析 processDetailCommandTemplate 的输出。
//
// 参数 Parameters:
//   - output (string): 命令原始输出。
//
// 返回 Returns:
//   - detail (ProcessDetail): 解析结果；PID 由调用方补齐。
//   - err (error): 取不到进程状态（进程不存在或已退出）时返回非 nil。
func ParseProcessDetail(output string) (ProcessDetail, error) {
	sections := splitSections(output)
	status := parseColonLines(sections["STATUS"])
	if len(status) == 0 {
		// 取不到状态只可能是进程已不存在：命令里所有读取都做了 2>/dev/null，
		// 因此"没输出"本身就是结论，不需要再去猜错误文本。
		return ProcessDetail{}, errors.New("进程不存在或已退出")
	}
	ioStats := parseColonLines(sections["IO"])

	detail := ProcessDetail{
		Name:                    status["Name"],
		State:                   status["State"],
		PPID:                    parseIntOrZero(status["PPid"]),
		Threads:                 parseIntOrZero(status["Threads"]),
		RSSBytes:                parseProcStatusKB(status["VmRSS"]),
		VSZBytes:                parseProcStatusKB(status["VmSize"]),
		VoluntaryCtxSwitches:    parseInt64OrZero(status["voluntary_ctxt_switches"]),
		NonVoluntaryCtxSwitches: parseInt64OrZero(status["nonvoluntary_ctxt_switches"]),
		Command:                 strings.TrimSpace(sections["CMDLINE"]),
		Exe:                     firstLine(sections["EXE"]),
		Cwd:                     firstLine(sections["CWD"]),
		StartedAt:               parsePsLStart(sections["LSTART"]),
		IO: ProcessIO{
			RChar:      parseUintOrZero(ioStats["rchar"]),
			WChar:      parseUintOrZero(ioStats["wchar"]),
			ReadBytes:  parseUintOrZero(ioStats["read_bytes"]),
			WriteBytes: parseUintOrZero(ioStats["write_bytes"]),
		},
	}
	if utime, stime, ok := parseProcStat(sections["STAT"]); ok {
		detail.CPUTicks = utime + stime
	}
	detail.FDs, detail.FDTruncated = classifyFDList(sections["FD"])
	return detail, nil
}

// splitSections 把复合命令的输出按 `===NAME===` 标记拆成分节。
//
// 标记之前的任何内容都会被丢弃——那里只可能是登录横幅之类的噪声。
//
// 参数 Parameters:
//   - output (string): 命令原始输出。
//
// 返回 Returns:
//   - sections (map[string]string): 分节名到正文的映射。
func splitSections(output string) map[string]string {
	sections := make(map[string]string, 8)
	current := ""
	builder := &strings.Builder{}
	flush := func() {
		if current != "" {
			sections[current] = builder.String()
		}
		builder.Reset()
	}
	for _, line := range strings.Split(output, "\n") {
		if match := processDetailSectionPattern.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			flush()
			current = match[1]
			continue
		}
		if current == "" {
			continue
		}
		builder.WriteString(line)
		builder.WriteString("\n")
	}
	flush()
	return sections
}

// parseColonLines 解析 `Key:	Value` 形式的输出。
//
// /proc/<pid>/status 与 /proc/<pid>/io 都是这种格式：键占一列、值在制表符之后。
//
// 参数 Parameters:
//   - section (string): 某一节的正文。
//
// 返回 Returns:
//   - values (map[string]string): 键到值（已 TrimSpace）的映射。
func parseColonLines(section string) map[string]string {
	values := make(map[string]string, 64)
	for _, line := range strings.Split(section, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		values[key] = strings.TrimSpace(value)
	}
	return values
}

// parseProcStatusKB 解析 /proc/<pid>/status 里以 kB 为单位的字段（如 "2140 kB"）。
//
// 参数 Parameters:
//   - raw (string): 字段原始取值。
//
// 返回 Returns:
//   - bytes (uint64): 字节数；无法解析时返回 0。
func parseProcStatusKB(raw string) uint64 {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return 0
	}
	kb, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024 // 该字段的单位是 kB
}

// parseProcStat 从 /proc/<pid>/stat 里取出 utime 与 stime（字段 14、15）。
//
// 不能直接对整行按空白切分：第 2 个字段 comm 被圆括号包裹，而进程名本身可能含空格与括号
// （例如 "(sd-pam)"）。权威做法是取**最后一个 ')'** 之后再切分——从那里开始的 token
// 依次对应字段 3、4、5……，因此字段 N 落在下标 N-3。
//
// 参数 Parameters:
//   - content (string): /proc/<pid>/stat 的原始内容。
//
// 返回 Returns:
//   - utime (int64): 用户态时钟滴答数。
//   - stime (int64): 内核态时钟滴答数。
//   - ok (bool): 解析成功时为 true。
func parseProcStat(content string) (utime, stime int64, ok bool) {
	index := strings.LastIndex(content, ")")
	if index < 0 {
		return 0, 0, false
	}
	const utimeField, stimeField = 14, 15
	fields := strings.Fields(content[index+1:])
	if len(fields) <= stimeField-3 {
		return 0, 0, false
	}
	utime, _ = strconv.ParseInt(fields[utimeField-3], 10, 64)
	stime, _ = strconv.ParseInt(fields[stimeField-3], 10, 64)
	return utime, stime, true
}

// classifyFDList 统计文件描述符列表并按类型归类。
//
// 参数 Parameters:
//   - section (string): `readlink /proc/<pid>/fd/*` 的输出（每行一个链接目标）。
//
// 返回 Returns:
//   - counts (ProcessFDCounts): 分类计数。
//   - truncated (bool): 是否达到上限而未能统计完。
func classifyFDList(section string) (ProcessFDCounts, bool) {
	var counts ProcessFDCounts
	for _, line := range strings.Split(section, "\n") {
		target := strings.TrimSpace(line)
		if target == "" {
			continue
		}
		if counts.Total >= processFDListLimit {
			break
		}
		counts.Total++
		switch classifyFD(target) {
		case "socket":
			counts.Socket++
		case "pipe":
			counts.Pipe++
		case "anon":
			counts.Anon++
		case "device":
			counts.Device++
		case "file":
			counts.File++
		default:
			counts.Other++
		}
	}
	// 达到上限就说明还没数完：此时 Total 是"至少这么多"，必须让界面说明这一点。
	// 命令侧的 head -n 会先截断，这里不能只依赖"多出一个"来判断。
	return counts, counts.Total >= processFDListLimit
}

// classifyFD 按 /proc/<pid>/fd 的链接目标给描述符归类。
//
// 参数 Parameters:
//   - target (string): 链接目标，形如 socket:[123]、pipe:[1]、anon_inode:[eventfd]、/dev/sda。
//
// 返回 Returns:
//   - kind (string): socket / pipe / anon / device / file / other。
func classifyFD(target string) string {
	switch {
	case strings.HasPrefix(target, "socket:"):
		return "socket"
	case strings.HasPrefix(target, "pipe:"):
		return "pipe"
	case strings.HasPrefix(target, "anon_inode:"):
		return "anon"
	case strings.HasPrefix(target, "/dev/"):
		return "device"
	case strings.HasPrefix(target, "/"):
		return "file"
	default:
		return "other"
	}
}

// parsePsLStart 把 `ps -o lstart=` 的输出规范化为 2006-01-02 15:04:05。
//
// 参数 Parameters:
//   - raw (string): ps 的原始输出。
//
// 返回 Returns:
//   - text (string): 规范化时间；无法解析时为空串（界面显示短横线）。
func parsePsLStart(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "n/a") {
		return ""
	}
	parsed, err := time.Parse(psLStartLayout, trimmed)
	if err != nil {
		return ""
	}
	return parsed.Format("2006-01-02 15:04:05")
}

// firstLine 返回文本里第一处非空内容。
//
// readlink 失败时该节只可能是空行，用它可以避免把空白当成路径。
//
// 参数 Parameters:
//   - section (string): 某一节的正文。
//
// 返回 Returns:
//   - line (string): 第一行非空内容；全空时返回空串。
func firstLine(section string) string {
	for _, line := range strings.Split(section, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// parseInt64OrZero 解析 64 位整数，失败时返回 0。
//
// 参数 Parameters:
//   - raw (string): 原始文本。
//
// 返回 Returns:
//   - value (int64): 解析结果；失败时为 0。
func parseInt64OrZero(raw string) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

// parseUintOrZero 解析无符号整数，失败时返回 0。
//
// 参数 Parameters:
//   - raw (string): 原始文本。
//
// 返回 Returns:
//   - value (uint64): 解析结果；失败时为 0。
func parseUintOrZero(raw string) uint64 {
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return value
}
