// Package iploc 提供 IP 归属地查询：支持开关、切换数据源、结果缓存与并发单飞。
//
// 缺省数据源为 ip-api.com（免费、无需 key、支持中文，限速 45 次/分钟），
// 国内网络可切换为 pconline，或用 endpoint 指定自定义接口（模板中的 {ip} 会被替换）。
package iploc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// 数据源标识。
const (
	// ProviderBaidu 百度 opendata 接口（默认：国内直连、免 key、中文、毫秒级响应）。
	ProviderBaidu = "baidu"
	// ProviderIPAPI ip-api.com（海外，免 key，中文，国内访问较慢）。
	ProviderIPAPI = "ipapi"
	// ProviderPconline 太平洋电脑网（部分网络返回 403）。
	ProviderPconline = "pconline"
	// ProviderCustom 自定义接口（endpoint 模板）。
	ProviderCustom = "custom"
)

// maxResponseBytes 限制单次响应体大小，避免异常接口拖垮内存。
const maxResponseBytes = 64 * 1024

// Config 是 IP 归属地查询配置。
type Config struct {
	// Enabled 是否启用归属地查询。
	Enabled bool
	// Provider 数据源：ipapi / pconline / custom。
	Provider string
	// Endpoint 自定义数据源地址模板（含 {ip}）。
	Endpoint string
	// Timeout 单次查询超时（HTTP 层）。
	Timeout time.Duration
	// SyncWait 登录等需要立即落库的场景最多等待查询结果的时长；
	// 超时则先落库兜底标签，查询在后台继续，结果写入缓存供后续请求使用。
	SyncWait time.Duration
	// CacheTTL 查询结果缓存时间。
	CacheTTL time.Duration
	// PrivateLabel 内网地址标签。
	PrivateLabel string
	// DisabledLabel 关闭功能时的标签。
	DisabledLabel string
	// UnknownLabel 查询失败时的标签。
	UnknownLabel string
}

// Cache 是归属地结果缓存（由 cachestore.Store 实现）。
type Cache interface {
	// Get 读取缓存。
	Get(ctx context.Context, key string, dest any) bool
	// Set 写入缓存。
	Set(ctx context.Context, key string, value any) error
}

// Resolver 执行 IP 归属地查询。
type Resolver struct {
	// cfg 配置。
	cfg Config
	// cache 结果缓存。
	cache Cache
	// client HTTP 客户端。
	client *http.Client
	// mu 保护 inflight。
	mu sync.Mutex
	// inflight 记录进行中的查询，避免同一 IP 并发重复请求。
	inflight map[string]chan struct{}
}

// New 创建归属地查询器。
//
// 参数 Parameters:
//   - cfg (Config): 配置。
//   - cache (Cache): 结果缓存，可为 nil。
//
// 返回 Returns:
//   - resolver (*Resolver): 查询器。
func New(cfg Config, cache Cache) *Resolver {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 6 * time.Second
	}
	if cfg.SyncWait <= 0 {
		cfg.SyncWait = 1500 * time.Millisecond
	}
	if strings.TrimSpace(cfg.Provider) == "" {
		cfg.Provider = ProviderBaidu
	}
	if strings.TrimSpace(cfg.PrivateLabel) == "" {
		cfg.PrivateLabel = "内网地址"
	}
	if strings.TrimSpace(cfg.DisabledLabel) == "" {
		cfg.DisabledLabel = "未解析(已关闭归属地查询)"
	}
	if strings.TrimSpace(cfg.UnknownLabel) == "" {
		cfg.UnknownLabel = "未解析"
	}
	return &Resolver{
		cfg:      cfg,
		cache:    cache,
		client:   &http.Client{Timeout: timeout},
		inflight: make(map[string]chan struct{}, 16),
	}
}

// Enabled 返回是否启用归属地查询。
//
// 返回 Returns:
//   - enabled (bool): 是否启用。
func (r *Resolver) Enabled() bool { return r != nil && r.cfg.Enabled }

