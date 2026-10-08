// Package remote 提供监控资产的远程运维能力：SSH/RDP 连接配置（凭据加密存储）、
// SSH Web 终端会话与录像、命令流审计，以及 RDP 经 Guacamole 的接入。
package remote

import "time"

// 协议常量。
const (
	// ProtocolSSH 是 SSH 协议。
	ProtocolSSH = "ssh"
	// ProtocolRDP 是 RDP 协议。
	ProtocolRDP = "rdp"
)

// 认证方式常量。
const (
	// AuthPassword 用户名 + 密码认证。
	AuthPassword = "password"
	// AuthKey 用户名 + 私钥认证。
	AuthKey = "key"
)

// 会话状态常量。
const (
	// SessionActive 会话进行中。
	SessionActive = "active"
	// SessionClosed 会话正常结束。
	SessionClosed = "closed"
	// SessionFailed 会话建立失败。
	SessionFailed = "failed"
)

// RemoteEndpoint 对应 remote_endpoints 表：一台监控资产（agent）的远程连接配置。
//
// 密码 / 私钥 / passphrase 都以 AES-GCM 密文（base64）落库，明文永不下发前端。
type RemoteEndpoint struct {
	// ID 主键（自增）。
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// AgentID 关联的监控资产主键（agents.id），唯一。
	AgentID string `gorm:"column:agent_id;uniqueIndex;size:128" json:"agentId"`
	// Protocol 协议：ssh / rdp。
	Protocol string `gorm:"column:protocol;size:16" json:"protocol"`
	// Host 连接地址（IP 或域名）。
	Host string `gorm:"column:host;size:255" json:"host"`
	// Port 端口。
	Port int `gorm:"column:port" json:"port"`
	// Username 登录用户名。
	Username string `gorm:"column:username;size:128" json:"username"`
	// AuthType 认证方式：password / key。
	AuthType string `gorm:"column:auth_type;size:16" json:"authType"`
	// SecretCipher 密码或私钥的 AES-GCM 密文（base64）。
	SecretCipher string `gorm:"column:secret_cipher;type:text" json:"-"`
	// PassphraseCipher 私钥 passphrase 的 AES-GCM 密文（可选）。
	PassphraseCipher string `gorm:"column:passphrase_cipher;type:text" json:"-"`
	// Enabled 是否启用。
	Enabled bool `gorm:"column:enabled" json:"enabled"`
	// Remark 备注。
	Remark string `gorm:"column:remark;size:255" json:"remark"`
	// CreatedBy 创建人。
	CreatedBy int64 `gorm:"column:created_by" json:"createdBy"`
	// UpdatedBy 更新人。
	UpdatedBy int64 `gorm:"column:updated_by" json:"updatedBy"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `gorm:"column:created_at" json:"createdAt"`
	// UpdatedAt 更新时间。
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

// TableName 返回 remote_endpoints 表名。
func (RemoteEndpoint) TableName() string { return "remote_endpoints" }

// SSHSession 对应 ssh_sessions 表：一次 SSH 终端会话。
type SSHSession struct {
	// ID 主键（自增）。
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// AgentID 目标资产主键。
	AgentID string `gorm:"column:agent_id;size:128" json:"agentId"`
	// Operator 操作人账号。
	Operator string `gorm:"column:operator;size:128" json:"operator"`
	// Username 登录目标机的用户名。
	Username string `gorm:"column:username;size:128" json:"username"`
	// ClientIP 操作人来源 IP。
	ClientIP string `gorm:"column:client_ip;size:64" json:"clientIp"`
	// Status 状态：active / closed / failed。
	Status string `gorm:"column:status;size:16" json:"status"`
	// CloseReason 断开原因（正常 / 错误信息）。
	CloseReason string `gorm:"column:close_reason;size:255" json:"closeReason"`
	// RecordingPath 录像文件相对路径（asciinema v2 cast）。
	RecordingPath string `gorm:"column:recording_path;size:512" json:"recordingPath"`
	// BytesIn 从客户端收到的字节数。
	BytesIn int64 `gorm:"column:bytes_in" json:"bytesIn"`
	// BytesOut 发送给客户端的字节数。
	BytesOut int64 `gorm:"column:bytes_out" json:"bytesOut"`
	// StartedAt 开始时间。
	StartedAt time.Time `gorm:"column:started_at" json:"startedAt"`
	// EndedAt 结束时间（进行中为空）。
	EndedAt *time.Time `gorm:"column:ended_at" json:"endedAt"`
}

// TableName 返回 ssh_sessions 表名。
func (SSHSession) TableName() string { return "ssh_sessions" }

// SSHCommand 对应 ssh_commands 表：会话中输入的命令流（按回车聚合）。
type SSHCommand struct {
	// ID 主键（自增）。
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// SessionID 所属会话。
	SessionID uint `gorm:"column:session_id;index" json:"sessionId"`
	// Seq 命令序号（会话内递增）。
	Seq int `gorm:"column:seq" json:"seq"`
	// Command 命令文本（去尾部换行）。
	Command string `gorm:"column:command;type:text" json:"command"`
	// RecordedAt 记录时间。
	RecordedAt time.Time `gorm:"column:recorded_at" json:"recordedAt"`
}

// TableName 返回 ssh_commands 表名。
func (SSHCommand) TableName() string { return "ssh_commands" }

// RDPSession 对应 rdp_sessions 表：一次 RDP（Guacamole）连接记录。
type RDPSession struct {
	// ID 主键（自增）。
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// AgentID 目标资产主键。
	AgentID string `gorm:"column:agent_id;size:128" json:"agentId"`
	// Operator 操作人账号。
	Operator string `gorm:"column:operator;size:128" json:"operator"`
	// Username 登录目标机的用户名。
	Username string `gorm:"column:username;size:128" json:"username"`
	// GuacConnectionID Guacamole 连接 ID。
	GuacConnectionID string `gorm:"column:guac_connection_id;size:128" json:"guacConnectionId"`
	// Status 状态：active / closed / failed。
	Status string `gorm:"column:status;size:16" json:"status"`
	// StartedAt 开始时间。
	StartedAt time.Time `gorm:"column:started_at" json:"startedAt"`
	// EndedAt 结束时间（进行中为空）。
	EndedAt *time.Time `gorm:"column:ended_at" json:"endedAt"`
}

// TableName 返回 rdp_sessions 表名。
func (RDPSession) TableName() string { return "rdp_sessions" }

// DangerousCommand 对应 dangerous_commands 表：高危命令拦截规则。
type DangerousCommand struct {
	// ID 主键（自增）。
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// Name 规则名称。
	Name string `gorm:"column:name;size:128;not null" json:"name"`
	// Pattern 匹配内容（命令文本或正则表达式）。
	Pattern string `gorm:"column:pattern;type:text;not null" json:"pattern"`
	// MatchType 匹配类型：exact / regex / prefix。
	MatchType string `gorm:"column:match_type;size:16;not null" json:"matchType"`
	// Enabled 是否启用。
	Enabled bool `gorm:"column:enabled;default:true" json:"enabled"`
	// Description 描述。
	Description string `gorm:"column:description;size:255" json:"description"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `gorm:"column:created_at" json:"createdAt"`
	// UpdatedAt 更新时间。
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

// TableName 返回 dangerous_commands 表名。
func (DangerousCommand) TableName() string { return "dangerous_commands" }

// MatchResult 是单次高危命令匹配结果。
type MatchResult struct {
	Matched bool   `json:"matched"`
	RuleID  uint   `json:"ruleId"`
	Name    string `json:"name"`
}
