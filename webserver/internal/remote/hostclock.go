package remote

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// hostClockRunTimeout 是主机探针的执行超时。
//
// 只读几个文件，正常情况下几十毫秒就返回；给 8 秒是给「首次建连较慢」留余量，
// 同时不至于让 HTTP 请求长期挂住——RunCommand 每次调用都会新建 SSH 连接。
const hostClockRunTimeout = 8 * time.Second

// hostClockCommand 是在目标机上取主机信息的复合命令。
//
// 两个设计点：
//
//  1. 一条命令、一个 SSH 会话。RunCommand 每次调用都会新建连接（见其注释），
//     把发行版/架构拆成另一个接口就等于多付一次握手——而它们本可以从同一次会话里顺手取回。
//     这也是本接口除了时钟还返回 osName / machine 的原因。
//
//  2. 每行都带 KEY= 前缀，而不是靠输出行号定位。原因：时区与发行版都走"可用才输出"的
//     回退链，某一段没输出时行数会少一行；按序号取值会把后面的字段整体错位
//     （实测踩过：时区为空时，发行版被当成了时区）。带前缀后每行自证身份，更稳。
//
// 各字段含义：
//   - EPOCH   ：远端 Unix 时间（含纳秒），与本地时钟夹逼后算出偏移；
//   - OFFSET  ：UTC 偏移（形如 +0800），用于把远端钟面时间渲染出来；
//   - TZ      ：时区名，回退链 /etc/timezone → timedatectl → /etc/sysconfig/clock；
//   - OS      ：发行版名称（/etc/os-release 的 PRETTY_NAME），如 Ubuntu 20.04.3 LTS；
//   - MACHINE ：机器架构（uname -m），如 x86_64。
//
// 取不到的字段该行为空，由界面显示「未知」或回落到 agents 表的值，而不是编一个默认值。
const hostClockCommand = `echo "EPOCH=$(date +%s.%N)"; ` +
	`echo "OFFSET=$(date '+%z')"; ` +
	`echo "TZ=$(cat /etc/timezone 2>/dev/null || timedatectl show -p Timezone --value 2>/dev/null || sed -n 's/^ZONE="\(.*\)"/\1/p' /etc/sysconfig/clock 2>/dev/null)"; ` +
	`echo "OS=$(grep -m1 ^PRETTY_NAME /etc/os-release 2>/dev/null | cut -d= -f2- | sed 's/"//g')"; ` +
	`echo "MACHINE=$(uname -m)"`

// utcOffsetPattern 匹配 `date '+%z'` 的输出（形如 +0800 / -0700）。
var utcOffsetPattern = regexp.MustCompile(`^[+-][0-9]{4}$`)

// timezonePattern 匹配时区名（Asia/Shanghai、UTC、Etc/GMT+8 等）。
//
// 这里做白名单式校验而不是直接采信远端输出：限制字符集可以顺带挡掉把噪声行
// （shell 告警、报错）当成时区显示出来的情况。
var timezonePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+/-]{0,63}$`)

// machinePattern 匹配 uname -m 的输出（x86_64、aarch64、armv7l、amd64 等）。
var machinePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,32}$`)

// hostOSNameMaxLength 是发行版名称的展示上限。
//
// /etc/os-release 由目标机控制，虽然 grep -m1 只取一行，但仍截断一下更稳妥，
// 避免异常内容把面板布局撑坏。
const hostOSNameMaxLength = 120