// DisabledLabel 返回功能关闭时的展示标签。
//
// 返回 Returns:
//   - label (string): 标签文本。
func (r *Resolver) DisabledLabel() string {
	if r == nil {
		return "未解析"
	}
	return r.cfg.DisabledLabel
}

// Resolve 查询 IP 归属地（阻塞，用于登录等需要立即落库的场景）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - ip (string): 客户端 IP。
//
// 返回 Returns:
//   - location (string): 归属地文本；未启用/内网/失败时返回对应兜底标签。
func (r *Resolver) Resolve(ctx context.Context, ip string) string {
	if r == nil || !r.cfg.Enabled {
		return r.DisabledLabel()
	}
	trimmed := normalizeIP(ip)
	if trimmed == "" {
		return r.cfg.UnknownLabel
	}
	if isPrivate(trimmed) {
		return r.cfg.PrivateLabel
	}
	if location := r.fromCache(ctx, trimmed); location != "" {
		return location
	}
	// 冷启动：先发起后台查询，再最多等待 SyncWait。
	// 这样即使上游很慢（跨境接口常见 3~8s）也不会把延迟转嫁给登录请求；
	// 查询结果会写入缓存，后续登录/在线用户/操作日志即可命中。
	r.Prime(trimmed)
	deadline := time.Now().Add(r.cfg.SyncWait)
	for {
		if location := r.fromCache(ctx, trimmed); location != "" {
			return location
		}
		if !time.Now().Before(deadline) {
			slog.Info("IP 归属地查询未在等待窗口内返回，已转为异步补写",
				"ip", trimmed, "provider", r.cfg.Provider, "syncWait", r.cfg.SyncWait)
			return r.cfg.UnknownLabel
		}
		time.Sleep(80 * time.Millisecond)
	}
}

// Location 返回归属地标签（非阻塞）：命中缓存直接返回，未命中时异步预取并先返回兜底标签。
//
// 用途：操作日志等高频写入路径，避免同步查询拖慢请求。
//
// 参数 Parameters:
//   - ip (string): 客户端 IP。
//
// 返回 Returns:
//   - location (string): 归属地文本或兜底标签。
func (r *Resolver) Location(ip string) string {
	if r == nil || !r.cfg.Enabled {
		return r.DisabledLabel()
	}
	trimmed := normalizeIP(ip)
	if trimmed == "" {
		return r.cfg.UnknownLabel
	}
	if isPrivate(trimmed) {
		return r.cfg.PrivateLabel
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.Timeout)
	defer cancel()
	if location := r.fromCache(ctx, trimmed); location != "" {
		return location
	}
	r.Prime(trimmed)
	return r.cfg.UnknownLabel
}

// Prime 异步预取归属地并写入缓存（不阻塞调用方）。
//
// 参数 Parameters:
//   - ip (string): 客户端 IP。
func (r *Resolver) Prime(ip string) {
	if r == nil || !r.cfg.Enabled {
		return
	}
	trimmed := normalizeIP(ip)
	if trimmed == "" || isPrivate(trimmed) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), r.cfg.Timeout+time.Second)
		defer cancel()
		if r.fromCache(ctx, trimmed) != "" {
			return
		}
		if wait := r.begin(trimmed); wait != nil {
			return
		}
		location, err := r.fetch(ctx, trimmed)
		r.finish(trimmed)
		if err != nil || location == "" {
			slog.Warn("预取 IP 归属地失败", "ip", trimmed, "provider", r.cfg.Provider, "error", err)
			return
		}
		r.toCache(ctx, trimmed, location)
	}()
}

// cacheKey 返回归属地缓存的业务键。
//
// 参数 Parameters:
//   - ip (string): IP 地址。
//
// 返回 Returns:
//   - key (string): 缓存键。
func (r *Resolver) cacheKey(ip string) string { return "iploc:" + ip }

