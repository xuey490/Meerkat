package service

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/iploc"
	"github.com/company/monitor-webserver/internal/logic"
	"github.com/company/monitor-webserver/internal/loginguard"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/permission"
	"github.com/company/monitor-webserver/internal/session"
	"github.com/company/monitor-webserver/internal/util"
)

// 登录相关的哨兵错误，由 api 层映射为与迁移前一致的响应码（A0210 / A0231）。
var (
	// ErrBadCredentials 用户名或密码错误。
	ErrBadCredentials = errors.New("用户名或密码错误")
	// ErrAccountLocked 账户处于锁定状态。
	ErrAccountLocked = errors.New("账户已锁定")
	// ErrInvalidRefreshToken 刷新令牌无效。
	ErrInvalidRefreshToken = errors.New("刷新令牌无效")
	// ErrInvalidUser 令牌对应用户无效。
	ErrInvalidUser = errors.New("用户无效")
)

// LoginFailureInfo 是登录失败时返回给前端的安全提示信息。
type LoginFailureInfo struct {
	// FailedAttempts 已累计的连续失败次数。
	FailedAttempts int
	// RemainingAttempts 锁定前剩余可尝试次数。
	RemainingAttempts int
	// Locked 是否已进入锁定状态。
	Locked bool
	// LockoutMinutes 锁定时长（分钟），供前端提示。
	LockoutMinutes int
	// RetryAfterSeconds 距自动解锁的剩余秒数（计数键 TTL，Redis 到期即自动解锁）。
	RetryAfterSeconds int
}

// LoginError 保留登录失败原因及安全提示信息。
type LoginError struct {
	Cause error
	Info  LoginFailureInfo
}

func (e *LoginError) Error() string { return e.Cause.Error() }
func (e *LoginError) Unwrap() error { return e.Cause }

// 登录日志状态：沿用 init.sql 中 sa_system_login_log.status 的语义。
const (
	// LoginStatusSuccess 登录/退出成功。
	LoginStatusSuccess = 1
	// LoginStatusFailed 登录失败。
	LoginStatusFailed = 2
)

// TokenConfig 是签发令牌所需的配置。
type TokenConfig struct {
	// Secret JWT 签名密钥。
	Secret string
	// AccessSeconds 访问令牌有效期（秒）。
	AccessSeconds int
	// RefreshDays 刷新令牌有效期（天）。
	RefreshDays int
}

// RequestMeta 是请求上下文信息（service 层保持对 HTTP 无感知，由 api 层采集后传入）。
type RequestMeta struct {
	// IP 客户端 IP。
	IP string
	// UserAgent 原始 User-Agent。
	UserAgent string
	// ProtectionExempt indicates a configured whitelist IP.
	ProtectionExempt bool
}

// AuthService 提供登录、刷新、退出与当前用户信息。
type AuthService struct {
	// auth 认证领域操作。
	auth *logic.AuthLogic
	// perms 权限服务（构造当前用户的角色与权限集合）。
	perms *permission.Service
	// tokens 令牌配置。
	tokens TokenConfig
	// locations IP 归属地查询器（登录地点、在线用户展示）。
	locations *iploc.Resolver
	// guard 会话失效登记（强制下线后让 access token 立即失效）。
	guard *session.Guard
	// logins 登录失败锁定（账号 + 来源 IP 双维度，状态存缓存并随 TTL 自动解锁）。
	logins *loginguard.Guard
}

// NewAuthService 创建认证应用服务。
//
// 参数 Parameters:
//   - auth (*logic.AuthLogic): 认证领域操作。
//   - perms (*permission.Service): 权限服务。
//   - tokens (TokenConfig): 令牌配置。
//   - locations (*iploc.Resolver): IP 归属地查询器，可为 nil。
//   - guard (*session.Guard): 会话失效登记，可为 nil。
//   - logins (*loginguard.Guard): 登录失败锁定守卫，可为 nil（不启用锁定）。
//
// 返回 Returns:
//   - service (*AuthService): 认证应用服务。
func NewAuthService(auth *logic.AuthLogic, perms *permission.Service, tokens TokenConfig,
	locations *iploc.Resolver, guard *session.Guard, logins *loginguard.Guard) *AuthService {
	return &AuthService{auth: auth, perms: perms, tokens: tokens, locations: locations, guard: guard, logins: logins}
}

