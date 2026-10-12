package remote

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ListeningPort 是目标主机上的一条监听端口。
type ListeningPort struct {
	// Protocol 协议：tcp / udp。
	Protocol string `json:"protocol"`
	// Local 本地地址:端口，如 0.0.0.0:22、127.0.0.53%lo:53、[::]:5902。
	Local string `json:"local"`
	// Process 监听进程，形如 "99113/sshd"；多个进程共享同一端口时以逗号分隔。
	//
	// 非 root 用户拿不到别人的进程信息，此时为空串，界面显示短横线即可——
	// 拿不到进程名不代表这次查询失败，端口本身仍然是有效数据。
	Process string `json:"process"`
}

// portProcessPattern 从 ss 的 Process 列里取出「进程名 + PID」。
//
// ss 的原始形态形如 users:(("sshd",pid=99113,fd=3),("sshd",pid=99113,fd=4))：
// 同一个进程会因多个文件描述符重复出现，所以解析后必须按 PID 去重。
var portProcessPattern = regexp.MustCompile(`\("([^"]+)",pid=(\d+)`)

// ListeningPorts 通过 SSH 查询目标主机的监听端口。
//
// 命令用 `ss -lntupH`，每个开关都有理由：-l 只看监听（需求就是“正在监听的端口”）；
// -n 不做反向解析，否则在 DNS 不通的机器上会长时间卡住；-t / -u 同时覆盖 TCP 与 UDP；
// -p 带出监听进程（需要 root，拿不到时进程列为空）；-H 去掉表头，省得在解析里特意跳过一行。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//
// 返回 Returns:
//   - ports ([]ListeningPort): 监听端口列表（保持 ss 的输出顺序）。
//   - err (error): 主机不存在、系统不支持、连接或执行失败时返回非 nil。
func (s *Service) ListeningPorts(agentID string) ([]ListeningPort, error) {
	osType, err := s.agentOS(agentID)
	if err != nil {
		return nil, err
	}
	if osType == "" {
		return nil, errors.New("主机不存在或尚未上报操作系统，无法查询端口")
	}
	if osType != "linux" {
		return nil, fmt.Errorf("暂仅支持查询 Linux 主机的端口（该主机为 %s）", osType)
	}

	output, err := s.RunCommand(agentID, "ss -lntupH", commandRunTimeout)
	if err != nil {
		return nil, err
	}
	return ParseListeningPorts(output), nil
}

// ParseListeningPorts 解析 `ss -lntupH` 的输出。
//
// 每行的列固定为：Netid State Recv-Q Send-Q Local Peer Process。
// Local / Peer 一定不含空格（地址里的空格会被 ss 转义），只有 Process 列可能出现空格，
// 因此按空白切分后取前 6 段定位、其余整体作为进程列——与 ps 的解析是同一思路。
//
// 参数 Parameters:
//   - output (string): ss 命令输出。
//
// 返回 Returns:
//   - ports ([]ListeningPort): 监听端口列表；列数不足的行会被跳过（例如空行与告警文本）。
func ParseListeningPorts(output string) []ListeningPort {
	ports := make([]ListeningPort, 0, 32)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		var extra []string
		if len(fields) > 6 {
			extra = fields[6:]
		}
		ports = append(ports, ListeningPort{
			Protocol: strings.ToLower(fields[0]),
			Local:    fields[4],
			Process:  formatPortProcess(extra),
		})
	}
	return ports
}

// formatPortProcess 把 ss 的 Process 列整理成「PID/进程名」的紧凑形式。
//
// 一条监听可能被多个进程共享（SO_REUSEPORT，例如多个 httpd worker），
// 因此结果是逗号分隔的多项；同一 PID 的多个 fd 只保留一次。
//
// 参数 Parameters:
//   - fields ([]string): Process 列的空白切分结果。
//
// 返回 Returns:
//   - text (string): 形如 "1782/httpd, 1781/httpd"；无进程信息时为空串。
func formatPortProcess(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	matches := portProcessPattern.FindAllStringSubmatch(strings.Join(fields, " "), -1)
	if len(matches) == 0 {
		return ""
	}
	seen := make(map[string]bool, len(matches))
	parts := make([]string, 0, len(matches))
	for _, match := range matches {
		pid := match[2]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		parts = append(parts, pid+"/"+match[1])
	}
	return strings.Join(parts, ", ")
}