// fromCache 读取缓存结果。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - ip (string): IP 地址。
//
// 返回 Returns:
//   - location (string): 缓存命中时的归属地；未命中返回空串。
func (r *Resolver) fromCache(ctx context.Context, ip string) string {
	if r.cache == nil {
		return ""
	}
	var location string
	if r.cache.Get(ctx, r.cacheKey(ip), &location) {
		return location
	}
	return ""
}

// toCache 写入缓存。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - ip (string): IP 地址。
//   - location (string): 归属地文本。
func (r *Resolver) toCache(ctx context.Context, ip, location string) {
	if r.cache == nil {
		return
	}
	_ = r.cache.Set(ctx, r.cacheKey(ip), location)
}

// begin 登记一次进行中的查询；已有进行中的查询时返回其完成通道（等待方）。
//
// 参数 Parameters:
//   - ip (string): IP 地址。
//
// 返回 Returns:
//   - wait (chan struct{}): 等待通道；本次为发起方时返回 nil。
func (r *Resolver) begin(ip string) chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if channel, exists := r.inflight[ip]; exists {
		return channel
	}
	r.inflight[ip] = make(chan struct{})
	return nil
}

// finish 结束一次查询并唤醒等待方。
//
// 参数 Parameters:
//   - ip (string): IP 地址。
func (r *Resolver) finish(ip string) {
	r.mu.Lock()
	channel, exists := r.inflight[ip]
	if exists {
		delete(r.inflight, ip)
	}
	r.mu.Unlock()
	if exists {
		close(channel)
	}
}

// fetch 按数据源请求归属地。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - ip (string): IP 地址。
//
// 返回 Returns:
//   - location (string): 归属地文本；失败时返回空串。
func (r *Resolver) fetch(ctx context.Context, ip string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(r.cfg.Provider)) {
	case ProviderIPAPI:
		return r.fetchIPAPI(ctx, ip)
	case ProviderPconline:
		return r.fetchPconline(ctx, ip)
	case ProviderCustom:
		return r.fetchCustom(ctx, ip)
	default:
		return r.fetchBaidu(ctx, ip)
	}
}

// fetchBaidu 查询百度 opendata 接口（国内直连、免 key、中文、毫秒级）。
//
// 响应形如 {"status":"0","data":[{"location":"广东省佛山市 电信"}]}。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - ip (string): IP 地址。
//
// 返回 Returns:
//   - location (string): 归属地文本。
//   - err (error): 请求或解析失败时返回原始错误。
func (r *Resolver) fetchBaidu(ctx context.Context, ip string) (string, error) {
	endpoint := "https://opendata.baidu.com/api.php?query=" + url.QueryEscape(ip) + "&co=&resource_id=6006&oe=utf8"
	body, err := r.get(ctx, endpoint)
	if err != nil {
		return "", err
	}
	var payload struct {
		Status string `json:"status"`
		Data   []struct {
			Location string `json:"location"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("parse baidu response: %w", err)
	}
	for _, item := range payload.Data {
		if location := strings.TrimSpace(item.Location); location != "" {
			return location, nil
		}
	}
	return "", fmt.Errorf("baidu response has no location (status %q)", payload.Status)
}

// fetchIPAPI 查询 ip-api.com（中文，无需密钥）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - ip (string): IP 地址。
//
// 返回 Returns:
//   - location (string): 归属地文本。
func (r *Resolver) fetchIPAPI(ctx context.Context, ip string) (string, error) {
	endpoint := fmt.Sprintf(
		"http://ip-api.com/json/%s?lang=zh-CN&fields=status,country,regionName,city,isp",
		url.PathEscape(ip))
	body, err := r.get(ctx, endpoint)
	if err != nil {
		return "", err
	}
	var payload struct {
		Status     string `json:"status"`
		Country    string `json:"country"`
		RegionName string `json:"regionName"`
		City       string `json:"city"`
		Isp        string `json:"isp"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("parse ip-api response: %w", err)
	}
	if payload.Status != "success" {
		return "", fmt.Errorf("ip-api status %q", payload.Status)
	}
	return joinLocation(payload.Country, payload.RegionName, payload.City), nil
}

// fetchPconline 查询太平洋电脑网接口（返回 GBK 编码 JSON）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - ip (string): IP 地址。
//
// 返回 Returns:
//   - location (string): 归属地文本。
func (r *Resolver) fetchPconline(ctx context.Context, ip string) (string, error) {
	endpoint := "http://whois.pconline.com.cn/ipJson.jsp?json=true&ip=" + url.QueryEscape(ip)
	body, err := r.get(ctx, endpoint)
	if err != nil {
		return "", err
	}
	decoded, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), body)
	if err != nil {
		decoded = body
	}
	var payload struct {
		Pro  string `json:"pro"`
		City string `json:"city"`
		Addr string `json:"addr"`
	}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return "", fmt.Errorf("parse pconline response: %w", err)
	}
	if addr := strings.TrimSpace(payload.Addr); addr != "" {
		return addr, nil
	}
	return joinLocation("", payload.Pro, payload.City), nil
}

