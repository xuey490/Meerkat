// Package app 是 webserver 的装配根：读取配置、打开权限库与监控库、执行迁移与种子、
// 装配 service 与控制器、提供 HTTP 处理器与后台推送协程。
package app

import (
	"errors"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/company/monitor-webserver/internal/resourcealert"
	"github.com/company/monitor-webserver/pkg/logger"
	pkgmiddleware "github.com/company/monitor-webserver/pkg/middleware"
)

// PostgresConfig 是 PostgreSQL 连接配置。
//
// 权限库与监控库共用同一份连接参数：两者在同一个 database 内靠表名前缀区分
// （sa_system_* 为权限与治理数据，agents/alerts/... 为监控数据）。
type PostgresConfig struct {
	// Host 主机。
	Host string `yaml:"host"`
	// Port 端口。
	Port int `yaml:"port"`
	// User 用户名。
	User string `yaml:"user"`
	// Password 密码。
	Password string `yaml:"password"`
	// Database 库名。
	Database string `yaml:"database"`
	// SSLMode SSL 模式。
	SSLMode string `yaml:"ssl_mode"`
	// TimeZone 会话时区；默认 Asia/Shanghai。
	//
	// 种子 SQL 用 localtimestamp 写入本地时间，GORM 写 *time.Time 也要用同一时间基准，
	// 不统一的话同一列会混进两种时区的时间。
	TimeZone string `yaml:"timezone"`
	// MaxOpenConns 权限库最大连接数；默认 20。
	//
	// 这是本次迁移的核心收益：原 SQLite 实现把连接数强制限制为 1，所有请求串行排队，
	// 并发一高就报 database is locked。
	MaxOpenConns int `yaml:"max_open_conns"`
	// MaxIdleConns 权限库空闲连接数；默认 5。
	MaxIdleConns int `yaml:"max_idle_conns"`
	// ConnMaxLifetimeSeconds 连接最长存活秒数；默认 1800。
	ConnMaxLifetimeSeconds int `yaml:"conn_max_lifetime_seconds"`
}

