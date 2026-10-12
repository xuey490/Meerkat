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

// GuacOptions 是远程桌面（guacd）的运行参数。
type GuacOptions struct {
	// Client guacd 连接工厂；nil 表示未启用远程桌面。
	Client *GuacdClient
	// LocalRecordingDir 录像文件读取基准目录（本进程视角的绝对路径）。
	LocalRecordingDir string
	// HandshakeTimeout 单次握手的时间上限。
	HandshakeTimeout time.Duration
	// IdleTimeout 会话空闲超时；<=0 表示不限制。
	IdleTimeout time.Duration
}

// Service 提供远程运维（SSH / RDP / VNC）的连接配置、会话审计与凭据管理。
type Service struct {
	// db 监控库（PostgreSQL）会话。
	db *gorm.DB
	// key 凭据加密密钥（32 字节）；未配置时为 nil。
	key []byte
	// recordingDir SSH 会话录像文件目录（绝对路径，asciinema v2 cast）。
	recordingDir string
	// tickets 一次性连接票据仓库。
	tickets *TicketStore
	// guac 远程桌面（guacd）参数。
	guac GuacOptions
	// serviceCache 服务列表缓存（见 listcache.go）。
	serviceCache *listCache[ServiceUnit]
	// processCache 进程列表缓存。
	processCache *listCache[ProcessInfo]
}

// New 创建远程运维服务。
//
// 参数 Parameters:
//   - db (*gorm.DB): 监控库会话。
//   - secretText (string): 凭据加密密钥文本（空表示未配置，凭据功能不可用）。
//   - recordingDir (string): SSH 会话录像目录（绝对路径）。
//   - guac (GuacOptions): 远程桌面参数（Client 为 nil 表示未启用）。
//
// 返回 Returns:
//   - service (*Service): 远程运维服务。
func New(db *gorm.DB, secretText, recordingDir string, guac GuacOptions) *Service {
	service := &Service{
		db:           db,
		recordingDir: recordingDir,
		tickets:      NewTicketStore(),
		guac:         guac,
		serviceCache: newListCache[ServiceUnit](serviceListTTL),
		processCache: newListCache[ProcessInfo](processListTTL),
	}
	if strings.TrimSpace(secretText) != "" {
		service.key = DeriveKey(secretText)
	}
	return service
}

// Migrate 创建远程运维相关表、回填新增列，并把连接配置的唯一约束升级为「按协议」。
func (s *Service) Migrate() error {
	if err := s.db.AutoMigrate(&RemoteEndpoint{}, &SSHSession{}, &SSHCommand{}, &RDPSession{}, &DangerousCommand{}); err != nil {
		return err
	}
	if err := s.backfillEndpointProtocol(); err != nil {
		return err
	}
	if err := s.dropLegacyAgentUniqueIndex(); err != nil {
		return err
	}
	return s.backfillSessionColumns()
}

// backfillEndpointProtocol 把 remote_endpoints 上缺失的 protocol 补成 ssh。
//
// 为什么必须做：唯一约束改成 (agent_id, protocol) 后，protocol 为 NULL 的行不参与
// 唯一性判断（SQL 里 NULL 不等于 NULL），同一台机器可以插进多条空 protocol 记录，
// 之后按协议精确取配置就会取到不确定的一条。历史上 protocol 缺失的唯一来源是
// 「只支持 SSH」的旧版本，因此统一回填为 ssh。
//
// 条件带 IS NULL / ”，天然幂等，不会覆盖已写的值。
//
// 返回 Returns:
//   - err (error): 回填失败时返回非 nil。
func (s *Service) backfillEndpointProtocol() error {
	return s.db.Exec(
		`update remote_endpoints set protocol = ? where protocol is null or protocol = ''`,
		ProtocolSSH).Error
}