// HostClock 是目标主机的时区、时钟与（顺带取回的）机器标识。
type HostClock struct {
	// Supported 目标机是否支持探测；仅 Linux 支持。
	Supported bool `json:"supported"`
	// Reason 不支持的原因（Supported 为 false 时非空）。
	Reason string `json:"reason"`
	// Timezone 时区名，如 Asia/Shanghai；取不到时为空串。
	Timezone string `json:"timezone"`
	// UTCOffset UTC 偏移，形如 +0800；取不到时为空串。
	UTCOffset string `json:"utcOffset"`
	// RemoteEpoch 远端 Unix 时间（秒，含小数）。前端据此让钟面继续走字。
	RemoteEpoch float64 `json:"remoteEpoch"`
	// RemoteTime 远端时间（按远端自己的 UTC 偏移渲染的 RFC3339）。
	//
	// 用远端偏移而不是换算到本机时区：面板要回答的是「目标机上的钟面是几点」，
	// 换算过来会把「它的钟走得对不对」这个信息抹掉。
	RemoteTime string `json:"remoteTime"`
	// OffsetSeconds 远端时钟相对本服务的偏移（秒，正数表示远端更快）。
	//
	// 精度约为 ±RTT/2：SSH 往返时延的一半是这条通道下无法消除的估计误差，
	// 因此它只适合判断「时钟是否明显跑偏」，不能当作 NTP 级别的授时对比。
	OffsetSeconds float64 `json:"offsetSeconds"`
	// RoundTripMs 本次采集的 SSH 往返耗时（毫秒），即上面误差量级的来源。
	RoundTripMs int64 `json:"roundTripMs"`
	// OSName 发行版名称，如 Ubuntu 20.04.3 LTS；取不到时为空串。
	//
	// agents 表只有内核串（"Linux 5.15.0-139-generic"），没有发行版，
	// 因此它只能从目标机取；顺手在同一条命令里带回，避免为此再开一次 SSH。
	OSName string `json:"osName"`
	// Machine 机器架构（uname -m），如 x86_64；取不到时为空串。
	//
	// 与 agents.architecture 的区别只是命名习惯：那个是 Go 的 GOARCH（amd64），
	// 这个是被探测机自己的说法（x86_64）。
	Machine string `json:"machine"`
}

// HostClock 通过 SSH 采集目标主机的时区、时钟与机器标识。
//
// 只支持 Linux：Windows 的对应命令与解析是另一套（PowerShell），本版本直接返回
// supported=false 让界面显示「暂不支持」，而不是给一份编造的数据。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//
// 返回 Returns:
//   - clock (HostClock): 时区、时钟与机器标识。
//   - err (error): 主机不存在、取凭据、连接或执行失败时返回非 nil。
func (s *Service) HostClock(agentID string) (HostClock, error) {
	osType, err := s.agentOS(agentID)
	if err != nil {
		return HostClock{}, err
	}
	if osType == "" {
		return HostClock{}, errors.New("主机不存在或尚未上报操作系统，无法采集主机时钟")
	}
	if osType != "linux" {
		return HostClock{
			Supported: false,
			Reason:    "该主机为 " + osType + "，暂仅支持采集 Linux 主机的时区与时钟",
		}, nil
	}

	// 夹逼两次本地时钟，取中点作为「远端时间对应的本地时刻」。
	started := time.Now()
	output, err := s.RunCommand(agentID, hostClockCommand, hostClockRunTimeout)
	finished := time.Now()
	if err != nil {
		return HostClock{}, err
	}
	return ParseHostClock(output, started, finished)
}