// Config 是 webserver.yaml 的映射结构。
type Config struct {
	// HTTPAddress 监听地址，为空时使用 :8090。
	HTTPAddress string `yaml:"http_address"`
	// AccessLog 是否打印每次请求的完整地址与耗时。
	AccessLog bool `yaml:"access_log"`
	// Protection HTTP 限流、黑名单与登录锁定配置。
	Protection struct {
		Enabled            bool     `yaml:"enabled"`
		APIRequests        int      `yaml:"api_requests"`
		APIWindowSeconds   int      `yaml:"api_window_seconds"`
		LoginRequests      int      `yaml:"login_requests"`
		LoginWindowSeconds int      `yaml:"login_window_seconds"`
		Blacklist          []string `yaml:"blacklist"`
		Whitelist          []string `yaml:"whitelist"`
		TrustProxyHeaders  bool     `yaml:"trust_proxy_headers"`
		LoginFailureLimit  int      `yaml:"login_failure_limit"`
		LockoutMinutes     int      `yaml:"lockout_minutes"`
		// LockoutIP 达到失败阈值时是否同时锁定来源 IP；用指针区分「未配置」与「显式关闭」，
		// 未配置时默认启用（见 Config.ProtectionLockoutIP）。
		LockoutIP *bool `yaml:"lockout_ip"`
	} `yaml:"protection"`
	// BackupDir 数据库备份目录（计划任务的备份产物落在这里）。
	//
	// 旧实现是从 sqlite_path 推导同级 backup 目录；权限库迁到 PostgreSQL 后
	// 不再有数据库文件路径，因此改为独立配置项。
	BackupDir string `yaml:"backup_dir"`
	// Postgres PostgreSQL 连接配置。
	//
	// 权限库（sa_system_*）与监控库（agents/alerts/...）共用这一套连接参数，
	// 在同一 database 内靠表名前缀区分。
	Postgres PostgresConfig `yaml:"postgres"`
	// Influx 时序库配置。
	Influx struct {
		// URL 地址。
		URL string `yaml:"url"`
		// Token 访问令牌。
		Token string `yaml:"token"`
		// Organization 组织。
		Organization string `yaml:"organization"`
		// Bucket 桶。
		Bucket string `yaml:"bucket"`
	} `yaml:"influx"`
	// JWT 令牌配置。
	JWT struct {
		// Secret 签名密钥，必填。
		Secret string `yaml:"secret"`
		// AccessSeconds 访问令牌有效期（秒）。
		AccessSeconds int `yaml:"access_seconds"`
		// RefreshDays 刷新令牌有效期（天）。
		RefreshDays int `yaml:"refresh_days"`
	} `yaml:"jwt"`
	// Captcha 控制登录验证码；enabled 用指针以区分「未配置」与「显式关闭」。
	Captcha struct {
		// Enabled 是否启用。
		Enabled *bool `yaml:"enabled"`
		// Length 字符数。
		Length int `yaml:"length"`
		// TTLSeconds 有效期（秒）。
		TTLSeconds int `yaml:"ttl_seconds"`
		// MaxItems 进程内验证码上限。
		MaxItems int `yaml:"max_items"`
		// Width 图片宽度。
		Width int `yaml:"width"`
		// Height 图片高度。
		Height int `yaml:"height"`
	} `yaml:"captcha"`
	// Upload 附件上传配置。
	Upload struct {
		// Dir 本地存储目录（相对进程工作目录，默认 upload）。
		Dir string `yaml:"dir"`
		// URLPrefix 静态访问前缀（默认 /upload）。
		URLPrefix string `yaml:"url_prefix"`
		// MaxSizeMB 单文件大小上限（MB）；0 取默认 50，负数表示不限制。
		MaxSizeMB int `yaml:"max_size_mb"`
	} `yaml:"upload"`
	// Redis 缓存配置；缓存管理模块据此展示与清理 Redis 缓存。
	Redis struct {
		// Enabled 是否启用（false 时缓存管理只展示进程内缓存）。
		Enabled bool `yaml:"enabled"`
		// Addr 地址，默认 127.0.0.1:6379。
		Addr string `yaml:"addr"`
		// Username ACL 用户名。
		Username string `yaml:"username"`
		// Password 密码。
		Password string `yaml:"password"`
		// DB 默认库序号。
		DB int `yaml:"db"`
		// Databases 需要在缓存管理页展示的库；为空时只展示 DB。
		Databases []int `yaml:"databases"`
		// ScanLimit 单个库最多扫描的键数，超出部分不展示（避免大库拖垮页面）。
		ScanLimit int `yaml:"scan_limit"`
		// TimeoutSeconds 单次 Redis 操作超时（秒）。
		TimeoutSeconds int `yaml:"timeout_seconds"`
		// Groups 自定义分组（name 展示名，pattern 键匹配模式）。
		Groups []struct {
			// Name 展示名。
			Name string `yaml:"name"`
			// Pattern 键匹配模式，如 token:*。
			Pattern string `yaml:"pattern"`
		} `yaml:"groups"`
	} `yaml:"redis"`
	// Cache 业务缓存（字典、系统配置等热点数据）。
	Cache struct {
		// Enabled 是否启用业务缓存；关闭后全部读操作直连数据库。
		Enabled *bool `yaml:"enabled"`
		// Prefix 缓存键前缀，便于在缓存管理页按分组浏览与批量失效。
		Prefix string `yaml:"prefix"`
		// TTLSeconds 默认过期时间（秒）。
		TTLSeconds int `yaml:"ttl_seconds"`
	} `yaml:"cache"`
	// IPLocation IP 归属地查询（登录地点、在线用户、操作日志）。
	IPLocation struct {
		// Enabled 是否启用归属地查询；关闭后统一展示 disabled_label。
		Enabled *bool `yaml:"enabled"`
		// Provider 数据源：baidu（默认，国内免 key 毫秒级）/ ipapi / pconline / custom。
		Provider string `yaml:"provider"`
		// Endpoint 自定义数据源地址模板，含 {ip} 占位符（provider=custom 时生效）。
		Endpoint string `yaml:"endpoint"`
		// TimeoutSeconds 单次查询超时（秒）。
		TimeoutSeconds int `yaml:"timeout_seconds"`
		// SyncWaitMs 登录时最多等待归属地结果的时间（毫秒），超时不阻塞登录、转为异步补写。
		SyncWaitMs int `yaml:"sync_wait_ms"`
		// CacheTTLHours 查询结果缓存时长（小时）。
		CacheTTLHours int `yaml:"cache_ttl_hours"`
		// PrivateLabel 内网地址标签。
		PrivateLabel string `yaml:"private_label"`
		// DisabledLabel 关闭功能时的标签。
		DisabledLabel string `yaml:"disabled_label"`
		// UnknownLabel 查询失败时的标签。
		UnknownLabel string `yaml:"unknown_label"`
	} `yaml:"ip_location"`
	// DemoEnabled 演示模式：开启后除白名单外的写操作（POST/PUT/PATCH/DELETE）一律拒绝。
	DemoEnabled bool `yaml:"demo_enabled"`
	// DemoWhitelist 演示模式下额外放行的路径；默认已含登录、验证码、退出、刷新令牌。
	DemoWhitelist []string `yaml:"demo_whitelist"`
	// Middleware 安全中间件配置；CORS 直接复用 pkg/middleware 的原版组件。
	Middleware struct {
		// CORS 跨域策略；留空时使用 pkg/middleware 的默认值。
		CORS pkgmiddleware.CORSConfig `yaml:"cors"`
		// XSS 输入清洗（query / 表单 / JSON 体中的脚本特征）。
		XSS struct {
			// Enabled 是否启用；未配置默认启用。
			Enabled *bool `yaml:"enabled"`
		} `yaml:"xss"`
		// XSRF 跨站请求伪造防护（Origin / Referer 校验）。
		XSRF struct {
			// Enabled 是否启用；未配置默认启用。
			Enabled *bool `yaml:"enabled"`
			// AllowOrigins 明确放行的来源（scheme://host[:port]）；
			// 留空表示只校验同源，其中 "*" 不参与放行。
			AllowOrigins []string `yaml:"allow_origins"`
		} `yaml:"xsrf"`
	} `yaml:"middleware"`
	// Log 日志配置（zap + lumberjack，复用 pkg/logger；留空时取内置默认值）。
	Log logger.Config `yaml:"log"`
	// Remote 远程运维（SSH/RDP）配置。
	Remote struct {
		// SecretKey 凭据加密密钥文本；留空时回退 jwt.secret，可用 WEBSERVER_SECRET_KEY 覆盖。
		SecretKey string `yaml:"secret_key"`
		// RecordingDir 会话录像目录（相对进程工作目录，默认 data/ssh-recordings）。
		RecordingDir string `yaml:"recording_dir"`
	} `yaml:"remote"`
	// Guac Apache Guacamole 网关配置：浏览器内 RDP / VNC 远程桌面。
	//
	// 只需部署 guacd 这一个 C 守护进程，不需要 guacamole-client（Java/Tomcat/MySQL）。
	// 连接参数由本服务在握手期权威注入，密码永不下发浏览器。
	Guac struct {
		// Enabled 是否启用远程桌面；未启用时前端隐藏入口。
		Enabled bool `yaml:"enabled"`
		// Address guacd 地址（host:port），默认 127.0.0.1:4822。
		Address string `yaml:"address"`
		// RecordingEnabled 是否录制会话（输出 .guac 指令流，可在审计页回放）。
		RecordingEnabled bool `yaml:"recording_enabled"`
		// RecordingDirInGuacd 传给 guacd 的录像目录。
		//
		// 这是【guacd 所在文件系统】的路径：开发机上 guacd 跑在 WSL2 内，
		// 该值是 /mnt/c/... 形态；生产上按容器/宿主机的实际路径填写。
		RecordingDirInGuacd string `yaml:"recording_dir_in_guacd"`
		// LocalRecordingDir webserver 读取录像的基准目录。
		//
		// 这是【本进程所在文件系统】的路径，与 RecordingDirInGuacd 指向同一份文件，
		// 只是两个不同视角。拆成两个字段，是为了让同一份代码在 Windows 与 Linux 上都不必修改。
		LocalRecordingDir string `yaml:"local_recording_dir"`
		// ConnectTimeoutSeconds 拨号 guacd 的超时（秒），默认 5。
		ConnectTimeoutSeconds int `yaml:"connect_timeout_seconds"`
		// HandshakeTimeoutSeconds 握手超时（秒），默认 15。
		HandshakeTimeoutSeconds int `yaml:"handshake_timeout_seconds"`
		// IdleTimeoutSeconds 会话空闲超时（秒），默认 600；<=0 表示不限制。
		IdleTimeoutSeconds int `yaml:"idle_timeout_seconds"`
		// RetentionDays 录像保留天数，默认 30；<=0 表示不清理。
		RetentionDays int `yaml:"retention_days"`
	} `yaml:"guac"`
	// Alert 资源告警配置（CPU / 内存 / 系统负载 / 磁盘持续超阈值 → 写日志 + 告警中心）。
	Alert AlertConfig `yaml:"alert"`
	// BootstrapAdminPassword 非空时每次启动都会把 admin 密码重置为该值。
	BootstrapAdminPassword string `yaml:"bootstrap_admin_password"`
}

