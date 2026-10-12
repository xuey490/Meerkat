package remote

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// 远程命令执行的超时与上限。
const (
	// commandDialTimeout SSH 拨号超时。
	commandDialTimeout = 10 * time.Second
	// commandRunTimeout 单条命令的执行超时。
	//
	// SSH 的 Session 不支持 context（x/crypto/ssh 没有带 context 的执行接口），
	// 因此超时靠 select + 关闭连接兜底。
	commandRunTimeout = 15 * time.Second
	// processListLimit 返回给前端的进程条数上限（按内存降序截断）。
	//
	// 一台机器可能有上千个进程，全量返回既拖慢前端传输也没有意义——
	// 运维关心的是「谁在吃内存」。
	processListLimit = 500
)

// ProcessInfo 是目标主机上的一条进程信息。
type ProcessInfo struct {
	// PID 进程号。
	PID int `json:"pid"`
	// User 进程属主；Windows 的 tasklist 不提供该字段，此时为空。
	User string `json:"user"`
	// CPUPerc CPU 占用百分比；Windows 上不可用，固定为 0。
	CPUPerc float64 `json:"cpuPercent"`
	// MemPerc 内存占用百分比；Windows 上不可用，固定为 0。
	MemPerc float64 `json:"memPercent"`
	// RSSBytes 常驻内存字节数。
	RSSBytes uint64 `json:"rssBytes"`
	// Command 完整启动命令（含参数）。
	Command string `json:"command"`
	// Name 进程名（由启动命令推导）。
	Name string `json:"name"`
}

// processLinePattern 匹配 `ps -eo pid=,user=,pcpu=,pmem=,rss=,args=` 的输出行。
//
// 三处写法都是有原因的：
//   - 列间用 [ \t] 而不是 \s：\s 包含换行，贪婪匹配时会跨行吞掉内容，
//     把命令行拼错位（输入里混入 \r 时尤其明显）；
//   - 前 5 列显式匹配：ps 的列之间是对齐用的连续空格，而命令行本身也含空格，
//     按空格切分会把命令行拆碎；
//   - 末尾 (.*) 收下剩余全部内容作为命令行，调用方再 TrimSpace。
var processLinePattern = regexp.MustCompile(`^[ \t]*(\d+)[ \t]+(\S+)[ \t]+([\d.]+)[ \t]+([\d.]+)[ \t]+(\d+)[ \t]*(.*)$`)

// RunCommand 在目标主机上执行一条命令并返回合并输出。
//
// 为什么独立建连、而不复用终端会话：终端会话持有的 ssh.Client 是请求私有的
// （随 WebSocket 断开即关闭，且字段未导出），进程内也没有连接池。这条路径与 SFTP
// 保持一致：取凭据 → 拨号 → 执行 → 关闭。代价是每次约几百毫秒的握手，
// 换来的是没有跨请求的连接状态、实现也简单。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//   - command (string): 要执行的命令。
//   - timeout (time.Duration): 执行超时；<=0 时用 commandRunTimeout。
//
// 返回 Returns:
//   - output (string): 命令的合并输出（stdout + stderr），失败时也返回已产生的输出。
//   - err (error): 取凭据、连接或执行失败时返回非 nil。
func (s *Service) RunCommand(agentID, command string, timeout time.Duration) (string, error) {
	host, port, username, authType, secret, passphrase, err := s.SSHCredentials(agentID)
	if err != nil {
		return "", err
	}
	auth, err := sshAuth(authType, secret, passphrase)
	if err != nil {
		return "", err
	}
	config := &ssh.ClientConfig{
		User:            username,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         commandDialTimeout,
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return "", fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	// 关闭连接会让下面可能仍在阻塞的 CombinedOutput 立即返回，因此不会泄漏 goroutine。
	defer func() { _ = client.Close() }()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("ssh new session: %w", err)
	}
	defer func() { _ = session.Close() }()

	type commandResult struct {
		output []byte
		err    error
	}
	done := make(chan commandResult, 1)
	go func() {
		output, runErr := session.CombinedOutput(command)
		done <- commandResult{output: output, err: runErr}
	}()

	if timeout <= 0 {
		timeout = commandRunTimeout
	}
	select {
	case res := <-done:
		if res.err != nil {
			return string(res.output), fmt.Errorf("执行命令失败: %w", res.err)
		}
		return string(res.output), nil
	case <-time.After(timeout):
		return "", fmt.Errorf("命令执行超时（超过 %s）", timeout)
	}
}