// fetchCustom 查询自定义接口：endpoint 中的 {ip} 会被替换为实际 IP，
// 响应为 JSON 对象时按常见字段拼接归属地。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - ip (string): IP 地址。
//
// 返回 Returns:
//   - location (string): 归属地文本。
func (r *Resolver) fetchCustom(ctx context.Context, ip string) (string, error) {
	endpoint := strings.TrimSpace(r.cfg.Endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("custom provider endpoint is empty")
	}
	endpoint = strings.ReplaceAll(endpoint, "{ip}", url.QueryEscape(ip))
	body, err := r.get(ctx, endpoint)
	if err != nil {
		return "", err
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return strings.TrimSpace(string(body)), nil
	}
	for _, key := range []string{"addr", "location", "area"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), nil
		}
	}
	pick := func(keys ...string) string {
		for _, key := range keys {
			if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
		return ""
	}
	return joinLocation(pick("country"), pick("province", "region", "regionName", "pro"), pick("city")), nil
}

// get 发起 GET 请求并返回响应体（限长）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - endpoint (string): 请求地址。
//
// 返回 Returns:
//   - body ([]byte): 响应体。
//   - err (error): 请求失败或状态码非 2xx 时返回错误。
func (r *Resolver) get(ctx context.Context, endpoint string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "monitor-webserver/1.0")
	response, err := r.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("ip location upstream status %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
}

// normalizeIP 规范化 IP 文本：去空白、去端口、去 IPv6 方括号。
//
// 参数 Parameters:
//   - raw (string): 原始文本。
//
// 返回 Returns:
//   - ip (string): 规范化后的 IP；非法时返回空串。
func normalizeIP(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimPrefix(strings.TrimSuffix(trimmed, "]"), "[")
	if host, _, err := net.SplitHostPort(trimmed); err == nil {
		trimmed = host
	}
	if net.ParseIP(trimmed) == nil {
		return ""
	}
	return trimmed
}

// isPrivate 判断是否为内网/回环/链路本地地址。
//
// 参数 Parameters:
//   - ip (string): IP 地址。
//
// 返回 Returns:
//   - yes (bool): 内网类地址时为 true。
func isPrivate(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.IsLoopback() || parsed.IsPrivate() || parsed.IsLinkLocalUnicast() || parsed.IsUnspecified()
}

// joinLocation 拼接归属地文本，跳过空片段。
//
// 参数 Parameters:
//   - parts (...string): 归属地片段（国家 / 省 / 市）。
//
// 返回 Returns:
//   - location (string): 以空格拼接的文本。
func joinLocation(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return strings.Join(cleaned, " ")
}