// AlertConfig 是资源告警（CPU / 内存 / 系统负载 / 磁盘持续超阈值）的配置。
//
// 阈值与判定期以数据库中的告警规则为准（可在「告警规则」页与服务器列表按需调整），
// 本段只控制评估开关、查询行为与日志落盘。
type AlertConfig struct {
	// Enabled 是否启用资源告警评估。
	Enabled bool `yaml:"enabled"`
	// EvaluateIntervalSeconds 评估周期（秒）；默认 60。
	EvaluateIntervalSeconds int `yaml:"evaluate_interval_seconds"`
	// Query 时序窗口查询参数。
	Query struct {
		// MergeHosts 是否把同一规则下的多台主机合并为一次 Flux 查询；未配置时启用。
		MergeHosts *bool `yaml:"merge_hosts"`
		// MaxPoints 单次查询返回点数上限保护；默认 2000。
		MaxPoints int `yaml:"max_points"`
	} `yaml:"query"`
	// Log 告警日志落盘配置（独立文件，避免与业务日志混在一起）。
	Log AlertLogConfig `yaml:"log"`
	// Attribution 告警归因配置。
	Attribution struct {
		// TopN 归因进程条数；默认 5。
		TopN int `yaml:"top_n"`
		// PreferProcstat 是否优先使用进程级采集（procstat）数据；未配置时启用。
		// 关闭或数据缺失时退化为心跳里的进程内存快照（有内存维度、缺 CPU 占比）。
		PreferProcstat *bool `yaml:"prefer_procstat"`
	} `yaml:"attribution"`
}