// agentOS 返回主机注册时上报的操作系统标识（runtime.GOOS）。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//
// 返回 Returns:
//   - osType (string): 小写的系统标识，如 linux / windows。
//   - err (error): 查询失败时返回非 nil。
func (s *Service) agentOS(agentID string) (string, error) {
	var osType string
	if err := s.db.Table("agents").Select("os").Where("id = ?", agentID).Scan(&osType).Error; err != nil {
		return "", fmt.Errorf("查询主机操作系统失败: %w", err)
	}
	return strings.ToLower(strings.TrimSpace(osType)), nil
}

// ListProcesses 通过 SSH 实时查询目标主机的进程列表。
//
// 结果会进入短 TTL 缓存（见 listcache.go）：这条查询每次都要新建一条 SSH 连接，
// 而抽屉开关、切行、刷新按钮会让同一份数据被反复请求。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//   - force (bool): true 时跳过缓存直接查询（供界面的「刷新」按钮使用）。
//
// 返回 Returns:
//   - processes ([]ProcessInfo): 进程列表（按内存降序，最多 processListLimit 条）。
//   - err (error): 查询或命令执行失败时返回非 nil。
func (s *Service) ListProcesses(agentID string, force bool) ([]ProcessInfo, error) {
	if !force {
		if processes, found := s.processCache.get(agentID); found {
			return processes, nil
		}
	}

	osType, err := s.agentOS(agentID)
	if err != nil {
		return nil, err
	}

	var processes []ProcessInfo
	if osType == "windows" {
		output, runErr := s.RunCommand(agentID, "tasklist /FO CSV /NH", commandRunTimeout)
		if runErr != nil {
			return nil, runErr
		}
		processes = ParseTasklistProcessList(output)
	} else {
		// args 放最后：命令行里的空格不会影响它前面的列。
		output, runErr := s.RunCommand(agentID, "ps -eo pid=,user=,pcpu=,pmem=,rss=,args=", commandRunTimeout)
		if runErr != nil {
			return nil, runErr
		}
		processes = ParseProcessList(output)
	}

	// 整理必须在入缓存之前完成，见 prepareProcessList 的说明。
	processes = prepareProcessList(processes)
	s.processCache.put(agentID, processes)
	return processes, nil
}

// prepareProcessList 把原始进程列表整理成对外返回的形态：按内存降序并截断。
//
// 为什么单独抽出来并强调「必须在入缓存之前调用」：缓存里的切片会被多个请求共享，
// 若把未排序的切片放进缓存、之后每次命中再排一次，就是对同一底层数组的并发写
// （两个请求同时 sort）——那是实打实的数据竞争。整理只能做一次，且必须发生在
// 数据被共享之前。
//
// 参数 Parameters:
//   - processes ([]ProcessInfo): 原始进程列表。
//
// 返回 Returns:
//   - prepared ([]ProcessInfo): 按内存降序、最多 processListLimit 条的列表。
func prepareProcessList(processes []ProcessInfo) []ProcessInfo {
	// 按内存降序：运维打开进程列表首先想知道谁在吃内存。
	sort.Slice(processes, func(i, j int) bool { return processes[i].RSSBytes > processes[j].RSSBytes })
	if len(processes) > processListLimit {
		processes = processes[:processListLimit]
	}
	return processes
}

// killProtectedPIDs 是不允许通过界面结束的进程。
//
// 只拦最核心的两个：PID 1（init/systemd）与 2（kthreadd）被结束后整机不可用，
// 且误操作不可逆。其余进程交给操作者判断——界面上已有二次确认，且请求会被操作日志审计。
var killProtectedPIDs = map[int]string{
	1: "系统初始化进程 init/systemd",
	2: "内核线程管理进程 kthreadd",
}

// KillProcess 结束目标主机上的指定进程。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//   - pid (int): 进程号。
//
// 返回 Returns:
//   - err (error): 校验不通过或命令执行失败时返回非 nil。
func (s *Service) KillProcess(agentID string, pid int) error {
	if pid <= 0 {
		return errors.New("进程号无效")
	}
	if reason, protected := killProtectedPIDs[pid]; protected {
		return fmt.Errorf("不允许结束 PID %d（%s）", pid, reason)
	}
	osType, err := s.agentOS(agentID)
	if err != nil {
		return err
	}
	// pid 是 int，格式化后不可能带 shell 元字符，因此命令拼接不存在注入风险。
	command := fmt.Sprintf("kill -9 %d", pid)
	if osType == "windows" {
		command = fmt.Sprintf("taskkill /F /PID %d", pid)
	}
	// 缓存失效：杀进程必然改变进程列表；若杀的是某个服务的主进程，systemd 会把该
	// 服务标成 failed，服务列表也随之改变——所以两者一起失效（见 InvalidateTargetLists）。
	// 无论成功失败都失效：失败也可能已经产生了副作用。
	defer s.InvalidateTargetLists(agentID)

	output, err := s.RunCommand(agentID, command, commandRunTimeout)
	if err != nil {
		// 失败原因通常在输出里（如 "No such process"），带上便于界面提示。
		detail := strings.TrimSpace(output)
		if detail != "" {
			return fmt.Errorf("结束进程 %d 失败: %w（%s）", pid, err, detail)
		}
		return fmt.Errorf("结束进程 %d 失败: %w", pid, err)
	}
	return nil
}

