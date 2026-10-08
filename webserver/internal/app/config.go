// Package app 是 webserver 的装配根：读取配置、打开权限库与监控库、执行迁移与种子、
// 装配 service 与控制器、提供 HTTP 处理器与后台推送协程。
package app

import (
	"errors"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/company/monitor-webserver/pkg/logger"
	pkgmiddleware "github.com/company/monitor-webserver/pkg/middleware"
)

// Config 是 webserver.yaml 的映射结构。
type Config struct {
	// HTTPAddress 监听地址，为空时使用 :8090。
	HTTPAddress string `yaml:"http_address"`
	// AccessLog 是否打印每次请求的完整地址与耗时。
	AccessLog bool `yaml:"access_log"`
	// SQLitePath 权限库文件路径。
	SQLitePath string `yaml:"sqlite_path"`
	// Postgres 监控库（PostgreSQL）连接配置。
	Postgres struct {
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
	} `yaml:"postgres"`
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
	// Guacamole Apache Guacamole 嵌入配置（RDP 远程桌面）。
	Guacamole struct {
		// Enabled 是否启用；未启用时前端隐藏 RDP 入口。
		Enabled bool `yaml:"enabled"`
		// BaseURL guacamole-client 地址，形如 http://127.0.0.1:8080/guacamole。
		BaseURL string `yaml:"base_url"`
		// Username 管理 API 账号。
		Username string `yaml:"username"`
		// Password 管理 API 密码。
		Password string `yaml:"password"`
		// DataSource 数据源名，默认 mysql（官方 docker 镜像）。
		DataSource string `yaml:"data_source"`
	} `yaml:"guacamole"`
	// BootstrapAdminPassword 非空时每次启动都会把 admin 密码重置为该值。
	BootstrapAdminPassword string `yaml:"bootstrap_admin_password"`
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
	if cfg.SQLitePath == "" {
		cfg.SQLitePath = "data/webserver.db"
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
	if cfg.Remote.RecordingDir == "" {
		cfg.Remote.RecordingDir = "logs/ssh-recordings"
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