// AlertLogConfig 是资源告警日志的落盘配置。
type AlertLogConfig struct {
	// Enabled 是否写独立日志文件；关闭时告警只进入业务日志。
	Enabled bool `yaml:"enabled"`
	// Format 输出形态：json（结构化 JSON Lines）/ text（人读文本）/ both；默认 both。
	Format string `yaml:"format"`
	// Dir 告警日志目录；默认 logs。
	Dir string `yaml:"dir"`
	// NamePattern 文件命名模板（不含扩展名），支持 {agent}（主机标识）与 {date}（YYYYMMDD）。
	//
	// 默认 {agent}-{date}-alerts，展开后同时产出 .log（结构化）与 .txt（人读），例如
	// logs/loca-windows-20261009-alerts.log。主机标识会做文件名安全化（非法字符换成下划线）。
	NamePattern string `yaml:"name_pattern"`
	// MaxSizeMB 单文件大小上限（MB）；默认 32。
	MaxSizeMB int `yaml:"max_size_mb"`
	// MaxBackups 保留的历史文件数；默认 14。
	MaxBackups int `yaml:"max_backups"`
	// MaxAgeDays 历史文件保留天数；默认 30。
	MaxAgeDays int `yaml:"max_age_days"`
	// Compress 是否压缩历史文件；未配置时压缩。
	Compress *bool `yaml:"compress"`
}