// systemdRestartTimeout 是重启 systemd 服务的执行超时。
//
// 比普通命令宽松：systemctl restart 要等目标服务走完停止流程（很多服务有数秒的优雅退出），
// 但也不能无限等——否则一次卡住的重启会让 HTTP 请求一直挂着。
const systemdRestartTimeout = 45 * time.Second

// systemdUnitPattern 从 /proc/<pid>/cgroup 的内容里提取 systemd unit 名。
//
// cgroup v2 形如 `0::/system.slice/nginx.service`，
// cgroup v1 形如 `1:name=systemd:/system.slice/telegraf.service`。
var systemdUnitPattern = regexp.MustCompile(`(?:^|/)([A-Za-z0-9_.@-]+\.service)(?:\s|$)`)

// systemdRestartBlocked 判断某个 unit 是否禁止通过界面重启。
//
// 这些是「重启它等于把整机或整用户会话的底座掀掉」的 unit，与进程列表里的 PID 1/2 同级。
//
// 参数 Parameters:
//   - unit (string): unit 名（已通过 unitNameValid 校验）。
//
// 返回 Returns:
//   - reason (string): 禁止原因；允许重启时返回空串。
func systemdRestartBlocked(unit string) string {
	switch {
	case strings.HasPrefix(unit, "user@"):
		return "用户会话管理器，重启会终止该用户的所有进程"
	case strings.HasPrefix(unit, "systemd-"):
		return "systemd 自身组件，重启会影响整机基础功能"
	case unit == "dbus.service":
		return "D-Bus 系统总线，多数服务依赖它"
	}
	return ""
}

// RestartProcess 重启目标主机上由 systemd 托管的进程。
//
// 为什么以 systemd 为界：Linux 没有「重启任意进程」的通用命令。可行的做法只有两种——
//  1. 先 kill 再按原命令行重新执行：会丢失环境变量、工作目录、文件描述符与日志重定向，
//     多数服务重启后会行为异常，风险高于收益；
//  2. 交给服务管理器 restart：由 systemd 按 unit 定义原样拉起，这才是可预期的重启。
//
// 因此这里只支持 systemd 托管的进程，其余情况明确报错，而不是"假装重启成功"。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//   - pid (int): 进程号。
//
// 返回 Returns:
//   - result (RestartResult): 被重启的 unit 与重启后的主进程号。
//   - err (error): 校验不通过、进程不受 systemd 托管或重启失败时返回非 nil。
func (s *Service) RestartProcess(agentID string, pid int) (RestartResult, error) {
	if pid <= 0 {
		return RestartResult{}, errors.New("进程号无效")
	}
	// 与杀进程同一套核心进程保护：重启同样会终止 PID 1/2。
	if reason, protected := killProtectedPIDs[pid]; protected {
		return RestartResult{}, fmt.Errorf("不允许重启 PID %d（%s）", pid, reason)
	}
	osType, err := s.agentOS(agentID)
	if err != nil {
		return RestartResult{}, err
	}
	if osType == "windows" {
		return RestartResult{}, errors.New("Windows 主机不支持按进程号重启，请在服务管理器里重启对应服务")
	}

	unit, err := s.systemdUnitOf(agentID, pid)
	if err != nil {
		return RestartResult{}, err
	}
	if unit == "" {
		return RestartResult{}, fmt.Errorf("进程 %d 不由 systemd 托管，无法自动重启：可先结束它，再用原命令启动", pid)
	}
	if reason := systemdRestartBlocked(unit); reason != "" {
		return RestartResult{}, fmt.Errorf("不允许重启 %s（%s）", unit, reason)
	}

	// 缓存失效：重启会换主进程号（进程列表变化），服务状态同样会变。
	// 放在 systemctl 之前 def：无论重启成功、失败还是超时都已动过目标机。
	defer s.InvalidateTargetLists(agentID)

	// unit 已通过 unitNameValid 的白名单校验，不存在命令注入面。
	// systemctl restart 是阻塞调用：返回即代表服务已完成「停止 → 启动」。
	output, err := s.RunCommand(agentID, "systemctl restart "+unit, systemdRestartTimeout)
	if err != nil {
		// systemctl 的失败原因在输出里（权限不足、unit 不存在等），带上便于界面提示。
		detail := strings.TrimSpace(output)
		if detail != "" {
			return RestartResult{}, fmt.Errorf("重启 %s 失败: %w（%s）", unit, err, detail)
		}
		return RestartResult{}, fmt.Errorf("重启 %s 失败: %w", unit, err)
	}

	// 回查重启后的主进程号并回传：界面据此直接显示新进程，不必让操作者自己去列表里翻。
	// 它同时也是「是否真的换了进程」的自证——正常 restart 后 MainPID 必然变化，
	// 若仍等于原 PID，说明这次重启没有实际生效（unit 与实际进程不匹配等）。
	mainPID := s.systemdMainPID(agentID, unit)
	return RestartResult{
		Unit:        unit,
		MainPID:     mainPID,
		PreviousPID: pid,
		Changed:     mainPID > 0 && mainPID != pid,
	}, nil
}

