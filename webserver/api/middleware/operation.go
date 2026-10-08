package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// contextResponseKey / contextCodeKey 由 api/v1 的 write 写入，供操作日志采集提示文案与响应码。
const (
	// ContextResponseKey 响应提示文案的上下文键。
	ContextResponseKey = "responseMessage"
	// ContextCodeKey 响应业务码的上下文键。
	ContextCodeKey = "responseCode"
	// successCode 成功业务码。
	successCode = "00000"
)

// maxLoggedBodySize 是操作日志记录的请求体上限（字节），超出部分截断。
const maxLoggedBodySize = 2048

// sensitiveFields 是操作日志需要脱敏的请求体字段（大小写不敏感）。
var sensitiveFields = map[string]bool{
	"password":        true,
	"oldpassword":     true,
	"newpassword":     true,
	"confirmpassword": true,
	"captchacode":     true,
	"token":           true,
	"refreshtoken":    true,
	"secret":          true,
}

// Locator 是 IP 归属地查询的最小接口（由 internal/iploc.Resolver 实现）。
//
// 中间件只调用非阻塞的 Location，命中缓存直接返回，未命中时异步预取。
type Locator interface {
	// Location 返回 IP 归属地标签（非阻塞）。
	Location(ip string) string
}

// OperateLog 构造操作日志中间件：自动记录 /api/v1 下的写操作。
//
// 记录范围：仅记录非 GET 请求，且跳过登录、刷新令牌、验证码、SSE 与日志接口自身，
// 避免日志接口自我放大。请求体在写入前做脱敏与长度截断；写库失败只告警，不阻断主流程。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//   - locator (Locator): IP 归属地查询器，可为 nil（此时归属地列留空）。
//
// 返回 Returns:
//   - handler (gin.HandlerFunc): Gin 中间件。
func OperateLog(db *gorm.DB, locator Locator) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		body := captureBody(c)
		c.Next()
		if !shouldRecord(c) {
			return
		}
		duration := float64(time.Since(start).Microseconds()) / 1000
		status := 1
		if c.Writer.Status() >= 400 || c.GetString(ContextCodeKey) != successCode {
			status = 0
		}
		ip := clientIP(c)
		location := ""
		if locator != nil {
			location = locator.Location(ip)
		}
		record := &model.SysOperLog{
			App:         textPtr(moduleFromPath(c.Request.URL.Path)),
			Method:      textPtr(c.Request.Method),
			Router:      textPtr(truncate(c.Request.URL.RequestURI(), 480)),
			ServiceName: textPtr(truncate(titleFromRequest(c), 30)),
			IP:          textPtr(ip),
			IPLocation:  textPtr(location),
			RequestData: textPtr(truncate(body, maxLoggedBodySize)),
			Duration:    textPtr(strconv.FormatFloat(duration, 'f', 2, 64)),
			ActionType:  textPtr(actionTypeFromMethod(c.Request.Method)),
			Status:      &status,
			AuditFields: model.AuditFields{CreateTime: util.NowPtr()},
		}
		if user := currentUser(c); user != nil {
			record.Username = textPtr(user.Username)
			record.OperatorID = &user.ID
		}
		device, browser, os := util.ParseUserAgent(c.Request.UserAgent())
		record.Device = textPtr(device)
		record.Browser = textPtr(browser)
		record.OS = textPtr(os)
		if message := c.GetString(ContextResponseKey); message != "" && status == 0 {
			record.ErrorMsg = textPtr(truncate(message, 480))
		}
		if err := db.Create(record).Error; err != nil {
			slog.Warn("webserver 操作日志写入失败", "error", err, "path", c.Request.URL.Path)
		}
	}
}

// shouldRecord 判断当前请求是否需要写入操作日志。
func shouldRecord(c *gin.Context) bool {
	path := c.Request.URL.Path
	if !strings.HasPrefix(path, "/api/v1/") {
		return false
	}
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	skipPrefixes := []string{
		"/api/v1/auth/login",
		"/api/v1/auth/refresh-token",
		"/api/v1/auth/captcha",
		"/api/v1/sse/",
		"/api/v1/logs",
	}
	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(path, prefix) {
			return false
		}
	}
	return true
}

// clientIP 提取客户端 IP，兼容反向代理场景。
func clientIP(c *gin.Context) string {
	if forwarded := c.GetHeader("X-Forwarded-For"); forwarded != "" {
		if index := strings.IndexByte(forwarded, ','); index > 0 {
			return strings.TrimSpace(forwarded[:index])
		}
		return strings.TrimSpace(forwarded)
	}
	if real := c.GetHeader("X-Real-IP"); real != "" {
		return strings.TrimSpace(real)
	}
	return c.ClientIP()
}

// captureBody 读取请求体（不破坏后续 handler 的读取）并做脱敏。
func captureBody(c *gin.Context) string {
	if c.Request == nil || c.Request.Body == nil {
		return ""
	}
	switch c.Request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return ""
	}
	if strings.HasPrefix(c.GetHeader("Content-Type"), "multipart/form-data") {
		// 文件上传只记录占位符，避免把二进制内容写进日志。
		return "[multipart/form-data]"
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, maxLoggedBodySize*4))
	if err != nil {
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if len(raw) == 0 {
		return ""
	}
	return sanitizePayload(raw)
}

// sanitizePayload 对 JSON 请求体脱敏，非 JSON 内容原样返回。
func sanitizePayload(raw []byte) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' && trimmed[0] != '[' {
		return string(trimmed)
	}
	var decoded any
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return string(trimmed)
	}
	scrubSecrets(decoded)
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return string(trimmed)
	}
	return string(encoded)
}

// scrubSecrets 递归地把敏感字段替换为 ***。
func scrubSecrets(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if sensitiveFields[strings.ToLower(key)] {
				typed[key] = "***"
				continue
			}
			scrubSecrets(item)
		}
	case []any:
		for _, item := range typed {
			scrubSecrets(item)
		}
	}
}

// moduleFromPath 从请求路径中取出模块名（/api/v1/users/1 → users）。
func moduleFromPath(path string) string {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(segments) >= 3 {
		return segments[2]
	}
	return ""
}

// titleFromRequest 生成操作标题：优先使用路由模板，其次使用请求路径。
func titleFromRequest(c *gin.Context) string {
	pattern := c.FullPath()
	if pattern == "" {
		pattern = c.Request.URL.Path
	}
	return c.Request.Method + " " + pattern
}

// actionTypeFromMethod 把 HTTP 方法映射为操作类型。
func actionTypeFromMethod(method string) string {
	switch method {
	case http.MethodPost:
		return "create"
	case http.MethodPut, http.MethodPatch:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return "query"
	}
}

// truncate 截断超长文本，避免单条日志过大。
func truncate(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	trimmed := value[:limit]
	for len(trimmed) > 0 && !utf8.ValidString(trimmed) {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed + "..."
}

// textPtr 返回字符串指针，便于写入可空列。
func textPtr(value string) *string {
	copied := value
	return &copied
}