// AlertEvaluateInterval 返回资源告警的评估周期。
//
// 返回 Returns:
//   - interval (time.Duration): 评估周期，未配置时为 60 秒。
func (c Config) AlertEvaluateInterval() time.Duration {
	if c.Alert.EvaluateIntervalSeconds <= 0 {
		return time.Minute
	}
	return time.Duration(c.Alert.EvaluateIntervalSeconds) * time.Second
}

// AlertMergeHosts 返回是否把同一规则下的多台主机合并为一次查询。
//
// 返回 Returns:
//   - merge (bool): 未配置时返回 true。
func (c Config) AlertMergeHosts() bool {
	if c.Alert.Query.MergeHosts == nil {
		return true
	}
	return *c.Alert.Query.MergeHosts
}

// AlertPreferProcstat 返回是否优先使用进程级采集数据做归因。
//
// 返回 Returns:
//   - prefer (bool): 未配置时返回 true。
func (c Config) AlertPreferProcstat() bool {
	if c.Alert.Attribution.PreferProcstat == nil {
		return true
	}
	return *c.Alert.Attribution.PreferProcstat
}

// ToOptions 把资源告警配置转换为 resourcealert 服务的运行参数。
//
// 返回 Returns:
//   - opts (resourcealert.Options): 服务运行参数。
func (c AlertConfig) ToOptions() resourcealert.Options {
	compress := true
	if c.Log.Compress != nil {
		compress = *c.Log.Compress
	}
	return resourcealert.Options{
		Enabled:          c.Enabled,
		EvaluateInterval: time.Duration(c.EvaluateIntervalSeconds) * time.Second,
		MergeHosts:       c.Query.MergeHosts == nil || *c.Query.MergeHosts,
		MaxPoints:        c.Query.MaxPoints,
		TopN:             c.Attribution.TopN,
		PreferProcstat:   c.Attribution.PreferProcstat == nil || *c.Attribution.PreferProcstat,
		Log: resourcealert.LogOptions{
			Enabled:     c.Log.Enabled,
			Format:      c.Log.Format,
			Dir:         c.Log.Dir,
			NamePattern: c.Log.NamePattern,
			MaxSizeMB:   c.Log.MaxSizeMB,
			MaxBackups:  c.Log.MaxBackups,
			MaxAgeDays:  c.Log.MaxAgeDays,
			Compress:    compress,
		},
	}
}

