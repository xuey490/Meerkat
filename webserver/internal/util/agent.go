package util

import (
	"net"
	"regexp"
	"strings"
)

// userAgentPattern 描述如何从 User-Agent 中识别操作系统与浏览器。
type userAgentPattern struct {
	pattern string
	label   string
}

// userAgentOSPatterns 是操作系统识别规则，按顺序匹配（越具体的越靠前）。
var userAgentOSPatterns = []userAgentPattern{
	{"Windows NT", "Windows"},
	{"Android", "Android"},
	{"iPhone", "iOS"},
	{"iPad", "iOS"},
	{"Mac OS X", "macOS"},
	{"Linux", "Linux"},
}

// userAgentBrowserPatterns 是浏览器识别规则，按顺序匹配（越具体的越靠前）。
var userAgentBrowserPatterns = []userAgentPattern{
	{"Edg", "Edge"},
	{"OPR", "Opera"},
	{"Firefox", "Firefox"},
	{"Chrome", "Chrome"},
	{"Safari", "Safari"},
	{"curl", "curl"},
}

// versionPattern 用于从 User-Agent 中截取浏览器主版本号。
var versionPattern = regexp.MustCompile(`\d+`)

// ParseUserAgent 从 User-Agent 中解析设备、浏览器与操作系统。
//
// 参数 Parameters:
//   - userAgent (string): 原始 User-Agent 头。
//
// 返回 Returns:
//   - device (string): 设备类型（PC / Mobile / Bot / Unknown）。
//   - browser (string): 浏览器名称，可能带主版本号。
//   - os (string): 操作系统名称。
func ParseUserAgent(userAgent string) (device, browser, os string) {
	ua := strings.TrimSpace(userAgent)
	if ua == "" {
		return "Unknown", "Unknown", "Unknown"
	}
	device = "PC"
	lower := strings.ToLower(ua)
	switch {
	case strings.Contains(lower, "bot") || strings.Contains(lower, "spider") || strings.Contains(lower, "curl"):
		device = "Bot"
	case strings.Contains(lower, "mobile") || strings.Contains(lower, "iphone") || strings.Contains(lower, "android"):
		device = "Mobile"
	}
	os = "Unknown"
	for _, candidate := range userAgentOSPatterns {
		if strings.Contains(ua, candidate.pattern) {
			os = candidate.label
			break
		}
	}
	browser = "Unknown"
	for _, candidate := range userAgentBrowserPatterns {
		if strings.Contains(ua, candidate.pattern) {
			browser = candidate.label
			break
		}
	}
	if version := versionPattern.FindString(ua); version != "" && browser != "Unknown" {
		browser = browser + " " + version
	}
	return device, browser, os
}

// IPLocation 给出 IP 归属地的粗粒度描述（不引入外部 IP 库，仅区分内网/公网）。
//
// 参数 Parameters:
//   - ip (string): 客户端 IP。
//
// 返回 Returns:
//   - location (string): "内网地址" / "公网地址" / 空串（无法解析时）。
func IPLocation(ip string) string {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return ""
	}
	if parsed.IsLoopback() || parsed.IsPrivate() || parsed.IsLinkLocalUnicast() {
		return "内网地址"
	}
	return "公网地址"
}