// ParseHostClock 解析 hostClockCommand 的输出并计算时钟偏移。
//
// 之所以拆成不依赖 SSH 的纯函数：偏移计算依赖「本地时钟」这个入参，
// 藏在 Service 方法里就只能连真机才能验证，无法做针对性的断言。
//
// 参数 Parameters:
//   - output (string): 命令原始输出（每行形如 KEY=VALUE）。
//   - started (time.Time): 发起采集前的本地时间。
//   - finished (time.Time): 采集返回后的本地时间。
//
// 返回 Returns:
//   - clock (HostClock): 解析结果；单个字段取不到时留空，不编造默认值。
//   - err (error): EPOCH 无法解析时返回非 nil（此时宁可报错也不显示假时间）。
func ParseHostClock(output string, started, finished time.Time) (HostClock, error) {
	values := parseKeyValueLines(output)

	epoch, err := strconv.ParseFloat(values["EPOCH"], 64)
	if err != nil {
		// 常见于 busybox 的 date 不支持 %N（会原样吐出 "1760157237.N"）。
		return HostClock{}, fmt.Errorf("解析远端时间 %q 失败（部分精简系统的 date 不支持 %%N）: %w", values["EPOCH"], err)
	}

	clock := HostClock{Supported: true, RemoteEpoch: epoch}
	if raw := values["OFFSET"]; utcOffsetPattern.MatchString(raw) {
		clock.UTCOffset = raw
	}
	// timedatectl 在时区未设置时会输出 Unknown / n/a，二者形状合规但不是时区名，
	// 必须单独排除，否则界面会出现「时区：Unknown」这种看似正常的错值。
	if raw := values["TZ"]; timezonePattern.MatchString(raw) && !isPlaceholderTimezone(raw) {
		clock.Timezone = raw
	}
	if raw := values["OS"]; raw != "" {
		clock.OSName = truncateText(raw, hostOSNameMaxLength)
	}
	if raw := values["MACHINE"]; machinePattern.MatchString(raw) {
		clock.Machine = raw
	}

	// 远端时间对应的本地时刻：把单程时延近似为 RTT/2。
	midpoint := started.Add(finished.Sub(started) / 2)
	// 用 UnixMilli 而不是 UnixNano 再除：纳秒量级已超出 float64 的有效精度，
	// 而 SSH 往返本身就有几十到几百毫秒的抖动，毫秒精度足够且没有精度陷阱。
	offset := epoch - float64(midpoint.UnixMilli())/1000
	// 只保留毫秒：更细的小数位纯属噪声，显示出来反而像"测得很准"。
	clock.OffsetSeconds = math.Round(offset*1000) / 1000
	clock.RoundTripMs = finished.Sub(started).Milliseconds()

	// 按远端自己的 UTC 偏移渲染钟面时间；偏移取不到时退回 UTC（仍比换成本机时区诚实）。
	zone := time.UTC
	if clock.UTCOffset != "" {
		zone = time.FixedZone("", UTCOffsetSeconds(clock.UTCOffset))
	}
	clock.RemoteTime = time.Unix(int64(epoch), 0).In(zone).Format(time.RFC3339)
	return clock, nil
}

// parseKeyValueLines 把 KEY=VALUE 形式的输出解析为映射。
//
// 不认识的键、不含等号的行（shell 告警、报错文本）都会被忽略——
// 调用方只读取自己关心的键，因此无需在这里做白名单。
//
// 参数 Parameters:
//   - output (string): 命令原始输出。
//
// 返回 Returns:
//   - values (map[string]string): 键到值的映射（值已去首尾空白）。
func parseKeyValueLines(output string) map[string]string {
	values := make(map[string]string, 8)
	for _, line := range strings.Split(output, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
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

// isPlaceholderTimezone 判断取到的是不是「占位值」而不是真正的时区名。
//
// 参数 Parameters:
//   - raw (string): 原始文本。
//
// 返回 Returns:
//   - placeholder (bool): 是占位值时为 true。
func isPlaceholderTimezone(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "unknown", "n/a", "none":
		return true
	default:
		return false
	}
}

// truncateText 按字符数截断文本（按 rune 计数，避免把多字节字符切成乱码）。
//
// 参数 Parameters:
//   - text (string): 原始文本。
//   - limit (int): 最大字符数。
//
// 返回 Returns:
//   - text (string): 截断后的文本。
func truncateText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// UTCOffsetSeconds 把 `date '+%z'` 的输出换算为 UTC 偏移秒数。
//
// 参数 Parameters:
//   - raw (string): 形如 +0800 / -0700 的偏移文本。
//
// 返回 Returns:
//   - seconds (int): 偏移秒数；格式不合法时返回 0。
func UTCOffsetSeconds(raw string) int {
	if !utcOffsetPattern.MatchString(raw) {
		return 0
	}
	sign := 1
	if raw[0] == '-' {
		sign = -1
	}
	hours, _ := strconv.Atoi(raw[1:3])
	minutes, _ := strconv.Atoi(raw[3:5])
	return sign * (hours*3600 + minutes*60)
}
