package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/company/monitor-webserver/api/middleware"
	apiRouter "github.com/company/monitor-webserver/api/router"
	v1 "github.com/company/monitor-webserver/api/v1"
	"github.com/company/monitor-webserver/internal/cache"
	"github.com/company/monitor-webserver/internal/cachestore"
	"github.com/company/monitor-webserver/internal/captcha"
	"github.com/company/monitor-webserver/internal/eventhub"
	"github.com/company/monitor-webserver/internal/iploc"
	"github.com/company/monitor-webserver/internal/job"
	"github.com/company/monitor-webserver/internal/loginguard"
	"github.com/company/monitor-webserver/internal/logic"
	"github.com/company/monitor-webserver/internal/logx"
	"github.com/company/monitor-webserver/internal/monitor"
	"github.com/company/monitor-webserver/internal/permission"
	"github.com/company/monitor-webserver/internal/remote"
	"github.com/company/monitor-webserver/internal/resourcealert"
	"github.com/company/monitor-webserver/internal/service"
	"github.com/company/monitor-webserver/internal/session"
	"github.com/company/monitor-webserver/pkg/redisx"
)

// App 聚合 webserver 运行期依赖与已装配的 HTTP 处理器。
type App struct {
	// cfg 运行配置。
	cfg Config
	// log zap 日志器（控制台 + logs 目录），由 logx.Setup 装配。
	log *zap.Logger
	// webDB 权限库：PostgreSQL（sa_system_*，与监控库同实例同库）。
	webDB *gorm.DB
	// monitorDB 监控库：PostgreSQL（agents/alerts/...）。
	monitorDB *gorm.DB
	// influx 时序库客户端。
	influx influxdb2.Client
	// router HTTP 路由。
	router *gin.Engine
	// events SSE 事件中心。
	events *eventhub.Hub
	// captcha 登录验证码仓储；nil 表示未初始化。
	captcha *captcha.Store
	// monitor 监控查询服务（后台推送需要）。
	monitor *monitor.Service
	// remote 远程运维服务（SSH/RDP 连接配置与会话审计）。
	remote *remote.Service
	// jobSvc 计划任务服务。
	jobSvc *job.Service
	// caches 缓存管理器（Redis + 进程内缓存，缓存管理模块使用）。
	caches *cache.Manager
	// businessCache 业务缓存（字典、配置、IP 归属地），Redis 不可用时为进程内缓存；可为 nil。
	businessCache *cachestore.Store
	// locations IP 归属地查询器（登录地点、在线用户、操作日志）。
	locations *iploc.Resolver
	// resourceAlert 资源告警服务（CPU/内存/负载/磁盘持续超阈值 → 日志 + 告警中心）。
	resourceAlert *resourcealert.Service
}