// Login 校验账号密码并签发令牌。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文（用于归属地查询与缓存）。
//   - req (dto.LoginRequest): 登录请求（验证码已在 api 层校验）。
//   - meta (RequestMeta): 请求上下文信息，用于登录日志与在线会话。
//
// 返回 Returns:
//   - result (dto.LoginResponse): 令牌响应。
//   - err (error): 账号密码错误时返回 ErrBadCredentials。
func (s *AuthService) Login(ctx context.Context, req dto.LoginRequest, meta RequestMeta) (dto.LoginResponse, error) {
	username := strings.TrimSpace(req.Username)
	// 归属地查询结果会写入缓存，同 IP 后续登录/操作日志直接命中缓存。
	location := s.locationOf(ctx, meta.IP)
	// 锁定检查在查库之前：已锁定的账号与来源 IP 不再消耗数据库查询（也避免用户名枚举）。
	// 白名单 IP 由 Protection 中间件标记，直接跳过锁定（但成功登录仍会清零账号计数）。
	if !meta.ProtectionExempt {
		if status := s.inspectLogin(ctx, username, meta.IP); status.Locked {
			s.recordLoginLog(meta, location, username, LoginStatusFailed, "账户或来源 IP 已锁定")
			return dto.LoginResponse{}, &LoginError{Cause: ErrAccountLocked, Info: s.failureInfo(status)}
		}
	}
	user, err := s.auth.UserByUsername(username)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)) != nil {
		info := LoginFailureInfo{}
		if !meta.ProtectionExempt {
			info = s.failureInfo(s.recordLoginFailure(ctx, username, meta.IP, user))
		}
		s.recordLoginLog(meta, location, username, LoginStatusFailed, "用户名或密码错误")
		return dto.LoginResponse{}, &LoginError{Cause: ErrBadCredentials, Info: info}
	}
	if !meta.ProtectionExempt {
		s.resetLoginFailures(ctx, username)
	}
	result, err := s.issueTokens(user, meta, location)
	if err != nil {
		return dto.LoginResponse{}, apperr.Internal("生成访问令牌失败", err)
	}
	if err := s.auth.TouchLogin(user.ID, meta.IP, time.Now()); err != nil {
		slog.Warn("webserver 更新最后登录信息失败", "error", err, "userId", user.ID)
	}
	s.recordLoginLog(meta, location, user.Username, LoginStatusSuccess, "登录成功")
	return result, nil
}

// inspectLogin 查询账号与来源 IP 的锁定状态（未启用锁定时返回未锁定）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - username (string): 登录账号。
//   - ip (string): 来源 IP。
//
// 返回 Returns:
//   - status (loginguard.Status): 锁定状态。
func (s *AuthService) inspectLogin(ctx context.Context, username, ip string) loginguard.Status {
	if s.logins == nil {
		return loginguard.Status{}
	}
	return s.logins.Inspect(ctx, username, ip)
}

// recordLoginFailure 累计一次登录失败。
//
// 账号存在时额外写一条告警日志，便于区分「密码错」与「账号不存在」两类失败原因排查；
// 注意：无论账号是否存在都会计数，避免用不存在的用户名无限试错。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - username (string): 登录账号。
//   - ip (string): 来源 IP。
//   - user (*model.SysUser): 命中的用户；账号不存在时为 nil。
//
// 返回 Returns:
//   - status (loginguard.Status): 累加后的失败次数与锁定状态。
func (s *AuthService) recordLoginFailure(ctx context.Context, username, ip string, user *model.SysUser) loginguard.Status {
	if s.logins == nil {
		return loginguard.Status{}
	}
	status := s.logins.Failure(ctx, username, ip)
	if status.Locked && user != nil {
		slog.Warn("webserver 登录失败达到阈值，账号或来源 IP 已锁定",
			"username", username, "ip", ip, "scope", status.Scope,
			"attempts", status.Attempts, "lockoutMinutes", s.logins.LockoutMinutes())
	}
	return status
}

// resetLoginFailures 清零账号维度的失败次数（登录成功时调用）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - username (string): 登录账号。
func (s *AuthService) resetLoginFailures(ctx context.Context, username string) {
	if s.logins == nil {
		return
	}
	s.logins.Reset(ctx, username)
}

// failureInfo 把锁定状态转换为对外的提示信息。
//
// 参数 Parameters:
//   - status (loginguard.Status): 锁定状态。
//
// 返回 Returns:
//   - info (LoginFailureInfo): 提示信息。
func (s *AuthService) failureInfo(status loginguard.Status) LoginFailureInfo {
	info := LoginFailureInfo{
		FailedAttempts:    status.Attempts,
		RemainingAttempts: status.RemainingAttempts(),
		Locked:            status.Locked,
	}
	if !status.Locked {
		return info
	}
	info.LockoutMinutes = s.logins.LockoutMinutes()
	// RetryAfter 取计数键的剩余 TTL（Redis 到期即自动解锁）；读不到时回退到配置的锁定时长。
	info.RetryAfterSeconds = int(status.RetryAfter / time.Second)
	if info.RetryAfterSeconds <= 0 {
		info.RetryAfterSeconds = info.LockoutMinutes * 60
	}
	return info
}

