package app

import (
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// 控制台颜色（ANSI）：仅用于启动横幅与访问日志，便于在滚动日志中区分。
const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[93;1m"
	colorCyan   = "\033[36;1m"
	colorGray   = "\033[90m"
)

// colorLog 按指定颜色打印一行带时间戳的日志。
//
// 参数 Parameters:
//   - color (string): ANSI 颜色前缀。
//   - message (string): 日志内容。
func colorLog(color, message string) {
	fmt.Printf("%s[%s] %s%s\n", colorGray, time.Now().Format("2006-01-02 15:04:05"), color+message+colorReset, colorReset)
}

// printAccessLog 以灰色打印一条访问日志（供 api 层中间件回调）。
//
// 参数 Parameters:
//   - message (string): 已格式化的访问日志内容。
func printAccessLog(message string) { colorLog(colorGray, message) }

// PrintStartup 打印启动横幅：进程状态、操作系统、PostgreSQL 版本与监听地址。
func (a *App) PrintStartup() {
	enableConsole()
	colorLog(colorYellow, "中心服务器webserver端启动成功")
	colorLog(colorGreen, "操作系统  "+hostOSName()+" "+runtime.GOARCH)
	if version := a.postgresVersion(); version != "" {
		colorLog(colorGreen, "PostgreSQL "+version)
	}
	for _, url := range listenURLs(a.cfg.HTTPAddress, localIPv4s()) {
		colorLog(colorCyan, "正在监听地址:  "+url)
	}
}

// postgresVersion 查询监控库的 PostgreSQL 版本号，失败时返回空串。
//
// 返回 Returns:
//   - version (string): 形如 18.4 的版本号；查询失败返回空串。
func (a *App) postgresVersion() string {
	if a.monitorDB == nil {
		return ""
	}
	var version string
	if err := a.monitorDB.Raw("SHOW server_version").Scan(&version).Error; err != nil {
		return ""
	}
	return strings.TrimSpace(version)
}

// listenURLs 根据监听地址与本机网卡生成可访问地址列表。
//
// 参数 Parameters:
//   - httpAddr (string): 形如 ":8090" 或 "127.0.0.1:8090" 的监听地址。
//   - ips ([]string): 本机 IPv4 地址列表。
//
// 返回 Returns:
//   - urls ([]string): 可访问的 http 地址列表。
func listenURLs(httpAddr string, ips []string) []string {
	port := httpAddr
	host := ""
	if index := strings.LastIndex(httpAddr, ":"); index >= 0 {
		port = httpAddr[index+1:]
		host = httpAddr[:index]
	}
	if port == "" {
		port = "8090"
	}
	urls := make([]string, 0, len(ips)+1)
	if host != "" {
		return append(urls, "http://"+host+":"+port)
	}
	urls = append(urls, "http://127.0.0.1:"+port)
	for _, ip := range ips {
		urls = append(urls, "http://"+ip+":"+port)
	}
	return urls
}

// formatVersion 拼接系统版本号（Windows 与类 Unix 共用）。
//
// 参数 Parameters:
//   - major (uint32): 主版本号。
//   - minor (uint32): 次版本号。
//   - build (uint32): 构建号。
//
// 返回 Returns:
//   - version (string): 形如 10.0.26100。
func formatVersion(major, minor, build uint32) string {
	return strconv.FormatUint(uint64(major), 10) + "." +
		strconv.FormatUint(uint64(minor), 10) + "." +
		strconv.FormatUint(uint64(build), 10)
}

// localIPv4s 返回本机非回环 IPv4 地址。
//
// 返回 Returns:
//   - ips ([]string): IPv4 地址列表。
func localIPv4s() []string {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	ips := make([]string, 0, 4)
	for _, address := range addresses {
		network, ok := address.(*net.IPNet)
		if !ok || network.IP.IsLoopback() {
			continue
		}
		ip := network.IP.To4()
		if ip == nil {
			continue
		}
		ips = append(ips, ip.String())
	}
	return ips
}