// New 构建 App：打开两套数据库、执行权限库迁移与种子、装配服务与路由。
//
// 参数 Parameters:
//   - cfg (Config): 已规范化的配置。
//
// 返回 Returns:
//   - app (*App): 可直接提供 http.Handler 的应用实例。
//   - err (error): 任一依赖初始化失败时返回非 nil。
func New(cfg Config) (*App, error) {
	// 日志最先装配：后续数据库、迁移、业务的 slog 输出都会落到 logs 目录。
	zapLog, err := logx.Setup(cfg.Log)
	if err != nil {
		return nil, err
	}
	// 备份目录随启动创建：计划任务的备份产物落在这里，缺目录会导致备份失败。
	if err := os.MkdirAll(cfg.BackupDir, 0o755); err != nil {
		_ = zapLog.Sync()
		return nil, err
	}
	// 附件目录随启动创建，避免首次上传时因目录缺失失败。
	if err := os.MkdirAll(cfg.Upload.Dir, 0o755); err != nil {
		_ = zapLog.Sync()
		return nil, err
	}
	// 会话录像目录随启动创建；转绝对路径，避免进程工作目录变化导致录像丢失。
	recordingDir, err := filepath.Abs(cfg.Remote.RecordingDir)
	if err != nil {
		_ = zapLog.Sync()
		return nil, err
	}
	if err := os.MkdirAll(recordingDir, 0o755); err != nil {
		_ = zapLog.Sync()
		return nil, err
	}
	// 权限库与监控库同属一个 PostgreSQL 实例（同一 database，靠表名前缀区分），
	// 但仍保留两个独立的 gorm.DB：连接池可各自调优，将来要物理分离只需改配置。
	webDB, err := gorm.Open(postgres.Open(postgresDSN(cfg.Postgres)), &gorm.Config{Logger: gormLogger()})
	if err != nil {
		_ = zapLog.Sync()
		return nil, err
	}
	webSQLDB, err := webDB.DB()
	if err != nil {
		_ = zapLog.Sync()
		return nil, err
	}
	// 并发不再被锁死：原 SQLite 实现把连接数限制为 1，所有请求串行排队，
	// 并发一高就撞 busy_timeout 报 database is locked。PostgreSQL 支持真正的并发连接，
	// 这里按配置放开连接池（默认 20 个连接 / 5 个空闲 / 1800s 存活）。
	webSQLDB.SetMaxOpenConns(cfg.Postgres.MaxOpenConns)
	webSQLDB.SetMaxIdleConns(cfg.Postgres.MaxIdleConns)
	webSQLDB.SetConnMaxLifetime(time.Duration(cfg.Postgres.ConnMaxLifetimeSeconds) * time.Second)

	monitorDB, err := gorm.Open(postgres.Open(postgresDSN(cfg.Postgres)), &gorm.Config{Logger: gormLogger()})
	if err != nil {
		_ = zapLog.Sync()
		return nil, err
	}

	app := &App{
		cfg:       cfg,
		log:       zapLog,
		webDB:     webDB,
		monitorDB: monitorDB,
		influx:    influxdb2.NewClient(cfg.Influx.URL, cfg.Influx.Token),
		events:    eventhub.New(),
		monitor:   monitor.New(monitorDB, influxdb2.NewClient(cfg.Influx.URL, cfg.Influx.Token), cfg.Influx.Organization, cfg.Influx.Bucket),
	}
	// 远程运维服务：SSH 终端与会话审计 + RDP/VNC 远程桌面（经 guacd）。
	// 远程桌面未启用时 guac.Client 为 nil，相关入口对前端隐藏。
	guacOptions := remote.GuacOptions{
		LocalRecordingDir: cfg.Guac.LocalRecordingDir,
		HandshakeTimeout:  time.Duration(cfg.Guac.HandshakeTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(cfg.Guac.IdleTimeoutSeconds) * time.Second,
	}
	if cfg.Guac.Enabled {
		guacOptions.Client = remote.NewGuacdClient(
			cfg.Guac.Address,
			time.Duration(cfg.Guac.ConnectTimeoutSeconds)*time.Second,
			cfg.Guac.RecordingEnabled,
			cfg.Guac.RecordingDirInGuacd,
		)
	}
	app.remote = remote.New(monitorDB, cfg.Remote.SecretKey, recordingDir, guacOptions)
	app.jobSvc = job.New(monitorDB, webDB, cfg.Influx.URL, cfg.Influx.Token, cfg.Influx.Organization, cfg.Influx.Bucket,
		cfg.BackupDir, job.PostgresConfig{
			Host: cfg.Postgres.Host, Port: cfg.Postgres.Port, User: cfg.Postgres.User,
			Password: cfg.Postgres.Password, Database: cfg.Postgres.Database, SSLMode: cfg.Postgres.SSLMode,
		})
	if err := app.migrate(); err != nil {
		app.Close()
		return nil, err
	}
	// 监控库上的远程运维表迁移（幂等；监控库已连接，失败则启动失败）。
	if err := app.remote.Migrate(); err != nil {
		app.Close()
		return nil, err
	}
	// 远程桌面对账：进程被强杀时 defer 不会执行，会把会话永久留在 active，
	// 让审计列表看起来永远有会话在进行中。启动时修正一次。
	if affected, err := app.remote.ReconcileOrphanSessions(); err != nil {
		zapLog.Warn("修正遗留的远程桌面会话失败", zap.Error(err))
	} else if affected > 0 {
		zapLog.Info("已修正遗留的远程桌面会话", zap.Int64("count", affected))
	}
	// 远程桌面录像的保留清理：需要访问 remote 服务，因此在装配阶段以闭包注册。
	// 对应 sa_job 里 invoke_target = cleanGuacRecordings 的记录（可在「计划任务」页配置周期）。
	app.jobSvc.RegisterTask("cleanGuacRecordings", func(_ context.Context, _ *job.Service) (string, error) {
		removed, err := app.remote.CleanupRecordings(cfg.Guac.RetentionDays)
		if err != nil {
			return "", err
		}
		if removed == 0 {
			return "没有需要清理的远程桌面录像", nil
		}
		return fmt.Sprintf("已清理过期远程桌面录像 %d 个", removed), nil
	})
	if err := app.jobSvc.Migrate(); err != nil {
		app.Close()
		return nil, err
	}
	if err := app.jobSvc.Start(); err != nil {
		app.Close()
		return nil, err
	}
	// 资源告警：规则表与告警表建在监控库上（与 collector 同库），评估协程常驻。
	alertStore := resourcealert.NewStore(monitorDB)
	alertService, alertErr := resourcealert.New(alertStore, app.influx,
		cfg.Influx.Organization, cfg.Influx.Bucket, app.events, cfg.Alert.ToOptions(), slog.Default())
	if alertErr != nil {
		app.Close()
		return nil, alertErr
	}
	app.resourceAlert = alertService
	if err := app.resourceAlert.Migrate(); err != nil {
		app.Close()
		return nil, err
	}
	app.resourceAlert.Start()
	// 放在 migrate 之后：此后的 New 无错误返回路径，避免清理协程泄漏。
	app.captcha = captcha.New(cfg.Captcha.Length,
		time.Duration(cfg.Captcha.TTLSeconds)*time.Second, cfg.Captcha.MaxItems)
	app.businessCache = app.newBusinessCache()
	app.locations = app.newIPLocator()
	app.router = app.newRouter()
	return app, nil
}

// newBusinessCache 创建业务缓存：配置了 Redis 就走 Redis，否则退化为进程内 TTL 缓存。
//
// 返回 Returns:
//   - store (*cachestore.Store): 业务缓存；cache.enabled=false 时返回 nil（直连数据库）。
func (a *App) newBusinessCache() *cachestore.Store {
	if !a.cfg.CacheEnabled() {
		return nil
	}
	var client *redis.Client
	if a.cfg.Redis.Enabled {
		client = redisx.NewClient(redisx.Config{
			Addr:     a.cfg.Redis.Addr,
			Username: a.cfg.Redis.Username,
			Password: a.cfg.Redis.Password,
			DB:       a.cfg.Redis.DB,
		})
	}
	return cachestore.New(client, a.cfg.Cache.Prefix,
		time.Duration(a.cfg.Cache.TTLSeconds)*time.Second)
}

// newLoginGuard 创建登录失败锁定守卫：账号与来源 IP 两个维度的失败计数存缓存，
// 计数键 TTL 即锁定时长，到期自动解锁（无需巡检任务）。
//
// 存储优先级：业务缓存（cache.enabled=true）→ 单独的 Redis / 进程内缓存。
// 即 cache.enabled=false 但 redis.enabled=true 时，锁定仍存 Redis（多实例共享）。
//
// 返回 Returns:
//   - guard (*loginguard.Guard): 登录锁定守卫。
func (a *App) newLoginGuard() *loginguard.Guard {
	lockout := time.Duration(a.cfg.Protection.LockoutMinutes) * time.Minute
	store := a.businessCache
	if store == nil {
		var client *redis.Client
		if a.cfg.Redis.Enabled {
			client = redisx.NewClient(redisx.Config{
				Addr:     a.cfg.Redis.Addr,
				Username: a.cfg.Redis.Username,
				Password: a.cfg.Redis.Password,
				DB:       a.cfg.Redis.DB,
			})
		}
		store = cachestore.New(client, a.cfg.Cache.Prefix, lockout)
	}
	return loginguard.New(store, loginguard.Options{
		Limit:   a.cfg.Protection.LoginFailureLimit,
		Lockout: lockout,
		LockIP:  a.cfg.ProtectionLockoutIP(),
	})
}

// newIPLocator 创建 IP 归属地查询器（结果写入业务缓存，便于在缓存管理页查看与清理）。
//
// 返回 Returns:
//   - resolver (*iploc.Resolver): 归属地查询器。
func (a *App) newIPLocator() *iploc.Resolver {
	cfg := a.cfg
	return iploc.New(iploc.Config{
		Enabled:       cfg.IPLocationEnabled(),
		Provider:      cfg.IPLocation.Provider,
		Endpoint:      cfg.IPLocation.Endpoint,
		Timeout:       time.Duration(cfg.IPLocation.TimeoutSeconds) * time.Second,
		SyncWait:      time.Duration(cfg.IPLocation.SyncWaitMs) * time.Millisecond,
		CacheTTL:      time.Duration(cfg.IPLocation.CacheTTLHours) * time.Hour,
		PrivateLabel:  cfg.IPLocation.PrivateLabel,
		DisabledLabel: cfg.IPLocation.DisabledLabel,
		UnknownLabel:  cfg.IPLocation.UnknownLabel,
	}, a.businessCache)
}

// newRouter 装配全部控制器与中间件。
//
// 依赖方向：app 构造 service 与控制器并注入 router；router 只依赖 api 层与 permission。
//
// 返回 Returns:
//   - engine (*gin.Engine): 已完成中间件与路由注册的 Gin 引擎。
func (a *App) newRouter() *gin.Engine {
	// 领域操作层
	userLogic := logic.NewUserLogic(a.webDB)
	roleLogic := logic.NewRoleLogic(a.webDB)
	deptLogic := logic.NewDeptLogic(a.webDB)
	menuLogic := logic.NewMenuLogic(a.webDB)
	postLogic := logic.NewPostLogic(a.webDB)
	dictLogic := logic.NewDictLogic(a.webDB)
	configLogic := logic.NewConfigLogic(a.webDB)
	noticeLogic := logic.NewNoticeLogic(a.webDB)
	logLogic := logic.NewLogLogic(a.webDB)
	authLogic := logic.NewAuthLogic(a.webDB)
	attachmentLogic := logic.NewAttachmentLogic(a.webDB)

	// 权限服务
	perms := permission.New(a.webDB)

	// 缓存管理：进程内缓存 +（可选）Redis
	a.caches = a.newCacheManager(perms)
	cacheService := service.NewCacheService(a.caches)

	// 应用服务层
	// 会话失效登记：强退后让 access token 立即失效（Redis 可用时跨实例共享）。
	sessionGuard := session.NewGuard(a.businessCache)
	authService := service.NewAuthService(authLogic, perms, service.TokenConfig{
		Secret:        a.cfg.JWT.Secret,
		AccessSeconds: a.cfg.JWT.AccessSeconds,
		RefreshDays:   a.cfg.JWT.RefreshDays,
	}, a.locations, sessionGuard, a.newLoginGuard())
	userService := service.NewUserService(userLogic, deptLogic, perms)
	roleService := service.NewRoleService(roleLogic, perms)
	deptService := service.NewDeptService(deptLogic)
	menuService := service.NewMenuService(menuLogic, roleLogic, userLogic, perms)
	postService := service.NewPostService(postLogic)
	dictService := service.NewDictService(dictLogic, a.events, a.businessCache)
	configService := service.NewConfigService(configLogic, a.businessCache)
	noticeService := service.NewNoticeService(noticeLogic)
	logService := service.NewLogService(logLogic)
	onlineService := service.NewOnlineService(authLogic, authService)
	// 启动预热：把全量系统配置写入缓存（失败只告警，不影响启动）。
	if err := configService.PrimeCache(context.Background()); err != nil {
		slog.Warn("webserver 配置缓存预热失败", "error", err)
	}
	attachmentService := service.NewAttachmentService(attachmentLogic, service.UploadConfig{
		Dir:       a.cfg.Upload.Dir,
		URLPrefix: a.cfg.Upload.URLPrefix,
		MaxSizeMB: a.cfg.Upload.MaxSizeMB,
	})

	// 控制器层
	authAPI := v1.NewAuthAPI(authService, a.captcha, v1.CaptchaConfig{
		Enabled: a.cfg.CaptchaEnabled(),
		Width:   a.cfg.Captcha.Width,
		Height:  a.cfg.Captcha.Height,
	})
	return apiRouter.New(apiRouter.Deps{
		Protection: middleware.ProtectionConfig{
			Enabled:           a.cfg.Protection.Enabled,
			APIRequests:       a.cfg.Protection.APIRequests,
			APIWindow:         time.Duration(a.cfg.Protection.APIWindowSeconds) * time.Second,
			LoginRequests:     a.cfg.Protection.LoginRequests,
			LoginWindow:       time.Duration(a.cfg.Protection.LoginWindowSeconds) * time.Second,
			Blacklist:         a.cfg.Protection.Blacklist,
			Whitelist:         a.cfg.Protection.Whitelist,
			TrustProxyHeaders: a.cfg.Protection.TrustProxyHeaders,
		},
		Auth:             authAPI,
		Users:            v1.NewUserAPI(userService),
		Roles:            v1.NewRoleAPI(roleService),
		Depts:            v1.NewDeptAPI(deptService),
		Menus:            v1.NewMenuAPI(menuService),
		Posts:            v1.NewPostAPI(postService),
		Dicts:            v1.NewDictAPI(dictService),
		Configs:          v1.NewConfigAPI(configService),
		Notices:          v1.NewNoticeAPI(noticeService),
		Logs:             v1.NewLogAPI(logService),
		Attachments:      v1.NewAttachmentAPI(attachmentService),
		Caches:           v1.NewCacheAPI(cacheService),
		Online:           v1.NewOnlineAPI(onlineService),
		UploadDir:        a.cfg.Upload.Dir,
		UploadURLPrefix:  a.cfg.Upload.URLPrefix,
		Monitor:          v1.NewMonitorAPI(a.monitor, a.events),
		Remote:           v1.NewRemoteAPI(a.remote),
		AlertRules:       v1.NewAlertRuleAPI(a.resourceAlert),
		DangerousCommand: v1.NewDangerousCommandAPI(a.remote),
		Job:              v1.NewJobAPI(a.jobSvc),
		JobLog:           v1.NewJobLogAPI(a.jobSvc),
		AuthService:      authService,
		Perms:            perms,
		OperateLog:       middleware.OperateLog(a.webDB, a.locations),
		AccessLog:        a.cfg.AccessLog,
		Print:            a.printAccessLog,
		// 安全中间件：CORS 复用 pkg/middleware 的原版组件。
		CORSConfig:    a.cfg.Middleware.CORS,
		CSRFConfig:    middleware.CSRFConfig{Enabled: a.cfg.XSRFEnabled(), AllowOrigins: a.cfg.XSRFAllowOrigins()},
		XSSEnabled:    a.cfg.XSSEnabled(),
		DemoEnabled:   a.cfg.DemoEnabled,
		DemoWhitelist: a.cfg.DemoWhitelist,
	})
}

// printAccessLog 输出一条访问日志：控制台保持原有彩色格式，同时落一份到 zap（logs 目录）。
//
// 参数 Parameters:
//   - message (string): 已格式化的访问日志内容。
func (a *App) printAccessLog(message string) {
	printAccessLog(message)
	if a.log != nil {
		a.log.Info("http access", zap.String("line", message))
	}
}

// newCacheManager 装配缓存管理器：Redis（可配置启用）与进程内缓存（权限快照、登录验证码）。
//
// 参数 Parameters:
//   - perms (*permission.Service): 权限服务，用于暴露权限快照缓存。
//
// 返回 Returns:
//   - manager (*cache.Manager): 缓存管理器。
func (a *App) newCacheManager(perms *permission.Service) *cache.Manager {
	var redisSource *cache.RedisSource
	if a.cfg.Redis.Enabled {
		// 复用 pkg/redisx 的 go-redis 客户端装配；每个库一个客户端。
		redisSource = cache.NewRedisSource(redisx.Config{
			Addr:     a.cfg.Redis.Addr,
			Username: a.cfg.Redis.Username,
			Password: a.cfg.Redis.Password,
			DB:       a.cfg.Redis.DB,
		}, a.cfg.Redis.Databases, a.cfg.Redis.ScanLimit,
			time.Duration(a.cfg.Redis.TimeoutSeconds)*time.Second, a.customCacheGroups())
	}
	sources := newCacheSources(perms, a.captcha)
	// 业务缓存走 Redis 时其键由 Redis 来源展示，只有进程内后端才需要单独挂载。
	if a.businessCache != nil && a.businessCache.Backend() == cachestore.BackendMemory {
		sources = append(sources, businessCacheSource{store: a.businessCache})
	}
	return cache.NewManager(redisSource, sources...)
}

// customCacheGroups 把配置里的自定义缓存分组转换为缓存模块的分组定义。
//
// 返回 Returns:
//   - groups ([]cache.CustomGroup): 自定义分组。
func (a *App) customCacheGroups() []cache.CustomGroup {
	groups := make([]cache.CustomGroup, 0, len(a.cfg.Redis.Groups))
	for _, group := range a.cfg.Redis.Groups {
		name := strings.TrimSpace(group.Name)
		pattern := strings.TrimSpace(group.Pattern)
		if name == "" || pattern == "" {
			continue
		}
		groups = append(groups, cache.CustomGroup{Name: name, Pattern: pattern})
	}
	return groups
}

// slogWriter 把 GORM 的日志桥接到 slog，统一 webserver 的日志出口。
type slogWriter struct{}

// Printf 输出一条 GORM 日志（GORM 已在内部按级别过滤）。
//
// 参数 Parameters:
//   - format (string): 消息模板，由 GORM logger 传入。
//   - args (...any): 模板参数。
func (slogWriter) Printf(format string, args ...any) {
	slog.Warn(strings.TrimSpace(fmt.Sprintf(format, args...)))
}

// gormLogger 返回统一配置的 GORM 日志器。
//
// 关键点：忽略 ErrRecordNotFound。系统大量使用「查询未命中即回源/新建」的模式
// （如按用户名探测用户是否存在、按 token_hash 校验刷新令牌），未命中属于正常路径，
// 若保留 GORM 默认行为会在滚动日志里刷出大量红色 record not found。
//
// 返回 Returns:
//   - logger (logger.Interface): 已关闭 not-found 日志、保留慢查询与错误输出的日志器。
func gormLogger() logger.Interface {
	return logger.New(slogWriter{}, logger.Config{
		SlowThreshold:             500 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
	})
}

// postgresDSN 拼装 PostgreSQL 连接串。
//
// 参数 Parameters:
//   - cfg (PostgresConfig): 连接配置。
//
// 返回 Returns:
//   - dsn (string): 供 gorm.io/driver/postgres 使用的连接串。
func postgresDSN(cfg PostgresConfig) string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database, cfg.SSLMode, cfg.TimeZone)
}