// Refresh 用刷新令牌换取新的访问令牌。
//
// 参数 Parameters:
//   - rawToken (string): 刷新令牌明文。
//
// 返回 Returns:
//   - result (dto.LoginResponse): 令牌响应。
//   - err (error): 令牌无效时返回 ErrInvalidRefreshToken / ErrInvalidUser。
func (s *AuthService) Refresh(rawToken string) (dto.LoginResponse, error) {
	token := strings.TrimSpace(rawToken)
	if token == "" {
		return dto.LoginResponse{}, ErrInvalidRefreshToken
	}
	record, err := s.auth.RefreshTokenByHash(logic.HashToken(token))
	if err != nil {
		return dto.LoginResponse{}, ErrInvalidRefreshToken
	}
	user, err := s.auth.UserByID(util.TextID(record.UserID))
	if err != nil {
		return dto.LoginResponse{}, ErrInvalidUser
	}
	access, err := s.accessToken(user, record.ID)
	if err != nil {
		return dto.LoginResponse{}, apperr.Internal("生成访问令牌失败", err)
	}
	if err := s.auth.TouchSession(record.ID, time.Now()); err != nil {
		slog.Warn("webserver 更新会话活跃时间失败", "error", err, "sessionId", record.ID)
	}
	return dto.LoginResponse{
		AccessToken:  access,
		RefreshToken: token,
		TokenType:    "Bearer",
		ExpiresIn:    s.tokens.AccessSeconds,
	}, nil
}

// Logout 吊销当前用户全部刷新令牌，并让当前 access token 立即失效。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文（用于写入会话失效标记）。
//   - user (*model.SysUser): 当前用户，可为 nil（幂等返回）。
//   - sid (string): 当前 access token 所属会话编号（可为空）。
//   - meta (RequestMeta): 请求上下文信息。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (s *AuthService) Logout(ctx context.Context, user *model.SysUser, sid string, meta RequestMeta) error {
	if user == nil {
		return nil
	}
	if err := s.auth.RevokeUserTokens(user.ID); err != nil {
		slog.Warn("webserver 吊销刷新令牌失败", "error", err, "userId", user.ID)
	}
	s.revokeSid(ctx, sid)
	s.recordLoginLog(meta, s.locationOf(ctx, meta.IP), user.Username, LoginStatusSuccess, "退出成功")
	return nil
}

// SessionRevoked 判断会话是否已被强制下线。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - sid (string): 会话编号。
//
// 返回 Returns:
//   - revoked (bool): 已失效时为 true。
func (s *AuthService) SessionRevoked(ctx context.Context, sid string) bool {
	if s.guard == nil {
		return false
	}
	return s.guard.Revoked(ctx, sid)
}

// RevokeSessionID 立即失效指定会话（强制下线时调用）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - sid (string): 会话编号。
func (s *AuthService) RevokeSessionID(ctx context.Context, sid string) {
	s.revokeSid(ctx, sid)
}

// AccessTokenTTLSeconds 返回访问令牌有效期（秒），供会话失效标记设置 TTL。
//
// 返回 Returns:
//   - seconds (int): 有效期秒数。
func (s *AuthService) AccessTokenTTLSeconds() int { return s.tokens.AccessSeconds }

// revokeSid 登记会话失效标记（TTL 取访问令牌有效期）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - sid (string): 会话编号。
func (s *AuthService) revokeSid(ctx context.Context, sid string) {
	if s.guard == nil || strings.TrimSpace(sid) == "" {
		return
	}
	s.guard.Revoke(ctx, sid, time.Duration(s.tokens.AccessSeconds)*time.Second)
}

// locationOf 查询 IP 归属地（未装配查询器时返回空串）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - ip (string): 客户端 IP。
//
// 返回 Returns:
//   - location (string): 归属地文本。
func (s *AuthService) locationOf(ctx context.Context, ip string) string {
	if s.locations == nil {
		return ""
	}
	return s.locations.Resolve(ctx, ip)
}

// Me 返回当前用户的角色与权限集合。
//
// 参数 Parameters:
//   - user (*model.SysUser): 当前用户。
//
// 返回 Returns:
//   - info (dto.UserInfo): 用户信息。
//   - err (error): 未登录时返回未认证错误。
func (s *AuthService) Me(user *model.SysUser) (dto.UserInfo, error) {
	if user == nil {
		return dto.UserInfo{}, apperr.Unauthorized("登录已过期")
	}
	entry := s.perms.Of(user)
	perms := make([]string, 0, len(entry.Perms))
	for perm := range entry.Perms {
		perms = append(perms, perm)
	}
	sort.Strings(perms)
	roles := entry.Roles
	if roles == nil {
		roles = []string{}
	}
	return dto.UserInfo{
		UserID:       util.TextID(user.ID),
		Username:     user.Username,
		Nickname:     user.Realname,
		Avatar:       user.Avatar,
		Roles:        roles,
		Perms:        perms,
		IsSuperAdmin: entry.IsSuper,
	}, nil
}