// systemdMainPID 查询 unit 当前的主进程号。
//
// 用 `systemctl show -p MainPID --value`：输出只有一串数字，比解析 status 稳定得多。
// 查不到（systemd 尚未记录、非 root 权限不足、服务已退出）时返回 0，由调用方按「未知」处理。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//   - unit (string): unit 名（已通过 unitNameValid 校验）。
//
// 返回 Returns:
//   - pid (int): 主进程号；未知时返回 0。
func (s *Service) systemdMainPID(agentID, unit string) int {
	output, err := s.RunCommand(agentID, "systemctl show -p MainPID --value "+unit, commandRunTimeout)
	if err != nil {
		return 0
	}
	return parseMainPID(output)
}

// parseMainPID 解析 `systemctl show -p MainPID --value` 的输出。
//
// 参数 Parameters:
//   - output (string): 命令原始输出（可能带换行与空白）。
//
// 返回 Returns:
//   - pid (int): 主进程号；输出不是正整数时返回 0。
func parseMainPID(output string) int {
	pid, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// RestartResult 是一次 systemd 服务重启的结果。
//
// 它存在的意义：只回一句「重启成功」时，操作者看到界面上还是原来的进程号，
// 会误以为没生效。回传新进程号后，「重启到底起没起作用」一眼可判。
type RestartResult struct {
	// Unit 被重启的 systemd unit。
	Unit string `json:"unit"`
	// MainPID 重启后的主进程号；0 表示查询不到（按「未知」处理，不代表失败）。
	MainPID int `json:"mainPID"`
	// PreviousPID 重启前的进程号，便于界面提示「从 x 变为 y」。
	PreviousPID int `json:"previousPID"`
	// Changed 主进程号是否已变化。MainPID > 0 且为 false 时，通常意味着重启没有真正换进程。
	Changed bool `json:"changed"`
}

// systemdUnitOf 查询进程所属的 systemd unit。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//   - pid (int): 进程号。
//
// 返回 Returns:
//   - unit (string): unit 名；进程不受 systemd 托管时返回空串。
//   - err (error): 读取 cgroup 失败时返回非 nil。
func (s *Service) systemdUnitOf(agentID string, pid int) (string, error) {
	// pid 是 int，拼出来只可能是一串数字，无注入面。
	output, err := s.RunCommand(agentID, fmt.Sprintf("cat /proc/%d/cgroup", pid), commandRunTimeout)
	if err != nil {
		return "", err
	}
	return parseSystemdUnit(output), nil
}

// parseSystemdUnit 从 /proc/<pid>/cgroup 的内容里解析出进程所属的 systemd unit。
//
// 不在 systemd 管辖下（容器内、脚本直接启动的进程）时返回空串。
//
// 参数 Parameters:
//   - cgroup (string): /proc/<pid>/cgroup 的原始内容。
//
// 返回 Returns:
//   - unit (string): unit 名；无法识别时返回空串。
func parseSystemdUnit(cgroup string) string {
	for _, line := range strings.Split(cgroup, "\n") {
		trimmed := strings.TrimSpace(line)
		// 路径穿越防御必须在正则之前完成：`../../etc/passwd.service` 会被正则截出
		// 无害的 "passwd.service"，逐个 unit 名校验根本拦不住它。
		if strings.Contains(trimmed, "..") {
			continue
		}
		match := systemdUnitPattern.FindStringSubmatch(trimmed)
		if match == nil {
			continue
		}
		if unitNameValid(match[1]) {
			return match[1]
		}
	}
	return ""
}

// unitNameValid 校验 systemd unit 名。
//
// 这是安全边界：unit 名会被拼进 `systemctl restart <unit>` 交给远端 shell 执行，
// 而它的来源（被监控机上的进程可影响自己的 cgroup 内容）并不完全可信，
// 因此只接受「字母/数字/._@- 组成、以 .service 结尾」的名字，杜绝命令注入。
//
// 参数 Parameters:
//   - unit (string): 待校验的 unit 名。
//
// 返回 Returns:
//   - valid (bool): 合法返回 true。
func unitNameValid(unit string) bool {
	if len(unit) > 128 || !strings.HasSuffix(unit, ".service") {
		return false
	}
	// 拒绝路径穿越：合法 unit 名里不会出现 ".."。
	if strings.Contains(unit, "..") {
		return false
	}
	for _, r := range unit {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '@' || r == '-':
		default:
			return false
		}
	}
	return true
}