// LoadConfig 读取并规范化 webserver 配置。
//
// 参数 Parameters:
//   - path (string): YAML 配置文件路径。
//
// 返回 Returns:
//   - cfg (Config): 补齐默认值后的配置。
//   - err (error): 文件读取、解析失败或缺少 jwt.secret 时返回非 nil。
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if value := os.Getenv("WEBSERVER_ADMIN_PASSWORD"); value != "" {
		cfg.BootstrapAdminPassword = value
	}
	if value := os.Getenv("WEBSERVER_JWT_SECRET"); value != "" {
		cfg.JWT.Secret = value
	}
	if value := os.Getenv("MONITOR_POSTGRES_PASSWORD"); value != "" {
		cfg.Postgres.Password = value
	}
	if value := os.Getenv("MONITOR_INFLUX_TOKEN"); value != "" {
		cfg.Influx.Token = value
	}
	if value := strings.TrimSpace(os.Getenv("WEBSERVER_DEMO_ENABLED")); value != "" {
		cfg.DemoEnabled = strings.EqualFold(value, "true") || value == "1"
	}
	if value := strings.TrimSpace(os.Getenv("WEBSERVER_SECRET_KEY")); value != "" {
		cfg.Remote.SecretKey = value
	}
	if cfg.HTTPAddress == "" {
		cfg.HTTPAddress = ":8090"
	}
	if cfg.Protection.APIRequests <= 0 {
		cfg.Protection.APIRequests = 120
	}
	if cfg.Protection.APIWindowSeconds <= 0 {
		cfg.Protection.APIWindowSeconds = 60
	}
	if cfg.Protection.LoginRequests <= 0 {
		cfg.Protection.LoginRequests = 10
	}
	if cfg.Protection.LoginWindowSeconds <= 0 {
		cfg.Protection.LoginWindowSeconds = 60
	}
	if cfg.Protection.LoginFailureLimit <= 0 {
		cfg.Protection.LoginFailureLimit = 5
	}
	if cfg.Protection.LockoutMinutes <= 0 {
		cfg.Protection.LockoutMinutes = 30
	}
	if cfg.BackupDir == "" {
		cfg.BackupDir = "data/backup"
	}
	if cfg.Postgres.TimeZone == "" {
		cfg.Postgres.TimeZone = "Asia/Shanghai"
	}
	if cfg.Postgres.MaxOpenConns <= 0 {
		cfg.Postgres.MaxOpenConns = 20
	}
	if cfg.Postgres.MaxIdleConns <= 0 {
		cfg.Postgres.MaxIdleConns = 5
	}
	if cfg.Postgres.ConnMaxLifetimeSeconds <= 0 {
		cfg.Postgres.ConnMaxLifetimeSeconds = 1800
	}
	if cfg.JWT.AccessSeconds == 0 {
		cfg.JWT.AccessSeconds = 7200
	}
	if cfg.JWT.RefreshDays == 0 {
		cfg.JWT.RefreshDays = 30
	}
	if cfg.Captcha.Enabled == nil {
		enabled := true
		cfg.Captcha.Enabled = &enabled
	}
	if cfg.Captcha.Length == 0 {
		cfg.Captcha.Length = 4
	}
	if cfg.Captcha.TTLSeconds == 0 {
		cfg.Captcha.TTLSeconds = 180
	}
	if cfg.Captcha.MaxItems == 0 {
		cfg.Captcha.MaxItems = 1000
	}
	if cfg.Captcha.Width == 0 {
		cfg.Captcha.Width = 120
	}
	if cfg.Captcha.Height == 0 {
		cfg.Captcha.Height = 44
	}
	if cfg.Upload.Dir == "" {
		cfg.Upload.Dir = "upload"
	}
	if cfg.Upload.URLPrefix == "" {
		cfg.Upload.URLPrefix = "/upload"
	}
	if cfg.Upload.MaxSizeMB == 0 {
		cfg.Upload.MaxSizeMB = 50
	}
	if cfg.Redis.Addr == "" {
		cfg.Redis.Addr = "127.0.0.1:6379"
	}
	if len(cfg.Redis.Databases) == 0 {
		cfg.Redis.Databases = []int{cfg.Redis.DB}
	}
	if cfg.Redis.ScanLimit == 0 {
		cfg.Redis.ScanLimit = 5000
	}
	if cfg.Redis.TimeoutSeconds == 0 {
		cfg.Redis.TimeoutSeconds = 5
	}
	if cfg.Cache.Enabled == nil {
		enabled := true
		cfg.Cache.Enabled = &enabled
	}
	if cfg.Cache.Prefix == "" {
		cfg.Cache.Prefix = "monitor:"
	}
	if cfg.Cache.TTLSeconds == 0 {
		cfg.Cache.TTLSeconds = 1800
	}
	if cfg.IPLocation.Enabled == nil {
		enabled := true
		cfg.IPLocation.Enabled = &enabled
	}
	if cfg.IPLocation.Provider == "" {
		cfg.IPLocation.Provider = "baidu"
	}
	if cfg.IPLocation.TimeoutSeconds == 0 {
		cfg.IPLocation.TimeoutSeconds = 6
	}
	if cfg.IPLocation.SyncWaitMs == 0 {
		cfg.IPLocation.SyncWaitMs = 1500
	}
	if cfg.IPLocation.CacheTTLHours == 0 {
		cfg.IPLocation.CacheTTLHours = 168
	}
	if cfg.IPLocation.PrivateLabel == "" {
		cfg.IPLocation.PrivateLabel = "内网地址"
	}
	if cfg.IPLocation.DisabledLabel == "" {
		cfg.IPLocation.DisabledLabel = "未解析(已关闭归属地查询)"
	}
	if cfg.IPLocation.UnknownLabel == "" {
		cfg.IPLocation.UnknownLabel = "未解析"
	}
	if cfg.Middleware.XSS.Enabled == nil {
		enabled := true
		cfg.Middleware.XSS.Enabled = &enabled
	}
	if cfg.Middleware.XSRF.Enabled == nil {
		enabled := true
		cfg.Middleware.XSRF.Enabled = &enabled
	}
	// 整段 log 未配置时套用内置默认（日志写 logs/app.log，按大小轮转）。
	if cfg.Log == (logger.Config{}) {
		cfg.Log = logger.DefaultConfig("webserver")
	}
	// 资源告警运行参数与日志路径的默认值；阈值与判定期以数据库规则为准。
	if cfg.Alert.EvaluateIntervalSeconds <= 0 {
		cfg.Alert.EvaluateIntervalSeconds = 60
	}
	if cfg.Alert.Query.MaxPoints <= 0 {
		cfg.Alert.Query.MaxPoints = 2000
	}
	if cfg.Alert.Attribution.TopN <= 0 {
		cfg.Alert.Attribution.TopN = 5
	}
	if strings.TrimSpace(cfg.Alert.Log.Format) == "" {
		cfg.Alert.Log.Format = "both"
	}
	if strings.TrimSpace(cfg.Alert.Log.Dir) == "" {
		cfg.Alert.Log.Dir = "logs"
	}
	if strings.TrimSpace(cfg.Alert.Log.NamePattern) == "" {
		cfg.Alert.Log.NamePattern = resourcealert.DefaultAlertNamePattern
	}
	if cfg.Alert.Log.MaxSizeMB <= 0 {
		cfg.Alert.Log.MaxSizeMB = 32
	}
	if cfg.Alert.Log.MaxBackups <= 0 {
		cfg.Alert.Log.MaxBackups = 14
	}
	if cfg.Alert.Log.MaxAgeDays <= 0 {
		cfg.Alert.Log.MaxAgeDays = 30
	}
	if cfg.Remote.RecordingDir == "" {
		cfg.Remote.RecordingDir = "logs/ssh-recordings"
	}
	// 远程桌面（guacd）：地址与各类超时给默认值。
	if cfg.Guac.Address == "" {
		cfg.Guac.Address = "127.0.0.1:4822"
	}
	if cfg.Guac.ConnectTimeoutSeconds <= 0 {
		cfg.Guac.ConnectTimeoutSeconds = 5
	}
	if cfg.Guac.HandshakeTimeoutSeconds <= 0 {
		cfg.Guac.HandshakeTimeoutSeconds = 15
	}
	if cfg.Guac.IdleTimeoutSeconds <= 0 {
		cfg.Guac.IdleTimeoutSeconds = 600
	}
	if cfg.Guac.RetentionDays <= 0 {
		cfg.Guac.RetentionDays = 30
	}
	// 录像目录的两个视角（详见 Guac 结构体上的注释）：
	//   guacd 侧默认按「开发机 guacd 跑在 WSL2」填写，生产部署时改成容器/宿主的实际路径；
	//   本地侧是 webserver 进程读盘用的路径，与前者指向同一份文件。
	if cfg.Guac.RecordingDirInGuacd == "" {
		cfg.Guac.RecordingDirInGuacd = "/mnt/c/monitor/guac-recordings/sessions"
	}
	if cfg.Guac.LocalRecordingDir == "" {
		cfg.Guac.LocalRecordingDir = `C:\monitor\guac-recordings\sessions`
	}
	// 凭据加密密钥：优先 remote.secret_key / 环境变量，否则回退 jwt.secret。
	if cfg.Remote.SecretKey == "" {
		cfg.Remote.SecretKey = cfg.JWT.Secret
	}
	if cfg.JWT.Secret == "" {
		return Config{}, errors.New("jwt.secret or WEBSERVER_JWT_SECRET is required")
	}
	return cfg, nil
}