// AccessTokenTTL 返回访问令牌有效期（秒），供 api 层构造响应。
//
// 返回 Returns:
//   - seconds (int): 有效期秒数。
func (s *AuthService) AccessTokenTTL() int { return s.tokens.AccessSeconds }

// UserFromToken 解析访问令牌并加载用户。
//
// 参数 Parameters:
//   - rawToken (string): Authorization 头中的 Bearer 令牌。
//
// 返回 Returns:
//   - user (*model.SysUser): 令牌对应用户。
//   - sid (string): 令牌所属会话编号（载荷 sid，可能为空）。
//   - err (error): 解析失败或用户无效时返回非 nil。
func (s *AuthService) UserFromToken(rawToken string) (*model.SysUser, string, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, "", ErrInvalidRefreshToken
	}
	token, err := jwt.Parse(rawToken, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.tokens.Secret), nil
	})
	if err != nil || !token.Valid {
		return nil, "", ErrInvalidRefreshToken
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, "", ErrInvalidRefreshToken
	}
	subject, _ := claims["sub"].(string)
	if subject == "" {
		return nil, "", ErrInvalidRefreshToken
	}
	sid, _ := claims["sid"].(string)
	user, err := s.auth.UserByID(subject)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", ErrInvalidUser
		}
		return nil, "", err
	}
	return user, sid, nil
}

// issueTokens 为用户签发访问令牌与刷新令牌，并把会话信息写入数据库。
//
// 刷新令牌记录同时充当「在线会话」：携带 IP、归属地、终端与最近活跃时间，
// 供在线用户列表展示与强制下线使用。
func (s *AuthService) issueTokens(user *model.SysUser, meta RequestMeta, location string) (dto.LoginResponse, error) {
	refresh := logic.RandomToken()
	now := time.Now()
	device, browser, os := util.ParseUserAgent(meta.UserAgent)
	record := &model.SysRefreshToken{
		ID:           logic.NewID(),
		UserID:       user.ID,
		TokenHash:    logic.HashToken(refresh),
		ExpiresAt:    now.AddDate(0, 0, s.tokens.RefreshDays),
		CreatedAt:    &now,
		IP:           util.NonEmptyPtr(meta.IP),
		IPLocation:   util.NonEmptyPtr(location),
		Device:       util.NonEmptyPtr(device),
		OS:           util.NonEmptyPtr(os),
		Browser:      util.NonEmptyPtr(browser),
		LastActiveAt: &now,
	}
	if err := s.auth.SaveRefreshToken(record); err != nil {
		return dto.LoginResponse{}, err
	}
	access, err := s.accessToken(user, record.ID)
	if err != nil {
		return dto.LoginResponse{}, err
	}
	return dto.LoginResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    s.tokens.AccessSeconds,
	}, nil
}

// accessToken 签发 JWT 访问令牌。
//
// 载荷中的 sid（会话编号）用于强制下线时立即失效：access token 本身无状态，
// 校验时通过 sid 查询会话失效标记。
func (s *AuthService) accessToken(user *model.SysUser, sid string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":      util.TextID(user.ID),
		"username": user.Username,
		"isSuper":  user.IsSuper == 1,
		"sid":      sid,
		"exp":      now.Add(time.Duration(s.tokens.AccessSeconds) * time.Second).Unix(),
		"iat":      now.Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.tokens.Secret))
}

// recordLoginLog 写入一条登录日志（失败只告警，不阻断业务流程）。
//
// 参数 Parameters:
//   - meta (RequestMeta): 请求上下文信息。
//   - location (string): 已解析的 IP 归属地（登录时同步解析，避免重复请求）。
//   - username (string): 登录账号。
//   - status (int): 1 成功 / 2 失败。
//   - message (string): 提示消息。
func (s *AuthService) recordLoginLog(meta RequestMeta, location, username string, status int, message string) {
	_, browser, os := util.ParseUserAgent(meta.UserAgent)
	// 登录日志表没有设备列，只记录系统与浏览器。
	record := logic.BuildLoginLog(logic.LoginLog{
		Username:   username,
		IP:         meta.IP,
		IPLocation: location,
		OS:         os,
		Browser:    browser,
		Status:     status,
		Message:    message,
	})
	if err := s.auth.AddLoginLog(record); err != nil {
		slog.Warn("webserver 登录日志写入失败", "error", err, "username", username)
	}
}
