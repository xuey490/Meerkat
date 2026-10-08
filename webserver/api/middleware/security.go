package middleware

import "github.com/gin-gonic/gin"

// SecurityHeaders 为所有响应补充基础安全响应头。
//
// 重点是 nosniff：/upload 下存放的是用户上传内容，禁止浏览器按内容猜测类型，
// 避免把伪装成图片的 HTML/SVG 当页面执行，形成同源脚本执行风险。
//
// 返回 Returns:
//   - handler (gin.HandlerFunc): 中间件。
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "same-origin")
		c.Next()
	}
}