// dropLegacyAgentUniqueIndex 删除 remote_endpoints 上「仅约束 agent_id」的唯一索引。
//
// 为什么必须显式删除：GORM 的 AutoMigrate 只加不删索引。把唯一约束从 agent_id 改成
// (agent_id, protocol) 之后，新索引会被建出来，但旧的单列唯一索引依然存在——
// 「一台机器配 SSH + VNC」照样插不进去，而且失败发生在写入时，报错只有一句
// duplicate key，排查成本很高。这条一旦漏掉，前面的模型改动等于白改。
//
// 这里按「唯一索引、非主键、只有一列、且该列是 agent_id」来匹配，而不是硬编码索引名：
// 索引名取决于 GORM 版本与建表历史（可能是 idx_remote_endpoints_agent_id，也可能是
// 更早的 uk_agent_id），写死容易失配却毫无提示。
//
// 返回 Returns:
//   - err (error): 查询或删除失败时返回非 nil。
func (s *Service) dropLegacyAgentUniqueIndex() error {
	const stmt = `
do $$
declare
	idx_name text;
begin
	for idx_name in
		select i.relname
		from pg_index x
		join pg_class t on t.oid = x.indrelid
		join pg_class i on i.oid = x.indexrelid
		where t.relname = 'remote_endpoints'
		  and x.indisunique
		  and not x.indisprimary
		  and x.indnatts = 1
		  and (select a.attname from pg_attribute a
		       where a.attrelid = t.oid and a.attnum = x.indkey[0]) = 'agent_id'
	loop
		execute format('drop index if exists %I', idx_name);
	end loop;
end $$`
	return s.db.Exec(stmt).Error
}

// backfillSessionColumns 把 rdp_sessions 新增列在存量行上的 NULL 补齐。
//
// 为什么必须做：新增列在已有行上是 NULL，而 Go 侧字段是值类型（string / int64），
// 读到 NULL 会直接报 "converting NULL to string is unsupported"，让整表查询失败——
// 不是显示不准，而是功能不可用。条件带 IS NULL，天然幂等，也不会覆盖后续写入的值。
//
// 返回 Returns:
//   - err (error): 回填失败时返回非 nil。
func (s *Service) backfillSessionColumns() error {
	return s.db.Exec(`update rdp_sessions set
		protocol = coalesce(protocol, 'rdp'),
		client_ip = coalesce(client_ip, ''),
		guac_connection_id = coalesce(guac_connection_id, ''),
		close_reason = coalesce(close_reason, ''),
		recording_path = coalesce(recording_path, ''),
		bytes_in = coalesce(bytes_in, 0),
		bytes_out = coalesce(bytes_out, 0),
		duration_sec = coalesce(duration_sec, 0)
		where protocol is null or client_ip is null or guac_connection_id is null
		   or close_reason is null or recording_path is null
		   or bytes_in is null or bytes_out is null or duration_sec is null`).Error
}

