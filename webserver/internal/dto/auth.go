package dto

// LoginRequest 是 POST /api/v1/auth/login 的请求体（对齐前端 LoginRequest）。
type LoginRequest struct {
	// Username 用户名。
	Username string `json:"username"`
	// Password 密码。
	Password string `json:"password"`
	// CaptchaID 验证码 ID。
	CaptchaID string `json:"captchaId"`
	// CaptchaCode 验证码内容。
	CaptchaCode string `json:"captchaCode"`
	// RememberMe 是否记住我。
	RememberMe bool `json:"rememberMe"`
}

// LoginResponse 是登录成功后的令牌返回体。
type LoginResponse struct {
	// AccessToken 访问令牌。
	AccessToken string `json:"accessToken"`
	// RefreshToken 刷新令牌。
	RefreshToken string `json:"refreshToken"`
	// TokenType 令牌类型，固定 Bearer。
	TokenType string `json:"tokenType"`
	// ExpiresIn 访问令牌有效期（秒）。
	ExpiresIn int `json:"expiresIn"`
}

// CaptchaResponse 是验证码返回体（对齐前端 CaptchaResponse）。
type CaptchaResponse struct {
	// CaptchaID 验证码 ID。
	CaptchaID string `json:"captchaId"`
	// CaptchaBase64 验证码图片 Data URI。
	CaptchaBase64 string `json:"captchaBase64"`
}

// RefreshRequest 是刷新令牌请求体（同时兼容 query 传参）。
type RefreshRequest struct {
	// RefreshToken 刷新令牌。
	RefreshToken string `json:"refreshToken" form:"refreshToken"`
}
