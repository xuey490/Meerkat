package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// maxXSSBodyBytes 是参与 XSS 清洗的请求体上限；超过则原样透传（多为文件上传）。
const maxXSSBodyBytes = 1 << 20

// xssDangerousTags 是需要清洗的脚本类标签。
var xssDangerousTags = []string{
	"script", "iframe", "object", "embed", "applet", "frame", "frameset", "style", "meta", "link", "base",
}

// xssPairedTagPatterns 匹配「成对标签及其内容」（RE2 不支持反向引用，按标签名逐个生成）。
var xssPairedTagPatterns = buildPairedTagPatterns()

// xssTagPattern 匹配残留的孤立脚本类标签。
var xssTagPattern = regexp.MustCompile(`(?is)<\s*/?\s*(script|iframe|object|embed|applet|frame|frameset|style|meta|link|base)\b[^>]*>`)

// xssURLPattern 匹配 javascript:/vbscript: 伪协议。
var xssURLPattern = regexp.MustCompile(`(?i)(javascript|vbscript)\s*:`)

// xssEventHandlerPattern 匹配 on* 内联事件属性。
var xssEventHandlerPattern = regexp.MustCompile(`(?i)\son[a-z]{3,}\s*=`)

// XSS 清洗请求中的 XSS 特征（query 参数、表单字段、JSON 请求体中的字符串）。
//
// 采用「黑名单特征移除」而不是全量 HTML 转义：站点存在公告富文本这类合法 HTML 字段，
// 全量转义会把正常内容也破坏掉。清洗范围：
//   - 成对的脚本类标签连同内容（<script>...</script> 等）；
//   - 残留的孤立脚本类标签；
//   - javascript:/vbscript: 伪协议；
//   - on* 内联事件属性。
//
// 返回 Returns:
//   - handler (gin.HandlerFunc): Gin 中间件。
func XSS() gin.HandlerFunc {
	return func(c *gin.Context) {
		if raw := c.Request.URL.RawQuery; raw != "" {
			c.Request.URL.RawQuery = sanitizeQuery(raw)
		}
		sanitizeFormValues(c)
		sanitizeJSONBody(c)
		c.Next()
	}
}

// buildPairedTagPatterns 为每个脚本类标签生成「标签 + 内容」的匹配式。
//
// 返回 Returns:
//   - patterns ([]*regexp.Regexp): 匹配式列表。
func buildPairedTagPatterns() []*regexp.Regexp {
	patterns := make([]*regexp.Regexp, 0, len(xssDangerousTags))
	for _, tag := range xssDangerousTags {
		patterns = append(patterns, regexp.MustCompile(
			`(?is)<\s*`+tag+`\b[^>]*>.*?<\s*/\s*`+tag+`\s*>`))
	}
	return patterns
}

// sanitizeQuery 清洗 URL 查询串中的每个参数值。
//
// 参数 Parameters:
//   - rawQuery (string): 原始查询串。
//
// 返回 Returns:
//   - cleaned (string): 清洗后的查询串。
func sanitizeQuery(rawQuery string) string {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery
	}
	changed := false
	for key, list := range values {
		for index := range list {
			cleaned := sanitizeValue(list[index])
			if cleaned != list[index] {
				list[index] = cleaned
				changed = true
			}
		}
		values[key] = list
	}
	if !changed {
		return rawQuery
	}
	return values.Encode()
}

// sanitizeFormValues 清洗 urlencoded 表单与 multipart 表单的文本字段。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
func sanitizeFormValues(c *gin.Context) {
	if !IsWriteMethod(c.Request.Method) {
		return
	}
	switch contentType := c.ContentType(); {
	case strings.HasPrefix(contentType, "application/x-www-form-urlencoded"):
		if err := c.Request.ParseForm(); err != nil || c.Request.PostForm == nil {
			return
		}
		for key, list := range c.Request.PostForm {
			for index := range list {
				list[index] = sanitizeValue(list[index])
			}
			c.Request.PostForm[key] = list
		}
	case strings.HasPrefix(contentType, "multipart/form-data"):
		// 只清洗表单文本字段，文件内容原样保留。
		if err := c.Request.ParseMultipartForm(maxXSSBodyBytes); err != nil {
			return
		}
		if c.Request.MultipartForm == nil {
			return
		}
		for key, list := range c.Request.MultipartForm.Value {
			for index := range list {
				list[index] = sanitizeValue(list[index])
			}
			c.Request.MultipartForm.Value[key] = list
		}
	}
}

// sanitizeJSONBody 清洗 JSON 请求体中的字符串字段（保留结构，重新序列化回写）。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
func sanitizeJSONBody(c *gin.Context) {
	if !IsWriteMethod(c.Request.Method) || !strings.HasPrefix(c.ContentType(), "application/json") {
		return
	}
	if c.Request.ContentLength <= 0 || c.Request.ContentLength > maxXSSBodyBytes {
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxXSSBodyBytes+1))
	if err != nil {
		return
	}
	restore := func(data []byte) {
		c.Request.Body = io.NopCloser(bytes.NewReader(data))
		c.Request.ContentLength = int64(len(data))
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		// 非 JSON（或解析失败）时原样透传，交给绑定层报错。
		restore(body)
		return
	}
	encoded, err := json.Marshal(sanitizeJSONValue(decoded))
	if err != nil {
		restore(body)
		return
	}
	restore(encoded)
}

// sanitizeJSONValue 递归清洗 JSON 结构中的字符串。
//
// 参数 Parameters:
//   - value (any): 已解析的 JSON 值。
//
// 返回 Returns:
//   - cleaned (any): 清洗后的值。
func sanitizeJSONValue(value any) any {
	switch typed := value.(type) {
	case string:
		return sanitizeValue(typed)
	case []any:
		for index := range typed {
			typed[index] = sanitizeJSONValue(typed[index])
		}
		return typed
	case map[string]any:
		for key, item := range typed {
			typed[key] = sanitizeJSONValue(item)
		}
		return typed
	default:
		return value
	}
}

// sanitizeValue 移除单个字符串中的 XSS 特征。
//
// 参数 Parameters:
//   - value (string): 原始字符串。
//
// 返回 Returns:
//   - cleaned (string): 清洗后的字符串。
func sanitizeValue(value string) string {
	if !needsSanitize(value) {
		return value
	}
	cleaned := value
	for _, pattern := range xssPairedTagPatterns {
		cleaned = pattern.ReplaceAllString(cleaned, "")
	}
	cleaned = xssTagPattern.ReplaceAllString(cleaned, "")
	cleaned = xssURLPattern.ReplaceAllString(cleaned, "")
	cleaned = xssEventHandlerPattern.ReplaceAllString(cleaned, " ")
	return cleaned
}

// needsSanitize 快速判断字符串是否可能含 XSS 特征（避免无谓的正则开销）。
//
// 参数 Parameters:
//   - value (string): 原始字符串。
//
// 返回 Returns:
//   - needed (bool): 需要清洗返回 true。
func needsSanitize(value string) bool {
	if value == "" {
		return false
	}
	if strings.ContainsAny(value, "<") {
		return true
	}
	lower := strings.ToLower(value)
	return strings.Contains(lower, "javascript") || strings.Contains(lower, "vbscript")
}