// Endpoint 返回资产在指定协议下的连接配置；未配置时返回 (nil, nil)。
//
// 必须带 protocol：同一台资产可以并存多条配置（SSH 供终端、RDP/VNC 供远程桌面），
// 只按 agent_id 取会取到不确定的一条——那是「一台机器只能用一个协议」时代的写法。
//
// 参数 Parameters:
//   - agentID (string): 资产主键。
//   - protocol (string): 已归一化的协议（ssh / rdp / vnc）。
//
// 返回 Returns:
//   - view (*EndpointView): 连接配置视图；未配置时返回 nil。
//   - err (error): 查询失败时返回非 nil。
func (s *Service) Endpoint(agentID, protocol string) (*EndpointView, error) {
	var row RemoteEndpoint
	err := s.db.Where("agent_id = ? AND protocol = ?", agentID, protocol).First(&row).Error
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

// FirstEndpoint 返回资产任意一条连接配置，按 ssh → rdp → vnc 的优先级取第一条。
//
// 存在的意义：改造前一台资产只有一条配置，调用方（尤其是旧前端）不指定协议也能取到。
// 现在一台机器可以并存多条，这个「不指定协议」的入口仍然需要一个确定的行为——
// 用固定优先级而不是数据库返回顺序，否则同一份数据在不同时刻可能取到不同协议。
//
// 参数 Parameters:
//   - agentID (string): 资产主键。
//
// 返回 Returns:
//   - view (*EndpointView): 命中的连接配置；一条都没有时为 nil。
//   - err (error): 查询失败时返回非 nil。
func (s *Service) FirstEndpoint(agentID string) (*EndpointView, error) {
	for _, protocol := range []string{ProtocolSSH, ProtocolRDP, ProtocolVNC} {
		view, err := s.Endpoint(agentID, protocol)
		if err != nil {
			return nil, err
		}
		if view != nil {
			return view, nil
		}
	}
	return nil, nil
}

// SaveEndpoint 新增或更新资产的连接配置。
func (s *Service) SaveEndpoint(agentID string, form EndpointForm, operatorID int64) error {
	if err := s.requireKey(); err != nil {
		return err
	}
	protocol := normalizeProtocol(form.Protocol)
	if protocol == "" {
		return apperr.Invalid("协议仅支持 ssh、rdp 或 vnc")
	}
	if strings.TrimSpace(form.Host) == "" {
		return apperr.Invalid("连接地址不能为空")
	}
	// VNC 认证只有密码，guacd 侧不注入 username（见 BuildConnectPlan），因此允许留空；
	// SSH / RDP 必须给出登录账号。
	if protocol != ProtocolVNC && strings.TrimSpace(form.Username) == "" {
		return apperr.Invalid("登录用户名不能为空")
	}
	if form.Port <= 0 || form.Port > 65535 {
		form.Port = defaultEndpointPort(protocol)
	}
	authType := form.AuthType
	if authType == "" {
		authType = AuthPassword
	}
	if authType != AuthPassword && authType != AuthKey {
		return apperr.Invalid("认证方式仅支持密码或密钥")
	}

	var row RemoteEndpoint
	err := s.db.Where("agent_id = ? AND protocol = ?", agentID, protocol).First(&row).Error
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

// DeleteEndpoint 删除资产在指定协议下的连接配置。
//
// 必须带 protocol：不带会把该资产的 SSH、RDP、VNC 配置一并删掉——用户点一次
// 「删除配置」却丢掉另外两条看不出关联的凭据，几乎无法自查。
//
// 参数 Parameters:
//   - agentID (string): 资产主键。
//   - protocol (string): 已归一化的协议（ssh / rdp / vnc）。
//
// 返回 Returns:
//   - err (error): 删除失败时返回非 nil。
func (s *Service) DeleteEndpoint(agentID, protocol string) error {
	normalized := normalizeProtocol(protocol)
	if normalized == "" {
		return apperr.Invalid("协议仅支持 ssh、rdp 或 vnc")
	}
	result := s.db.Where("agent_id = ? AND protocol = ?", agentID, normalized).Delete(&RemoteEndpoint{})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

// IssueSSHTicket 为资产的 SSH 连接签发一次性票据（校验配置可用）。
func (s *Service) IssueSSHTicket(agentID, operator string) (string, error) {
	// 按 SSH 协议精确取配置：同一台机器可能同时存在 rdp / vnc 配置，
	// 取「第一条」会取到不确定的记录。取到的必然是 ssh，故不再重复比对协议。
	endpoint, err := s.Endpoint(agentID, ProtocolSSH)
	if err != nil {
		return "", err
	}
	if endpoint == nil {
		return "", apperr.NotFound("该服务器尚未配置 SSH 连接，请先在「连接信息」中添加")
	}
	if !endpoint.Enabled {
		return "", apperr.Forbidden("该服务器的 SSH 连接已停用")
	}
	return s.tickets.Issue(agentID, operator, ProtocolSSH), nil
}

// IssueRemoteTicket 为资产的远程桌面连接（RDP / VNC）签发一次性 WS 票据。
//
// 与 SSH 复用同一套票据机制：浏览器先用受鉴权的 REST 接口换票据，再用 ?ticket= 建立 WS。
// 连接地址与凭据都不经过浏览器，由代理层在握手期权威注入。
//
// 参数 Parameters:
//   - agentID (string): 资产主键。
//   - protocol (string): 期望协议（rdp / vnc）；空表示自动探测（兼容未传协议的老前端）。
//   - operator (string): 操作人账号。
//
// 返回 Returns:
//   - ticket (string): 一次性票据。
//   - protocol (string): 实际协议（rdp / vnc）。
//   - err (error): 校验不通过时返回非 nil。
func (s *Service) IssueRemoteTicket(agentID, protocol, operator string) (string, string, error) {
	if s.guac.Client == nil {
		return "", "", apperr.Unavailable("远程桌面未启用（未配置 guacd）", nil)
	}
	endpoint, err := s.remoteDesktopEndpoint(agentID, protocol)
	if err != nil {
		return "", "", err
	}
	if endpoint == nil {
		// 明确告知「缺的是哪个协议」：只说「未配置连接信息」会让操作者以为
		// 这台机器的连接配置整个丢了，而实际上可能只是没配 VNC。
		return "", "", apperr.NotFound("该服务器尚未配置 " + remoteDesktopLabel(protocol) +
			" 连接，请先在「连接信息」中按该协议添加")
	}
	if !endpoint.Enabled {
		return "", "", apperr.Forbidden("该服务器的 " + strings.ToUpper(endpoint.Protocol) + " 连接已停用")
	}
	return s.tickets.Issue(agentID, operator, endpoint.Protocol), endpoint.Protocol, nil
}

// remoteDesktopEndpoint 取资产在目标协议下的远程桌面配置。
//
// protocol 为空时按 rdp → vnc 顺序探测：这条路径只为兼容未传协议的老前端而保留。
// 新前端一律显式传协议——否则「点 VNC 却连上 RDP」会以「进去发现协议不对」的形式
// 表现出来，很难归因到入口。
//
// 参数 Parameters:
//   - agentID (string): 资产主键。
//   - protocol (string): 期望协议（rdp / vnc）；空表示自动探测。
//
// 返回 Returns:
//   - endpoint (*EndpointView): 命中的连接配置；都没有时为 nil。
//   - err (error): 协议非法或查询失败时返回非 nil。
func (s *Service) remoteDesktopEndpoint(agentID, protocol string) (*EndpointView, error) {
	if strings.TrimSpace(protocol) != "" {
		normalized := normalizeRemoteProtocol(protocol)
		if normalized == "" {
			return nil, apperr.Invalid("远程桌面协议仅支持 rdp 或 vnc")
		}
		return s.Endpoint(agentID, normalized)
	}
	for _, candidate := range []string{ProtocolRDP, ProtocolVNC} {
		endpoint, err := s.Endpoint(agentID, candidate)
		if err != nil {
			return nil, err
		}
		if endpoint != nil {
			return endpoint, nil
		}
	}
	return nil, nil
}

// remoteDesktopLabel 把协议转成界面提示用的大写名称。
//
// 参数 Parameters:
//   - protocol (string): 协议；空表示未指定。
//
// 返回 Returns:
//   - label (string): 提示名称；未指定时为「RDP 或 VNC」。
func remoteDesktopLabel(protocol string) string {
	if normalized := normalizeRemoteProtocol(protocol); normalized != "" {
		return strings.ToUpper(normalized)
	}
	return "RDP 或 VNC"
}

// isRemoteDesktopProtocol 判断协议是否属于远程桌面（由 guacd 承载）。
func isRemoteDesktopProtocol(protocol string) bool {
	return protocol == ProtocolRDP || protocol == ProtocolVNC
}

// RemoteDesktopEnabled 返回远程桌面是否可用（供前端决定是否显示入口）。
func (s *Service) RemoteDesktopEnabled() bool { return s.guac.Client != nil }

// RecordingEnabled 返回是否开启会话录像。
//
// 供前端在会话中提示操作人「本次操作正在被记录」，与后端实际是否录像保持一致。
func (s *Service) RecordingEnabled() bool {
	return s.guac.Client != nil && s.guac.Client.RecordingEnabled()
}

// Guacd 返回 guacd 连接工厂；未启用时为 nil。
func (s *Service) Guacd() *GuacdClient { return s.guac.Client }

// LocalRecordingDir 返回录像读取基准目录（本进程视角）。
func (s *Service) LocalRecordingDir() string { return s.guac.LocalRecordingDir }

// HandshakeTimeout 返回单次握手的时间上限。
func (s *Service) HandshakeTimeout() time.Duration { return s.guac.HandshakeTimeout }

// IdleTimeout 返回会话空闲超时。
func (s *Service) IdleTimeout() time.Duration { return s.guac.IdleTimeout }

// DecryptEndpointSecret 返回资产连接配置与解密后的密码（供代理层在握手期注入）。
//
// 参数 Parameters:
//   - agentID (string): 资产主键。
//   - protocol (string): 协议（rdp / vnc）。
//
// 返回 Returns:
//   - endpoint (*RemoteEndpoint): 连接配置。
//   - password (string): 解密后的密码。
//   - err (error): 密钥未配置或查询失败时返回非 nil。
func (s *Service) DecryptEndpointSecret(agentID, protocol string) (*RemoteEndpoint, string, error) {
	if err := s.requireKey(); err != nil {
		return nil, "", err
	}
	var row RemoteEndpoint
	if err := s.db.Where("agent_id = ? AND protocol = ?", agentID, protocol).First(&row).Error; err != nil {
		return nil, "", err
	}
	password, err := DecryptSecret(s.key, row.SecretCipher)
	if err != nil {
		return nil, "", err
	}
	return &row, password, nil
}

// RemoteSessionQuery 是远程桌面会话列表的查询条件。
type RemoteSessionQuery struct {
	// Protocol 协议筛选：rdp / vnc；空表示全部。
	Protocol string
	// Operator 操作人筛选（模糊匹配）。
	Operator string
	// AgentID 资产筛选（模糊匹配）。
	AgentID string
	// StartFrom 开始时间下界（含）。
	StartFrom *time.Time
	// StartTo 开始时间上界（含）。
	StartTo *time.Time
	// Page 页码，从 1 开始。
	Page int
	// Size 每页条数。
	Size int
}

// ListRemoteSessions 分页查询远程桌面会话（按开始时间倒序）。
//
// 参数 Parameters:
//   - query (RemoteSessionQuery): 查询条件。
//
// 返回 Returns:
//   - rows ([]RDPSession): 当前页数据。
//   - total (int64): 符合条件的总数。
//   - err (error): 查询失败时返回非 nil。
func (s *Service) ListRemoteSessions(query RemoteSessionQuery) ([]RDPSession, int64, error) {
	statement := s.db.Model(&RDPSession{})
	if query.Protocol != "" {
		statement = statement.Where("protocol = ?", query.Protocol)
	}
	if query.Operator != "" {
		statement = statement.Where("operator ILIKE ?", "%"+query.Operator+"%")
	}
	if query.AgentID != "" {
		statement = statement.Where("agent_id ILIKE ?", "%"+query.AgentID+"%")
	}
	if query.StartFrom != nil {
		statement = statement.Where("started_at >= ?", *query.StartFrom)
	}
	if query.StartTo != nil {
		statement = statement.Where("started_at <= ?", *query.StartTo)
	}

	var total int64
	if err := statement.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, size := query.Page, query.Size
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	var rows []RDPSession
	if err := statement.Order("started_at DESC").
		Offset((page - 1) * size).Limit(size).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// GetRemoteSession 查询单条远程桌面会话。
//
// 参数 Parameters:
//   - id (uint): 会话主键。
//
// 返回 Returns:
//   - row (*RDPSession): 会话记录。
//   - err (error): 不存在或查询失败时返回非 nil。
func (s *Service) GetRemoteSession(id uint) (*RDPSession, error) {
	var row RDPSession
	if err := s.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &row, nil
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

// SessionQuery 是 SSH 会话审计列表的查询条件。
type SessionQuery struct {
	// Operator 操作人筛选（模糊匹配）。
	Operator string
	// Username 登录目标机的用户名筛选（模糊匹配）。
	Username string
	// AgentID 目标资产筛选（模糊匹配）。
	AgentID string
	// ClientIP 来源 IP 筛选（模糊匹配）。
	ClientIP string
	// Status 状态筛选：active / closed / failed；空表示全部。
	Status string
	// StartFrom 开始时间下界（含）。
	StartFrom *time.Time
	// StartTo 开始时间上界（含）。
	StartTo *time.Time
	// Page 页码，从 1 开始。
	Page int
	// Size 每页条数。
	Size int
}

// ListSessions 分页查询 SSH 会话审计记录（按开始时间倒序）。
//
// 参数 Parameters:
//   - query (SessionQuery): 查询条件。
//
// 返回 Returns:
//   - rows ([]SSHSession): 当前页数据。
//   - total (int64): 符合条件的总数。
//   - err (error): 查询失败时返回非 nil。
func (s *Service) ListSessions(query SessionQuery) ([]SSHSession, int64, error) {
	statement := s.db.Model(&SSHSession{})
	// 文本条件一律用 ILIKE 模糊匹配：审计场景下记得住的多半只是主机名或账号片段。
	if query.Operator != "" {
		statement = statement.Where("operator ILIKE ?", "%"+query.Operator+"%")
	}
	if query.Username != "" {
		statement = statement.Where("username ILIKE ?", "%"+query.Username+"%")
	}
	if query.AgentID != "" {
		statement = statement.Where("agent_id ILIKE ?", "%"+query.AgentID+"%")
	}
	if query.ClientIP != "" {
		statement = statement.Where("client_ip ILIKE ?", "%"+query.ClientIP+"%")
	}
	if query.Status != "" {
		statement = statement.Where("status = ?", query.Status)
	}
	if query.StartFrom != nil {
		statement = statement.Where("started_at >= ?", *query.StartFrom)
	}
	if query.StartTo != nil {
		statement = statement.Where("started_at <= ?", *query.StartTo)
	}

	var total int64
	if err := statement.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, size := query.Page, query.Size
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	var rows []SSHSession
	if err := statement.Order("started_at DESC").
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

// DeleteSessions 批量删除 SSH 会话审计记录。
//
// 会话记录、命令流、录像文件三者是一体的：只删记录会留下孤儿命令流，录像文件还会一直占磁盘，
// 都不算「清理完成」，因此在这里一次处理干净。录像文件按「不存在即忽略」，
// 单个文件删除失败只告警、不影响记录删除的结果。
//
// 参数 Parameters:
//   - ids ([]uint): 会话主键列表。
//
// 返回 Returns:
//   - removed (int): 实际删除的会话条数。
//   - err (error): 删除失败时返回非 nil。
func (s *Service) DeleteSessions(ids []uint) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	sessions := make([]SSHSession, 0, len(ids))
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id IN ?", ids).Find(&sessions).Error; err != nil {
			return err
		}
		if len(sessions) == 0 {
			return nil
		}
		sessionIDs := make([]uint, 0, len(sessions))
		for _, session := range sessions {
			sessionIDs = append(sessionIDs, session.ID)
		}
		if err := tx.Where("session_id IN ?", sessionIDs).Delete(&SSHCommand{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", sessionIDs).Delete(&SSHSession{}).Error
	})
	if err != nil {
		return 0, err
	}
	for _, session := range sessions {
		s.RemoveSessionRecording(session.RecordingPath)
	}
	return len(sessions), nil
}

// DeleteRemoteSessions 批量删除远程桌面（RDP / VNC）会话记录，并清理对应录像文件。
//
// 与 SSH 会话清理同一取舍：记录与录像一起清，避免「列表没了、磁盘还占着」。
//
// 参数 Parameters:
//   - ids ([]uint): 会话主键列表。
//
// 返回 Returns:
//   - removed (int): 实际删除的会话条数。
//   - err (error): 删除失败时返回非 nil。
func (s *Service) DeleteRemoteSessions(ids []uint) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	sessions := make([]RDPSession, 0, len(ids))
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id IN ?", ids).Find(&sessions).Error; err != nil {
			return err
		}
		if len(sessions) == 0 {
			return nil
		}
		sessionIDs := make([]uint, 0, len(sessions))
		for _, session := range sessions {
			sessionIDs = append(sessionIDs, session.ID)
		}
		return tx.Where("id IN ?", sessionIDs).Delete(&RDPSession{}).Error
	})
	if err != nil {
		return 0, err
	}
	for _, session := range sessions {
		s.RemoveGuacRecording(session.RecordingPath)
	}
	return len(sessions), nil
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
//
// 三者都放行：vnc 与 rdp 同属远程桌面（都由 guacd 承载），区别只在连接参数
// （见 GuacdClient.BuildConnectPlan）。早先版本漏了 vnc 分支，导致即使连接配置
// 支持 VNC，提交上来的 vnc 也会被归一化成空串、再报「协议仅支持 ssh 或 rdp」，
// 前端因此永远配不出 VNC。
func normalizeProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case ProtocolSSH:
		return ProtocolSSH
	case ProtocolRDP:
		return ProtocolRDP
	case ProtocolVNC:
		return ProtocolVNC
	default:
		return ""
	}
}