// Close 释放时序库客户端、验证码清理协程、Redis 连接与计划任务调度器（幂等）。
func (a *App) Close() {
	if a.resourceAlert != nil {
		a.resourceAlert.Stop()
	}
	if a.jobSvc != nil {
		a.jobSvc.Stop()
	}
	if a.influx != nil {
		a.influx.Close()
		a.influx = nil
	}
	if a.captcha != nil {
		a.captcha.Close()
		a.captcha = nil
	}
	if a.caches != nil {
		a.caches.Close()
		a.caches = nil
	}
	if a.log != nil {
		// 退出前刷盘：lumberjack 在 Sync 时关闭当前文件句柄。
		_ = a.log.Sync()
		a.log = nil
	}
}

// Router 返回装配完成的 HTTP 处理器。
//
// 返回 Returns:
//   - handler (http.Handler): Gin 引擎。
func (a *App) Router() http.Handler { return a.router }

// RunEventPublisher 周期性推送监控概览事件，供前端 SSE 订阅方刷新。
//
// 参数 Parameters:
//   - ctx (context.Context): 上层生命周期；取消后协程退出。
func (a *App) RunEventPublisher(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			overview, err := a.monitor.Overview()
			if err != nil {
				slog.Warn("webserver 监控概览推送失败", "error", err)
				continue
			}
			a.events.Publish(eventhub.TopicMonitorAgentStatus, overview)
		}
	}
}