// XSSEnabled 判断是否启用 XSS 清洗（未配置时默认启用）。
//
// 返回 Returns:
//   - enabled (bool): 是否启用。
func (c Config) XSSEnabled() bool {
	if c.Middleware.XSS.Enabled == nil {
		return true
	}
	return *c.Middleware.XSS.Enabled
}

// XSRFEnabled 判断是否启用 XSRF 防护（未配置时默认启用）。
//
// 返回 Returns:
//   - enabled (bool): 是否启用。
func (c Config) XSRFEnabled() bool {
	if c.Middleware.XSRF.Enabled == nil {
		return true
	}
	return *c.Middleware.XSRF.Enabled
}

// XSRFAllowOrigins 返回 XSRF 放行来源。
//
// 语义区分（两条路径不会互相污染）：
//   - xsrf.allow_origins 显式写了值：原样返回，其中 "*" 表示「不校验」；
//   - xsrf.allow_origins 留空：回落 CORS 的 allow_origins，但剔除 "*"——
//     CORS 允许 * 只代表「跨源读没问题」，不代表写操作可以不做同源判定。
//
// 返回 Returns:
//   - origins ([]string): 放行来源列表（可能含 "*"）。
func (c Config) XSRFAllowOrigins() []string {
	if len(c.Middleware.XSRF.AllowOrigins) > 0 {
		origins := make([]string, 0, len(c.Middleware.XSRF.AllowOrigins))
		for _, origin := range c.Middleware.XSRF.AllowOrigins {
			if trimmed := strings.TrimSpace(origin); trimmed != "" {
				origins = append(origins, trimmed)
			}
		}
		return origins
	}
	origins := make([]string, 0, len(c.Middleware.CORS.AllowOrigins))
	for _, origin := range c.Middleware.CORS.AllowOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" || trimmed == "*" {
			continue
		}
		origins = append(origins, trimmed)
	}
	return origins
}