// ParseProcessList 解析 Linux `ps -eo pid=,user=,pcpu=,pmem=,rss=,args=` 的输出。
//
// 参数 Parameters:
//   - output (string): ps 命令输出。
//
// 返回 Returns:
//   - processes ([]ProcessInfo): 进程列表；无法解析的行会被跳过（例如空行）。
func ParseProcessList(output string) []ProcessInfo {
	processes := make([]ProcessInfo, 0, 128)
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		match := processLinePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		pid, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		cpu, _ := strconv.ParseFloat(match[3], 64)
		memPerc, _ := strconv.ParseFloat(match[4], 64)
		rssKB, _ := strconv.ParseUint(match[5], 10, 64)
		command := strings.TrimSpace(match[6])
		processes = append(processes, ProcessInfo{
			PID:      pid,
			User:     match[2],
			CPUPerc:  cpu,
			MemPerc:  memPerc,
			RSSBytes: rssKB * 1024, // ps 的 rss 单位是 KB
			Command:  command,
			Name:     processName(command),
		})
	}
	return processes
}

// ParseTasklistProcessList 解析 Windows `tasklist /FO CSV /NH` 的输出。
//
// tasklist 不提供属主与 CPU，因此这些字段留空，内存取内存使用列。
//
// 参数 Parameters:
//   - output (string): tasklist 命令输出。
//
// 返回 Returns:
//   - processes ([]ProcessInfo): 进程列表；格式不符的行会被跳过。
func ParseTasklistProcessList(output string) []ProcessInfo {
	records, err := csv.NewReader(strings.NewReader(output)).ReadAll()
	if err != nil {
		return nil
	}
	processes := make([]ProcessInfo, 0, len(records))
	for _, record := range records {
		// 列顺序：映像名称, PID, 会话名, 会话#, 内存使用
		if len(record) < 5 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(record[1]))
		if err != nil {
			continue
		}
		name := strings.TrimSpace(record[0])
		processes = append(processes, ProcessInfo{
			PID:      pid,
			Name:     name,
			Command:  name,
			RSSBytes: parseTasklistMemory(record[4]),
		})
	}
	return processes
}

// parseTasklistMemory 解析 tasklist 的内存列（形如 "12,345 K"）。
//
// 参数 Parameters:
//   - raw (string): 原始内存文本。
//
// 返回 Returns:
//   - bytes (uint64): 字节数；无法解析时返回 0。
func parseTasklistMemory(raw string) uint64 {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, raw)
	kb, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024 // tasklist 的单位是 KB
}

// processName 从启动命令里推导进程名。
//
// 两种形态要区别对待：
//   - 普通进程：首个 token 是可执行文件路径（/sbin/init、/usr/bin/python3），取 basename；
//   - 内核线程：首个 token 是被方括号包起来的名字（[ksoftirqd/0]、[kworker/0:1]），
//     它整体就是进程名，不能按路径拆分——否则会得到 "0]" 这种残片。
//
// 参数 Parameters:
//   - command (string): 完整启动命令，可能为空。
//
// 返回 Returns:
//   - name (string): 进程名；无法推导时返回空串。
func processName(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	name := fields[0]
	if strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]") {
		return name
	}
	if index := strings.LastIndexAny(name, `/\`); index >= 0 && index+1 < len(name) {
		name = name[index+1:]
	}
	return name
}
