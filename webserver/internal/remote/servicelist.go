package remote

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ServiceUnit 是目标主机上的一个 systemd 服务单元。
type ServiceUnit struct {
	// Unit 单元名，如 ssh.service。
	Unit string `json:"unit"`
	// Active 运行状态：active / inactive / failed。
	Active string `json:"active"`
	// Sub 细分状态：running / exited / dead / failed。
	Sub string `json:"sub"`
	// Startup 自启状态：enabled / disabled / static / generated / masked；
	// 取不到时为空串（界面显示短横线）。
	Startup string `json:"startup"`
	// Description 单元描述。
	Description string `json:"description"`
	// Protected 是否禁止在界面上操作（见 ServiceOperationBlocked）。
	Protected bool `json:"protected"`
	// BlockReason 禁止操作的原因；可操作时为空串。
	BlockReason string `json:"blockReason"`
}

// serviceListCommand 是取服务清单的复合命令。
//
// 为什么要两条 systemctl：运行状态（active/sub/描述）来自 list-units，
// 而「是否开机自启」是单元文件属性，只有 list-unit-files 才知道——
// 两者是不同维度的数据，不能互相替代。中间用一行标记分隔，便于按段解析。
//
// --no-pager 防止远端分页器把命令挂住；--no-legend 去掉表头与尾部统计；
// --plain 去掉 systemd 用于标记状态的圆点符号。
const serviceListCommand = `systemctl list-units --type=service --all --no-pager --no-legend --plain; ` +
	`echo '===SPLIT==='; ` +
	`systemctl list-unit-files --type=service --no-pager --no-legend --plain`

// serviceListSplitMarker 分隔上面两条命令的输出。
const serviceListSplitMarker = "===SPLIT==="

// 允许的服务操作。取值会拼进 systemctl 命令，因此必须是白名单——
// 这是命令注入的第一道防线（第二道是 unitNameValid 对单元名的校验）。
const (
	// ServiceActionStart 启动服务。
	ServiceActionStart = "start"
	// ServiceActionStop 停止服务。
	ServiceActionStop = "stop"
	// ServiceActionRestart 重启服务。
	ServiceActionRestart = "restart"
	// ServiceActionEnable 设置开机自启。
	ServiceActionEnable = "enable"
	// ServiceActionDisable 取消开机自启。
	ServiceActionDisable = "disable"
)

// serviceActions 是允许的操作集合。
var serviceActions = map[string]bool{
	ServiceActionStart:   true,
	ServiceActionStop:    true,
	ServiceActionRestart: true,
	ServiceActionEnable:  true,
	ServiceActionDisable: true,
}

// serviceActionTimeout 是服务操作的执行超时。
//
// 比普通命令宽松：stop / restart 要走 systemd 的优雅停止流程（不少服务有数秒退出时间），
// start 也可能要等依赖就绪。与「重启 systemd 服务」取同一量级。
const serviceActionTimeout = 45 * time.Second

// blockedServicePrefixes 是按前缀拦截的单元族。
//
// 只放两个族，且都限定得比较紧：
//   - systemd-* 覆盖 journald / logind / udevd / resolved / networkd / timesyncd /
//     tmpfiles / sysctl 等全部底座组件；
//   - dbus-org.freedesktop.* 是 D-Bus 系统总线的别名单元。
//
// 刻意不写成 `dbus` 全前缀：那会把 dbus-org.bluez.service、dbus-fi.w1.wpa_supplicant1.service
// 这类**服务激活别名**（本质是蓝牙/WiFi 的别名）也算进来，给它们安上"D-Bus 系统总线"的
// 理由既不准确、又白拦了无害目标。真正需要拦的 dbus.service / dbus-broker.service 见下方名单。
var blockedServicePrefixes = []struct {
	prefix string
	reason string
}{
	{"systemd-", "systemd 自身组件（日志/登录/设备/DNS/网络/时间同步等），操作会破坏主机的基础功能"},
	{"dbus-org.freedesktop.", "D-Bus 系统总线的别名单元，操作会影响系统级服务的启动与通信"},
}

