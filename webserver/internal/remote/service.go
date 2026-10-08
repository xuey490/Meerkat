package remote

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/apperr"
)

// EndpointForm 是前端提交的连接配置表单。
//
// Secret / Passphrase 为明文（仅用于写入）；编辑时留空表示「不修改」。
type EndpointForm struct {
	// Protocol 协议：ssh / rdp。
	Protocol string `json:"protocol"`
	// Host 连接地址。
	Host string `json:"host"`
	// Port 端口。
	Port int `json:"port"`
	// Username 登录用户名。
	Username string `json:"username"`
	// AuthType 认证方式：password / key。
	AuthType string `json:"authType"`
	// Secret 密码或私钥（明文，空=不修改）。
	Secret string `json:"secret"`
	// Passphrase 私钥 passphrase（明文，空=不修改）。
	Passphrase string `json:"passphrase"`
	// Enabled 是否启用。
	Enabled bool `json:"enabled"`
	// Remark 备注。
	Remark string `json:"remark"`
}

// EndpointView 是返回前端的连接配置视图（永不含明文）。
type EndpointView struct {
	// AgentID 资产主键。
	AgentID string `json:"agentId"`
	// Protocol 协议。
	Protocol string `json:"protocol"`
	// Host 连接地址。
	Host string `json:"host"`
	// Port 端口。
	Port int `json:"port"`
	// Username 登录用户名。
	Username string `json:"username"`
	// AuthType 认证方式。
	AuthType string `json:"authType"`
	// HasSecret 是否已设置密码/私钥。
	HasSecret bool `json:"hasSecret"`
	// Enabled 是否启用。
	Enabled bool `json:"enabled"`
	// Remark 备注。
	Remark string `json:"remark"`
}

// Service 提供远程运维（SSH/RDP）的连接配置、会话审计与凭据管理。
type Service struct {
	// db 监控库（PostgreSQL）会话。
	db *gorm.DB
	// key 凭据加密密钥（32 字节）；未配置时为 nil。
	key []byte
	// recordingDir 会话录像文件目录（绝对路径）。
	recordingDir string
	// tickets 一次性连接票据仓库。
	tickets *TicketStore
	// guac Guacamole 客户端；未启用时为 nil。
	guac *GuacamoleClient
}

// New 创建远程运维服务。
//
// 参数 Parameters:
//   - db (*gorm.DB): 监控库会话。
//   - secretText (string): 凭据加密密钥文本（空表示未配置，凭据功能不可用）。
//   - recordingDir (string): 会话录像目录（绝对路径）。
//   - guac (*GuacamoleClient): Guacamole 客户端；nil 表示禁用 RDP 嵌入。
//
// 返回 Returns:
//   - service (*Service): 远程运维服务。
func New(db *gorm.DB, secretText, recordingDir string, guac *GuacamoleClient) *Service {
	service := &Service{
		db:           db,
		recordingDir: recordingDir,
		tickets:      NewTicketStore(),
		guac:         guac,
	}
	if strings.TrimSpace(secretText) != "" {
		service.key = DeriveKey(secretText)
	}
	return service
}

// Migrate 创建远程运维相关表（幂等）。
func (s *Service) Migrate() error {
	return s.db.AutoMigrate(&RemoteEndpoint{}, &SSHSession{}, &SSHCommand{}, &RDPSession{}, &DangerousCommand{})
}

