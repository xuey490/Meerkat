package v1

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/captcha"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/service"
)

// 认证相关的响应码（与迁移前保持一致）。
const (
	// captchaCode 验证码校验失败。
	captchaCode = "A0211"
	// credentialsCode 用户名或密码错误。
	credentialsCode = "A0210"
	// refreshCode 刷新令牌无效。
	refreshCode = "A0231"
)

// CaptchaConfig 是验证码相关配置。
type CaptchaConfig struct {
	// Enabled 是否启用验证码。
	Enabled bool
	// Width 图片宽度。
	Width int
	// Height 图片高度。
	Height int
}

// AuthAPI 是认证模块的 HTTP 控制器。
type AuthAPI struct {
	// auth 认证应用服务。
	auth *service.AuthService
	// captcha 验证码仓储，可为 nil（未启用）。
	captcha *captcha.Store
	// captchaCfg 验证码配置。
	captchaCfg CaptchaConfig
}

// NewAuthAPI 创建认证控制器。
//
// 参数 Parameters:
//   - auth (*service.AuthService): 认证应用服务。
//   - captchaStore (*captcha.Store): 验证码仓储，可为 nil。
//   - captchaCfg (CaptchaConfig): 验证码配置。
//
// 返回 Returns:
//   - api (*AuthAPI): 认证控制器。
func NewAuthAPI(auth *service.AuthService, captchaStore *captcha.Store, captchaCfg CaptchaConfig) *AuthAPI {
	return &AuthAPI{auth: auth, captcha: captchaStore, captchaCfg: captchaCfg}
}

// RegisterPublic 注册无需登录的认证路由（验证码、登录、刷新令牌）。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): /api/v1 路由组。
func (h *AuthAPI) RegisterPublic(group *gin.RouterGroup) {
	auth := group.Group("/auth")
	auth.GET("/captcha", h.Captcha)
	auth.POST("/login", h.Login)
	auth.POST("/refresh-token", h.Refresh)
}

// RegisterAuthed 注册需要登录的认证路由（退出、当前用户）。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
func (h *AuthAPI) RegisterAuthed(group *gin.RouterGroup) {
	group.DELETE("/auth/logout", h.Logout)
	group.GET("/users/me", h.Me)
}

// Captcha 处理 GET /api/v1/auth/captcha：生成验证码并返回 ID 与图片 Data URI。
//
// 响应带 no-store，避免浏览器缓存导致验证码与 ID 不匹配；
// 配置 captcha.enabled=false 时返回空 ID 与空图片，登录将跳过验证码校验。
func (h *AuthAPI) Captcha(c *gin.Context) {
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
	c.Header("Pragma", "no-cache")
	if !h.captchaCfg.Enabled || h.captcha == nil {
		OK(c, dto.CaptchaResponse{})
		return
	}
	id, text := h.captcha.Generate()
	uri, err := captcha.Render(text, h.captchaCfg.Width, h.captchaCfg.Height)
	if err != nil {
		write(c, http.StatusInternalServerError, "B0001", "生成验证码失败", nil)
		return
	}
	OK(c, dto.CaptchaResponse{CaptchaID: id, CaptchaBase64: uri})
}

// Login 处理 POST /api/v1/auth/login：验证码校验 → 密码校验 → 签发令牌 → 记录登录日志。
func (h *AuthAPI) Login(c *gin.Context) {
	var req dto.LoginRequest
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Username) == "" || req.Password == "" {
		BadRequest(c, "用户名和密码不能为空")
		return
	}
	// 验证码先于密码校验：一次性校验失败即拒绝，避免密码被低成本暴破。
	if !h.verifyCaptcha(c, req.CaptchaID, req.CaptchaCode) {
		return
	}
	result, err := h.auth.Login(c.Request.Context(), req,
		service.RequestMeta{IP: ClientIP(c), UserAgent: c.Request.UserAgent()})
	if err != nil {
		if errors.Is(err, service.ErrBadCredentials) {
			write(c, http.StatusUnauthorized, credentialsCode, "用户名或密码错误", nil)
			return
		}
		Fail(c, err, "登录失败")
		return
	}
	OK(c, result)
}

// Refresh 处理 POST /api/v1/auth/refresh-token：用刷新令牌换取新的访问令牌。
//
// 兼容前端两种调用方式：JSON body 与 query 参数（前端使用 query）。
func (h *AuthAPI) Refresh(c *gin.Context) {
	var req dto.RefreshRequest
	_ = c.ShouldBindJSON(&req)
	token := strings.TrimSpace(req.RefreshToken)
	if token == "" {
		token = strings.TrimSpace(c.Query("refreshToken"))
	}
	result, err := h.auth.Refresh(token)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidRefreshToken):
			write(c, http.StatusUnauthorized, refreshCode, "刷新令牌无效", nil)
		case errors.Is(err, service.ErrInvalidUser):
			write(c, http.StatusUnauthorized, refreshCode, "用户无效", nil)
		default:
			Fail(c, err, "刷新令牌失败")
		}
		return
	}
	OK(c, result)
}

// Logout 处理 DELETE /api/v1/auth/logout：吊销当前用户全部刷新令牌。
//
// 同时登记当前会话失效标记，使本次 access token 立即失效（TTL 取访问令牌有效期）。
func (h *AuthAPI) Logout(c *gin.Context) {
	meta := service.RequestMeta{IP: ClientIP(c), UserAgent: c.Request.UserAgent()}
	if err := h.auth.Logout(c.Request.Context(), CurrentUser(c), CurrentSessionID(c), meta); err != nil {
		Fail(c, err, "退出失败")
		return
	}
	OK(c, nil)
}

// Me 处理 GET /api/v1/users/me：返回当前用户信息、角色与权限集合。
func (h *AuthAPI) Me(c *gin.Context) {
	info, err := h.auth.Me(CurrentUser(c))
	if err != nil {
		Fail(c, err, "查询用户信息失败")
		return
	}
	OK(c, info)
}

// verifyCaptcha 校验登录请求中的验证码，并在失败时直接写出响应。
//
// 返回 Returns:
//   - ok (bool): true 表示校验通过或功能未启用；false 表示已写出错误响应。
func (h *AuthAPI) verifyCaptcha(c *gin.Context, id, code string) bool {
	if !h.captchaCfg.Enabled || h.captcha == nil {
		return true
	}
	if err := h.captcha.Verify(id, code); err != nil {
		write(c, http.StatusBadRequest, captchaCode, err.Error(), nil)
		return false
	}
	return true
}