// blockedServiceNames 是逐个点名的禁止操作单元。
//
// 三类：切断我们自身接入的、让主机失去网络的、影响核心系统安全的（防火墙与审计）。
var blockedServiceNames = map[string]string{
	// 0) 系统总线本体（不带前缀，避免误伤上面说的那些激活别名）。
	"dbus.service":        "D-Bus 系统总线，多数服务依赖它，操作会导致系统级服务批量异常",
	"dbus-broker.service": "D-Bus 系统总线（dbus-broker），多数服务依赖它，操作会导致系统级服务批量异常",

	// 1) 切断接入：本功能正是通过 SSH 下发命令的，停了 sshd 就再也回不去。
	"ssh.service":  "SSH 服务，操作会立即切断当前与后续所有远程接入",
	"sshd.service": "SSH 服务，操作会立即切断当前与后续所有远程接入",

	// 2) 网络：失联同样不可恢复。
	"networking.service":          "网络配置服务，停止后主机可能失去网络连接",
	"networkd-dispatcher.service": "网络状态分发服务，操作可能让网络配置不再生效",
	"networkmanager.service":      "网络管理服务，操作会让主机失去网络连接",
	"wicked.service":              "网络管理服务，操作会让主机失去网络连接",
	"connman.service":             "网络管理服务，操作会让主机失去网络连接",
	"dhcpcd.service":              "DHCP 客户端，停止后主机可能丢失 IP 地址",
	"wpa_supplicant.service":      "无线网络服务，停止后主机会断网",

	// 3) 核心系统安全：不影响连通性，但误停会静默削弱防护，界面上看不出后果。
	"ufw.service":       "防火墙，停用会静默削弱主机防护",
	"firewalld.service": "防火墙，停用会静默削弱主机防护",
	"nftables.service":  "防火墙，停用会静默削弱主机防护",
	"iptables.service":  "防火墙，停用会静默削弱主机防护",
	"ip6tables.service": "防火墙，停用会静默削弱主机防护",
	"shorewall.service": "防火墙，停用会静默削弱主机防护",
	"fail2ban.service":  "入侵防护服务，停用会静默削弱主机防护",
	"auditd.service":    "安全审计服务，停用会导致审计记录丢失",
	"apparmor.service":  "强制访问控制（AppArmor），操作会削弱系统的安全约束",
	"polkit.service":    "系统授权服务（polkit），影响系统级权限判定",
}

// ServiceOperationBlocked 判断某个服务是否禁止在界面上操作，返回禁止原因（允许时为空串）。
//
// 这是本功能最重要的安全边界。判据只有一条：**操作它之后，我们是否还可能再也连不上这台机器**。
// 之所以如此保守：服务操作本身就是通过 SSH 执行的，一旦目标机失联或系统不受控，
// 按钮按下去就再也没有补救机会——不像别的功能失败了大不了重来。
//
// 需要明确的是，这是**黑名单**而非形式化保证：把 sshd 包进自定义 unit 之类的部署仍能绕过。
// 真正的兜底是另外三层——独立的高危权限标识、操作前二次确认、以及全部写操作自动进操作日志。
//
// 参数 Parameters:
//   - unit (string): 单元名。
//
// 返回 Returns:
//   - reason (string): 禁止原因；允许操作时为空串。
func ServiceOperationBlocked(unit string) string {
	name := strings.ToLower(strings.TrimSpace(unit))
	if name == "" {
		return "服务名不能为空"
	}
	for _, blocked := range blockedServicePrefixes {
		if strings.HasPrefix(name, blocked.prefix) {
			return blocked.reason
		}
	}
	if reason, found := blockedServiceNames[name]; found {
		return reason
	}
	return ""
}

// ServiceUnits 通过 SSH 查询目标主机的 systemd 服务清单。
//
// 结果会进入短 TTL 缓存（见 listcache.go）：这条查询要跑两条 systemctl，
// 而抽屉开关、切行、刷新按钮会让同一份数据被反复请求。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//   - force (bool): true 时跳过缓存直接查询（供界面的「刷新」按钮使用）。
//
// 返回 Returns:
//   - units ([]ServiceUnit): 服务列表（按单元名排序）。
//   - err (error): 主机不存在、系统不支持、连接或执行失败时返回非 nil。
func (s *Service) ServiceUnits(agentID string, force bool) ([]ServiceUnit, error) {
	// 缓存命中就直接返回：不查库、不建连。这与 TTL 一样是「省一次 SSH」的关键，
	// 因此放在最前面（agentOS 还要查一次数据库）。
	if !force {
		if units, found := s.serviceCache.get(agentID); found {
			return units, nil
		}
	}

	osType, err := s.agentOS(agentID)
	if err != nil {
		return nil, err
	}
	if osType == "" {
		return nil, errors.New("主机不存在或尚未上报操作系统，无法查询服务")
	}
	if osType != "linux" {
		// Windows 的服务模型是 SCM（sc/net 命令），与 systemd 完全是两套，本版本不做。
		return nil, fmt.Errorf("暂仅支持查询 Linux 主机的 systemd 服务（该主机为 %s）", osType)
	}

	output, err := s.RunCommand(agentID, serviceListCommand, commandRunTimeout)
	if err != nil {
		return nil, err
	}
	units := ParseServiceUnits(output)
	// 只在成功时写缓存（错误不缓存）。存入后该切片不再被改动，可安全地并发读取。
	s.serviceCache.put(agentID, units)
	return units, nil
}