// Endpoint 返回资产的连接配置；未配置时返回 (nil, nil)。
func (s *Service) Endpoint(agentID string) (*EndpointView, error) {
	var row RemoteEndpoint
	err := s.db.Where("agent_id = ?", agentID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &EndpointView{
		AgentID:   row.AgentID,
		Protocol:  row.Protocol,
		Host:      row.Host,
		Port:      row.Port,
		Username:  row.Username,
		AuthType:  row.AuthType,
		HasSecret: row.SecretCipher != "",
		Enabled:   row.Enabled,
		Remark:    row.Remark,
	}, nil
}

// SaveEndpoint 新增或更新资产的连接配置。
func (s *Service) SaveEndpoint(agentID string, form EndpointForm, operatorID int64) error {
	if err := s.requireKey(); err != nil {
		return err
	}
	protocol := normalizeProtocol(form.Protocol)
	if protocol == "" {
		return apperr.Invalid("协议仅支持 ssh 或 rdp")
	}
	if strings.TrimSpace(form.Host) == "" {
		return apperr.Invalid("连接地址不能为空")
	}
	if strings.TrimSpace(form.Username) == "" {
		return apperr.Invalid("登录用户名不能为空")
	}
	if form.Port <= 0 || form.Port > 65535 {
		if protocol == ProtocolSSH {
			form.Port = 22
		} else {
			form.Port = 3389
		}
	}
	authType := form.AuthType
	if authType == "" {
		authType = AuthPassword
	}
	if authType != AuthPassword && authType != AuthKey {
		return apperr.Invalid("认证方式仅支持密码或密钥")
	}

	var row RemoteEndpoint
	err := s.db.Where("agent_id = ?", agentID).First(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	created := errors.Is(err, gorm.ErrRecordNotFound)
	if created {
		row = RemoteEndpoint{AgentID: agentID, CreatedAt: time.Now()}
	}
	row.Protocol = protocol
	row.Host = strings.TrimSpace(form.Host)
	row.Port = form.Port
	row.Username = strings.TrimSpace(form.Username)
	row.AuthType = authType
	row.Enabled = form.Enabled
	row.Remark = strings.TrimSpace(form.Remark)
	row.UpdatedBy = operatorID
	row.UpdatedAt = time.Now()

	// 凭据只在提交了非空明文时才覆盖；留空表示保留原值（编辑场景）。
	if strings.TrimSpace(form.Secret) != "" {
		cipher, err := EncryptSecret(s.key, form.Secret)
		if err != nil {
			return err
		}
		row.SecretCipher = cipher
	}
	if strings.TrimSpace(form.Passphrase) != "" {
		cipher, err := EncryptSecret(s.key, form.Passphrase)
		if err != nil {
			return err
		}
		row.PassphraseCipher = cipher
	}

	if created {
		return s.db.Create(&row).Error
	}
	return s.db.Save(&row).Error
}

// DeleteEndpoint 删除资产的连接配置。
func (s *Service) DeleteEndpoint(agentID string) error {
	result := s.db.Where("agent_id = ?", agentID).Delete(&RemoteEndpoint{})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

// IssueSSHTicket 为资产的 SSH 连接签发一次性票据（校验配置可用）。
func (s *Service) IssueSSHTicket(agentID, operator string) (string, error) {
	endpoint, err := s.Endpoint(agentID)
	if err != nil {
		return "", err
	}
	if endpoint == nil {
		return "", apperr.NotFound("该服务器尚未配置连接信息")
	}
	if endpoint.Protocol != ProtocolSSH {
		return "", apperr.Invalid("该服务器未配置为 SSH 连接")
	}
	if !endpoint.Enabled {
		return "", apperr.Forbidden("该服务器的 SSH 连接已停用")
	}
	return s.tickets.Issue(agentID, operator, ProtocolSSH), nil
}

// IssueRDPToken 为资产的 RDP 连接换取 Guacamole 一次性 token（见 guacamole.go）。
func (s *Service) IssueRDPToken(agentID, operator string) (string, error) {
	if s.guac == nil {
		return "", apperr.Unavailable("RDP 远程桌面未启用（未配置 Guacamole）", nil)
	}
	endpoint, err := s.Endpoint(agentID)
	if err != nil {
		return "", err
	}
	if endpoint == nil {
		return "", apperr.NotFound("该服务器尚未配置连接信息")
	}
	if endpoint.Protocol != ProtocolRDP {
		return "", apperr.Invalid("该服务器未配置为 RDP 连接")
	}
	if !endpoint.Enabled {
		return "", apperr.Forbidden("该服务器的 RDP 连接已停用")
	}
	return s.guac.IssueRDPToken(s, agentID, operator, endpoint)
}

// ConsumeTicket 取用一次性票据。
func (s *Service) ConsumeTicket(token string) (Ticket, bool) {
	return s.tickets.Consume(token)
}

// SSHCredentials 返回资产 SSH 连接的解密凭据与地址信息。
func (s *Service) SSHCredentials(agentID string) (host string, port int, username, authType, secret, passphrase string, err error) {
	if err := s.requireKey(); err != nil {
		return "", 0, "", "", "", "", err
	}
	var row RemoteEndpoint
	if err := s.db.Where("agent_id = ? AND protocol = ?", agentID, ProtocolSSH).First(&row).Error; err != nil {
		return "", 0, "", "", "", "", err
	}
	secret, err = DecryptSecret(s.key, row.SecretCipher)
	if err != nil {
		return "", 0, "", "", "", "", err
	}
	passphrase, err = DecryptSecret(s.key, row.PassphraseCipher)
	if err != nil {
		return "", 0, "", "", "", "", err
	}
	return row.Host, row.Port, row.Username, row.AuthType, secret, passphrase, nil
}

// RecordingPath 生成一条会话录像的相对路径（相对录像目录，含日期子目录）。
func (s *Service) RecordingPath(sessionID uint) string {
	return filepath.Join(time.Now().Format("20060102"), formatSessionFile(sessionID)+".cast")
}

// AbsRecordingPath 把相对录像路径解析为绝对路径。
func (s *Service) AbsRecordingPath(relative string) string {
	return filepath.Join(s.recordingDir, relative)
}

// ListSessions 分页查询 SSH/RDP 会话记录（按开始时间倒序）。
func (s *Service) ListSessions(page, size int) ([]SSHSession, int64, error) {
	var total int64
	if err := s.db.Model(&SSHSession{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []SSHSession
	if err := s.db.Order("started_at DESC").
		Offset((page - 1) * size).Limit(size).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// GetSession 查询单条会话记录。
func (s *Service) GetSession(id uint, out *SSHSession) error {
	return s.db.First(out, "id = ?", id).Error
}

// SessionCommands 查询会话的命令流。
func (s *Service) SessionCommands(sessionID uint) ([]SSHCommand, error) {
	var rows []SSHCommand
	if err := s.db.Where("session_id = ?", sessionID).Order("seq").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// requireKey 校验凭据加密密钥是否已配置。
func (s *Service) requireKey() error {
	if len(s.key) != 32 {
		return apperr.Unavailable("远程连接凭据密钥未配置（remote.secret_key）", nil)
	}
	return nil
}

// normalizeProtocol 归一化协议取值。
func normalizeProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case ProtocolSSH:
		return ProtocolSSH
	case ProtocolRDP:
		return ProtocolRDP
	default:
		return ""
	}
}

// formatSessionFile 生成录像文件名（会话 ID 零填充到 8 位，便于按名称排序）。
func formatSessionFile(sessionID uint) string {
	const digits = "0123456789"
	var buf [8]byte
	// 从最低位开始逐个填入，高位补 '0'：42 → 00000042。
	for i := len(buf) - 1; i >= 0; i-- {
		buf[i] = digits[sessionID%10]
		sessionID /= 10
	}
	return string(buf[:])
}

// ---------------------------------------------------------------------------
// 高危命令规则 CRUD
// ---------------------------------------------------------------------------

// ListDangerousCommands 分页查询高危命令规则。
func (s *Service) ListDangerousCommands(page, size int, keyword string) ([]DangerousCommand, int64, error) {
	var total int64
	query := s.db.Model(&DangerousCommand{})
	if keyword != "" {
		query = query.Where("name LIKE ? OR pattern LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []DangerousCommand
	if err := query.Order("updated_at DESC").
		Offset((page - 1) * size).Limit(size).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// GetDangerousCommand 查询单条高危命令规则。
func (s *Service) GetDangerousCommand(id uint) (*DangerousCommand, error) {
	var row DangerousCommand
	if err := s.db.First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// CreateDangerousCommand 新增高危命令规则。
func (s *Service) CreateDangerousCommand(row *DangerousCommand) error {
	row.CreatedAt = time.Now()
	row.UpdatedAt = time.Now()
	return s.db.Create(row).Error
}

// UpdateDangerousCommand 更新高危命令规则。
func (s *Service) UpdateDangerousCommand(row *DangerousCommand) error {
	row.UpdatedAt = time.Now()
	return s.db.Save(row).Error
}

// DeleteDangerousCommand 删除高危命令规则。
func (s *Service) DeleteDangerousCommand(id uint) error {
	return s.db.Delete(&DangerousCommand{}, id).Error
}

// ToggleDangerousCommand 启用/禁用高危命令规则。
func (s *Service) ToggleDangerousCommand(id uint, enabled bool) error {
	return s.db.Model(&DangerousCommand{}).Where("id = ?", id).Update("enabled", enabled).Error
}

// AllEnabledDangerousCommands 返回所有已启用的高危命令规则（用于 SSH 终端拦截）。
func (s *Service) AllEnabledDangerousCommands() ([]DangerousCommand, error) {
	var rows []DangerousCommand
	if err := s.db.Where("enabled = ?", true).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// CheckDangerousCommand 检查命令是否命中高危规则。
func (s *Service) CheckDangerousCommand(command string) *MatchResult {
	rules, err := s.AllEnabledDangerousCommands()
	if err != nil || len(rules) == 0 {
		return &MatchResult{Matched: false}
	}
	return CheckCommandAgainstRules(command, rules)
}

// CheckCommandAgainstRules 用规则列表检查命令（可独立测试）。
func CheckCommandAgainstRules(command string, rules []DangerousCommand) *MatchResult {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return &MatchResult{Matched: false}
	}
	for _, r := range rules {
		pattern := strings.TrimSpace(r.Pattern)
		if pattern == "" {
			continue
		}
		switch r.MatchType {
		case "exact":
			if trimmed == pattern {
				return &MatchResult{Matched: true, RuleID: r.ID, Name: r.Name}
			}
		case "prefix":
			if strings.HasPrefix(trimmed, pattern) {
				return &MatchResult{Matched: true, RuleID: r.ID, Name: r.Name}
			}
		case "regex":
			if matched, _ := regexp.MatchString(pattern, trimmed); matched {
				return &MatchResult{Matched: true, RuleID: r.ID, Name: r.Name}
			}
		}
	}
	return &MatchResult{Matched: false}
}