// ProtectionLockoutIP 判断登录失败是否同时锁定来源 IP（未配置时默认启用）。
//
// 返回 Returns:
//   - enabled (bool): 是否启用 IP 锁定。
func (c Config) ProtectionLockoutIP() bool {
	if c.Protection.LockoutIP == nil {
		return true
	}
	return *c.Protection.LockoutIP
}

// CacheEnabled 判断是否启用业务缓存（未配置时默认启用）。
//
// 返回 Returns:
//   - enabled (bool): 是否启用。
func (c Config) CacheEnabled() bool {
	if c.Cache.Enabled == nil {
		return true
	}
	return *c.Cache.Enabled
}

// IPLocationEnabled 判断是否启用 IP 归属地查询（未配置时默认启用）。
//
// 返回 Returns:
//   - enabled (bool): 是否启用。
func (c Config) IPLocationEnabled() bool {
	if c.IPLocation.Enabled == nil {
		return true
	}
	return *c.IPLocation.Enabled
}

// CaptchaEnabled 判断是否启用登录验证码（未配置时默认启用）。
//
// 返回 Returns:
//   - enabled (bool): 是否启用。
func (c Config) CaptchaEnabled() bool {
	if c.Captcha.Enabled == nil {
		return true
	}
	return *c.Captcha.Enabled
}
