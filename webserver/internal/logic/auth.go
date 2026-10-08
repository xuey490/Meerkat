package logic

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// AuthLogic 负责登录相关的数据读写：用户查询、刷新令牌与登录日志。
type AuthLogic struct {
	// db 权限库会话。
	db *gorm.DB
}

// NewAuthLogic 创建认证领域操作。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//
// 返回 Returns:
//   - logic (*AuthLogic): 认证领域操作实例。
func NewAuthLogic(db *gorm.DB) *AuthLogic { return &AuthLogic{db: db} }

// UserByUsername 按用户名查询启用状态的用户。
//
// 参数 Parameters:
//   - username (string): 用户名。
//
// 返回 Returns:
//   - user (*model.SysUser): 用户实体。
//   - err (error): 不存在时返回 gorm.ErrRecordNotFound。
func (l *AuthLogic) UserByUsername(username string) (*model.SysUser, error) {
	var user model.SysUser
	if err := l.db.Where("username = ? AND status = 1", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// UserByID 按主键查询启用状态的用户。
//
// 参数 Parameters:
//   - id (string): 用户主键文本。
//
// 返回 Returns:
//   - user (*model.SysUser): 用户实体。
//   - err (error): 不存在时返回 gorm.ErrRecordNotFound。
func (l *AuthLogic) UserByID(id string) (*model.SysUser, error) {
	var user model.SysUser
	if err := l.db.First(&user, "id = ? AND status = 1", id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// SaveRefreshToken 写入刷新令牌记录。
//
// 参数 Parameters:
//   - record (*model.SysRefreshToken): 令牌记录（TokenHash 为 SHA-256 指纹）。
//
// 返回 Returns:
//   - err (error): 写入失败时返回非 nil。
func (l *AuthLogic) SaveRefreshToken(record *model.SysRefreshToken) error {
	return l.db.Create(record).Error
}

// RefreshTokenByHash 按指纹查询未吊销且未过期的刷新令牌。
//
// 参数 Parameters:
//   - hash (string): 令牌 SHA-256 指纹。
//
// 返回 Returns:
//   - record (*model.SysRefreshToken): 令牌记录。
//   - err (error): 不存在时返回 gorm.ErrRecordNotFound。
func (l *AuthLogic) RefreshTokenByHash(hash string) (*model.SysRefreshToken, error) {
	var record model.SysRefreshToken
	err := l.db.Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", hash, time.Now()).
		First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// RevokeUserTokens 吊销指定用户的全部刷新令牌（退出、改密、重置密码后调用）。
//
// 参数 Parameters:
//   - userID (int64): 用户主键。
//
// 返回 Returns:
//   - err (error): 写入失败时返回非 nil。
func (l *AuthLogic) RevokeUserTokens(userID int64) error {
	return l.db.Model(&model.SysRefreshToken{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", time.Now()).Error
}

// Session 是在线用户列表用的会话行（刷新令牌 + 用户 / 部门信息）。
type Session struct {
	// SysRefreshToken 会话（刷新令牌）实体。
	model.SysRefreshToken
	// Username 登录账号。
	Username string
	// Nickname 用户昵称。
	Nickname string
	// DeptName 所属部门名称。
	DeptName *string
}

// ActiveSessions 分页查询在线会话（未吊销且未过期的刷新令牌）。
//
// 参数 Parameters:
//   - keywords (string): 账号 / IP 模糊搜索。
//   - username (string): 账号精确匹配。
//   - ip (string): IP 精确匹配。
//   - offset (int): 偏移量。
//   - limit (int): 每页数量。
//
// 返回 Returns:
//   - rows ([]Session): 会话列表。
//   - total (int64): 满足条件的总数。
//   - err (error): 查询失败时返回非 nil。
func (l *AuthLogic) ActiveSessions(keywords, username, ip string, offset, limit int) ([]Session, int64, error) {
	base := func() *gorm.DB {
		db := l.db.Table("sa_system_refresh_token AS t").
			Joins("LEFT JOIN sa_system_user AS u ON u.id = t.user_id").
			Where("t.revoked_at IS NULL AND t.expires_at > ?", time.Now())
		if trimmed := strings.TrimSpace(username); trimmed != "" {
			db = db.Where("u.username = ?", trimmed)
		}
		if trimmed := strings.TrimSpace(ip); trimmed != "" {
			db = db.Where("t.ip = ?", trimmed)
		}
		if pattern := util.LikeKeyword(keywords); pattern != "" {
			db = db.Where(`(u.username LIKE ? ESCAPE '\' OR u.realname LIKE ? ESCAPE '\' OR t.ip LIKE ? ESCAPE '\')`,
				pattern, pattern, pattern)
		}
		return db
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, InternalErr("查询在线用户失败", err)
	}
	var rows []Session
	err := base().
		Select("t.*, u.username AS username, u.realname AS nickname, d.name AS dept_name").
		Joins("LEFT JOIN sa_system_dept AS d ON d.id = u.dept_id").
		Order("t.created_at DESC").
		Limit(limit).Offset(offset).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, InternalErr("查询在线用户失败", err)
	}
	return rows, total, nil
}

// SessionByID 按会话编号查询在线会话。
//
// 参数 Parameters:
//   - id (string): 会话编号（刷新令牌主键）。
//
// 返回 Returns:
//   - row (*Session): 会话行（含账号信息）。
//   - err (error): 不存在时返回业务错误。
func (l *AuthLogic) SessionByID(id string) (*Session, error) {
	var row Session
	err := l.db.Table("sa_system_refresh_token AS t").
		Select("t.*, u.username AS username, u.realname AS nickname, d.name AS dept_name").
		Joins("LEFT JOIN sa_system_user AS u ON u.id = t.user_id").
		Joins("LEFT JOIN sa_system_dept AS d ON d.id = u.dept_id").
		Where("t.id = ?", strings.TrimSpace(id)).
		Scan(&row).Error
	if err != nil {
		return nil, InternalErr("查询会话失败", err)
	}
	if row.ID == "" {
		return nil, NotFoundErr("会话不存在或已下线")
	}
	return &row, nil
}

// RevokeSession 吊销单个会话（强制下线）。
//
// 参数 Parameters:
//   - id (string): 会话编号。
//
// 返回 Returns:
//   - err (error): 写入失败时返回非 nil。
func (l *AuthLogic) RevokeSession(id string) error {
	if err := l.db.Model(&model.SysRefreshToken{}).
		Where("id = ? AND revoked_at IS NULL", strings.TrimSpace(id)).
		Update("revoked_at", time.Now()).Error; err != nil {
		return InternalErr("强制下线失败", err)
	}
	return nil
}

// TouchSession 更新会话的最近活跃时间（刷新令牌时调用）。
//
// 参数 Parameters:
//   - id (string): 会话编号。
//   - at (time.Time): 活跃时间。
//
// 返回 Returns:
//   - err (error): 写入失败时返回非 nil。
func (l *AuthLogic) TouchSession(id string, at time.Time) error {
	return l.db.Model(&model.SysRefreshToken{}).
		Where("id = ?", strings.TrimSpace(id)).
		Update("last_active_at", at).Error
}

// TouchLogin 更新用户最后登录时间与 IP。
//
// 参数 Parameters:
//   - userID (int64): 用户主键。
//   - ip (string): 客户端 IP。
//   - at (time.Time): 登录时间。
//
// 返回 Returns:
//   - err (error): 写入失败时返回非 nil。
func (l *AuthLogic) TouchLogin(userID int64, ip string, at time.Time) error {
	return l.db.Model(&model.SysUser{}).Where("id = ?", userID).Updates(map[string]any{
		"login_time":  at,
		"login_ip":    ip,
		"update_time": at,
	}).Error
}

// AddLoginLog 写入一条登录日志。
//
// 参数 Parameters:
//   - record (*model.SysLoginLog): 登录日志（含账号、IP、归属地、系统、浏览器、状态与消息）。
//
// 返回 Returns:
//   - err (error): 写入失败时返回非 nil。
func (l *AuthLogic) AddLoginLog(record *model.SysLoginLog) error {
	return l.db.Create(record).Error
}

// LoginLog 是构造登录日志所需的信息。
type LoginLog struct {
	// Username 登录账号。
	Username string
	// IP 客户端 IP。
	IP string
	// IPLocation IP 归属地。
	IPLocation string
	// OS 操作系统。
	OS string
	// Browser 浏览器。
	Browser string
	// Status 1 成功 / 2 失败。
	Status int
	// Message 提示消息。
	Message string
}

// BuildLoginLog 把登录日志信息转换为实体。
//
// 参数 Parameters:
//   - info (LoginLog): 登录日志信息。
//
// 返回 Returns:
//   - record (*model.SysLoginLog): 登录日志实体。
func BuildLoginLog(info LoginLog) *model.SysLoginLog {
	now := time.Now()
	return &model.SysLoginLog{
		Username:    &info.Username,
		IP:          &info.IP,
		IPLocation:  &info.IPLocation,
		OS:          &info.OS,
		Browser:     &info.Browser,
		Status:      info.Status,
		Message:     &info.Message,
		LoginTime:   &now,
		AuditFields: model.AuditFields{CreateTime: &now, UpdateTime: &now},
	}
}

// NewID 生成 UUID 主键。
//
// 返回 Returns:
//   - id (string): UUID 字符串。
func NewID() string { return uuid.NewString() }

// RandomToken 生成 32 字节随机令牌（URL 安全 Base64）。
//
// 返回 Returns:
//   - token (string): 刷新令牌明文，只会下发给客户端一次。
func RandomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return uuid.NewString() + uuid.NewString()
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// HashToken 计算令牌的 SHA-256 指纹，用于落库与查询比对。
//
// 注意 Naming：此处使用确定性哈希而不是 bcrypt，因为刷新令牌需要按指纹精确查库，
// bcrypt 每次加盐会导致查询永远不命中。
//
// 参数 Parameters:
//   - token (string): 令牌明文。
//
// 返回 Returns:
//   - hash (string): 十六进制指纹。
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
