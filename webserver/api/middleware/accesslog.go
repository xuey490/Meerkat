package middleware

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// AccessLog 按配置打印每次请求的完整地址与耗时。
//
// 与迁移前一致：仅当 access_log=true 时挂载；/healthz 与 SSE 长连接不打印。
//
// 参数 Parameters:
//   - enabled (bool): 是否开启访问日志。
//   - print (func(string)): 输出函数，由 app 层注入控制台打印实现。
//
// 返回 Returns:
//   - handler (gin.HandlerFunc): Gin 中间件。
func AccessLog(enabled bool, print func(string)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enabled {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/healthz") || strings.HasPrefix(path, "/api/v1/sse") {
			c.Next()
			return
		}
		start := time.Now()
		c.Next()
		duration := float64(time.Since(start).Microseconds()) / 1000
		print(time.Now().Format("2006-01-02 15:04:05") + " " + c.Request.Method + " " + requestURL(c) +
			" " + strconv.Itoa(c.Writer.Status()) + " " + formatMilli(duration) + " ms")
	}
}

// requestURL 还原请求的完整地址（带 scheme 与 host）。
func requestURL(c *gin.Context) string {
	host := c.Request.Host
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + host + c.Request.URL.RequestURI()
}

// formatMilli 把毫秒耗时格式化为一位小数。
func formatMilli(value float64) string {
	return strconv.FormatFloat(value, 'f', 1, 64)
}