// ParseServiceUnits 解析 serviceListCommand 的输出。
//
// 两个来源取并集，缺一不可：
//   - list-units：运行状态与描述（含 --all，所以未运行的单元也在）；
//   - list-unit-files：单元文件清单与自启状态，是"这台机器装了哪些服务"的权威来源。
//
// 只用 list-units 会漏掉「从未被任何 target 引用、systemd 尚未加载」的单元；
// 只用 list-unit-files 则拿不到 active/sub 与描述。
//
// 参数 Parameters:
//   - output (string): 命令原始输出（两段以 serviceListSplitMarker 分隔）。
//
// 返回 Returns:
//   - units ([]ServiceUnit): 服务列表，按单元名升序；非 .service 的行一律忽略。
func ParseServiceUnits(output string) []ServiceUnit {
	unitsPart, filesPart := output, ""
	if index := strings.Index(output, serviceListSplitMarker); index >= 0 {
		unitsPart = output[:index]
		filesPart = output[index+len(serviceListSplitMarker):]
	}
	startupStates := parseUnitFileStates(filesPart)

	// 用 map 做并集，最后统一排序，避免依赖两条命令的输出顺序。
	collected := make(map[string]*ServiceUnit, 128)
	for _, line := range strings.Split(unitsPart, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.HasSuffix(fields[0], ".service") {
			continue
		}
		unit := &ServiceUnit{
			Unit:   fields[0],
			Active: strings.ToLower(fields[2]),
			Sub:    strings.ToLower(fields[3]),
		}
		if len(fields) > 4 {
			unit.Description = strings.Join(fields[4:], " ")
		}
		collected[unit.Unit] = unit
	}
	for name, state := range startupStates {
		if unit, found := collected[name]; found {
			unit.Startup = state
			continue
		}
		// 只在单元文件里出现：说明它没被加载（无人引用、从未启动）。
		// 运行状态按 systemd 的口径报 inactive/dead，而不是留空——"没在跑"是确定的事实。
		collected[name] = &ServiceUnit{Unit: name, Active: "inactive", Sub: "dead", Startup: state}
	}

	units := make([]ServiceUnit, 0, len(collected))
	for _, unit := range collected {
		if reason := ServiceOperationBlocked(unit.Unit); reason != "" {
			unit.Protected = true
			unit.BlockReason = reason
		}
		units = append(units, *unit)
	}
	sort.Slice(units, func(i, j int) bool { return units[i].Unit < units[j].Unit })
	return units
}

// parseUnitFileStates 从 list-unit-files 段里取出「单元名 → 自启状态」。
//
// 只取前两列即可：新版 systemd 会多输出一列 PRESET，忽略它不影响解析。
//
// 参数 Parameters:
//   - section (string): list-unit-files 的输出段。
//
// 返回 Returns:
//   - states (map[string]string): 单元名到自启状态（已转小写）的映射。
func parseUnitFileStates(section string) map[string]string {
	states := make(map[string]string, 128)
	for _, line := range strings.Split(section, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasSuffix(fields[0], ".service") {
			continue
		}
		states[fields[0]] = strings.ToLower(fields[1])
	}
	return states
}

// OperateService 对目标主机的服务执行启停 / 重启 / 自启操作。
//
// 三道校验缺一不可，顺序也是有意的（先便宜的、先本地的，再连线）：
//  1. 操作白名单——取值会拼进 systemctl 命令，必须限定在已知动作内；
//  2. 单元名校验——同样是拼命令，用已有白名单挡掉注入与 path traversal，
//     顺带把 socket / target 等非服务单元挡在外面（界面只列服务）；
//  3. 高危拦截——核心系统与安全相关单元一律拒绝，理由随错误返回，便于界面直说原因。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//   - unit (string): 服务单元名，形如 ssh.service。
//   - action (string): 操作，取值见 ServiceAction* 常量。
//
// 返回 Returns:
//   - err (error): 校验不通过、系统不支持或 systemctl 执行失败时返回非 nil。
func (s *Service) OperateService(agentID, unit, action string) error {
	normalizedAction := strings.ToLower(strings.TrimSpace(action))
	if !serviceActions[normalizedAction] {
		return fmt.Errorf("不支持的服务操作 %q（仅支持 start / stop / restart / enable / disable）", action)
	}
	if !unitNameValid(unit) {
		return fmt.Errorf("服务名不合法：%q", unit)
	}
	if reason := ServiceOperationBlocked(unit); reason != "" {
		return fmt.Errorf("不允许操作 %s：%s", unit, reason)
	}

	osType, err := s.agentOS(agentID)
	if err != nil {
		return err
	}
	if osType != "linux" {
		return fmt.Errorf("暂仅支持操作 Linux 主机的 systemd 服务（该主机为 %s）", osType)
	}

	// 缓存失效：**无论成功失败都必须失效**。
	//
	// 失败也可能是「做了一半」（restart 停成功、起失败），此时目标机状态确实变了。
	// 失效的代价只是下次多查一次，漏失效的代价却是界面显示错——两者不对称。
	// 用 defer 是为了覆盖下面每一条返回路径，将来在这个函数里加分支也不会漏。
	defer s.InvalidateTargetLists(agentID)

	// action 与 unit 都已过白名单，拼进命令不存在注入面。
	output, err := s.RunCommand(agentID, "systemctl "+normalizedAction+" "+unit, serviceActionTimeout)
	if err != nil {
		// systemctl 的失败原因在输出里（单元不存在、权限不足、依赖失败等），带上便于界面提示。
		if detail := strings.TrimSpace(output); detail != "" {
			return fmt.Errorf("%s %s 失败: %w（%s）", normalizedAction, unit, err, detail)
		}
		return fmt.Errorf("%s %s 失败: %w", normalizedAction, unit, err)
	}
	return nil
}