// ParseProtocol 解析并校验协议参数（供 HTTP 层使用）。
//
// 参数 Parameters:
//   - protocol (string): 待解析的协议文本。
//
// 返回 Returns:
//   - protocol (string): 归一化后的协议（ssh / rdp / vnc）。
//   - err (error): 取值非法时返回非 nil。
func ParseProtocol(protocol string) (string, error) {
	normalized := normalizeProtocol(protocol)
	if normalized == "" {
		return "", errors.New("协议仅支持 ssh、rdp 或 vnc")
	}
	return normalized, nil
}

// sessionStatuses 是会话状态的合法取值（与 model.go 的状态常量一致）。
var sessionStatuses = map[string]bool{
	SessionActive: true,
	SessionClosed: true,
	SessionFailed: true,
}

// ParseSessionStatus 解析并校验会话状态筛选参数（供 HTTP 层使用）。
//
// 空串表示「不过滤」，直接放行；非法取值返回错误，避免任意字符串进入查询条件。
//
// 参数 Parameters:
//   - status (string): 待解析的状态文本。
//
// 返回 Returns:
//   - status (string): 归一化后的状态；输入为空时返回空串。
//   - err (error): 取值非法时返回非 nil。
func ParseSessionStatus(status string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(status))
	if normalized == "" {
		return "", nil
	}
	if !sessionStatuses[normalized] {
		return "", errors.New("会话状态仅支持 active、closed 或 failed")
	}
	return normalized, nil
}

// normalizeRemoteProtocol 归一化远程桌面协议（只接受 rdp / vnc）。
//
// 与 normalizeProtocol 的区别：这里排除 ssh——远程桌面票据只对 guacd 承载的协议签发。
//
// 参数 Parameters:
//   - protocol (string): 待归一化的协议。
//
// 返回 Returns:
//   - protocol (string): rdp / vnc；其它输入返回空串。
func normalizeRemoteProtocol(protocol string) string {
	normalized := normalizeProtocol(protocol)
	if isRemoteDesktopProtocol(normalized) {
		return normalized
	}
	return ""
}

// defaultEndpointPort 返回各协议的默认端口。
//
// VNC 用 5900：早先「非 SSH 一律 3389」的写法会让不填端口的 VNC 配置指向 RDP 端口，
// 连接必然失败且提示不出原因。
//
// 参数 Parameters:
//   - protocol (string): 已归一化的协议。
//
// 返回 Returns:
//   - port (int): 默认端口。
func defaultEndpointPort(protocol string) int {
	switch protocol {
	case ProtocolSSH:
		return 22
	case ProtocolVNC:
		return 5900
	default:
		return 3389
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
