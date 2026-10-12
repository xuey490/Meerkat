package job

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// Service 提供计划任务的调度、管理与执行能力。
type Service struct {
	db           *gorm.DB
	webDB        *gorm.DB
	cron         *cron.Cron
	entries      map[uint]cron.EntryID
	mu           sync.RWMutex
	taskRegistry map[string]TaskFunc
	influxURL    string
	influxToken  string
	influxOrg    string
	influxBucket string
	pgConfig     PostgresConfig
	backupDir    string
}

// PostgresConfig PostgreSQL 连接配置。
type PostgresConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
}

// TaskFunc 是任务执行函数签名。
type TaskFunc func(ctx context.Context, svc *Service) (string, error)

// New 创建计划任务服务。
//
// 参数 Parameters:
//   - backupDir (string): 备份产物目录。旧实现从 sqlite_path 推导同级 backup 目录，
//     权限库迁到 PostgreSQL 后不再有数据库文件路径，因此改由配置传入。
func New(db, webDB *gorm.DB, influxURL, influxToken, influxOrg, influxBucket, backupDir string, pg PostgresConfig) *Service {
	if strings.TrimSpace(backupDir) == "" {
		backupDir = "data/backup"
	}
	s := &Service{
		db:           db,
		webDB:        webDB,
		cron:         cron.New(cron.WithSeconds()),
		entries:      make(map[uint]cron.EntryID),
		taskRegistry: make(map[string]TaskFunc),
		influxURL:    influxURL,
		influxToken:  influxToken,
		influxOrg:    influxOrg,
		influxBucket: influxBucket,
		pgConfig:     pg,
		backupDir:    backupDir,
	}
	s.registerBuiltinTasks()
	return s
}

// Migrate 创建计划任务相关表（幂等）。
func (s *Service) Migrate() error {
	return s.db.AutoMigrate(&Job{}, &JobLog{})
}

// Start 启动调度器并加载数据库中已启用的任务。
func (s *Service) Start() error {
	s.cron.Start()
	var jobs []Job
	if err := s.db.Where("status = ? AND del_flag = ?", "0", "0").Find(&jobs).Error; err != nil {
		return err
	}
	for _, j := range jobs {
		if err := s.schedule(j); err != nil {
			slog.Warn("计划任务加载失败", "job_id", j.ID, "name", j.Name, "err", err)
		}
	}
	return nil
}

// Stop 停止调度器。
func (s *Service) Stop() {
	s.cron.Stop()
}

// ---------------------------------------------------------------------------
// 内置任务注册
// ---------------------------------------------------------------------------

func (s *Service) registerBuiltinTasks() {
	s.taskRegistry["cleanInfluxDB"] = taskCleanInfluxDB
	s.taskRegistry["cleanLoginLog"] = taskCleanLoginLog
	s.taskRegistry["cleanOperLog"] = taskCleanOperLog
	s.taskRegistry["cleanAlertData"] = taskCleanAlertData
	s.taskRegistry["backupSQLite"] = taskBackupSQLite
	s.taskRegistry["backupPostgres"] = taskBackupPostgres
}

// RegisterTask 注册一个外部任务实现。
//
// 用途：有些任务需要跨模块协作（例如「清理过期远程桌面录像」要调用 remote 服务），
// 而 job 服务不应反向依赖它们。上层在装配阶段用闭包把依赖包进来即可。
//
// 参数 Parameters:
//   - name (string): 任务名，对应 sa_job.invoke_target。
//   - fn (TaskFunc): 任务实现。
func (s *Service) RegisterTask(name string, fn TaskFunc) {
	if strings.TrimSpace(name) == "" || fn == nil {
		return
	}
	s.taskRegistry[name] = fn
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

// ListJobs 分页查询计划任务。
func (s *Service) ListJobs(page, size int, keyword string) ([]Job, int64, error) {
	var total int64
	query := s.db.Model(&Job{}).Where("del_flag = ?", "0")
	if keyword != "" {
		query = query.Where("job_name LIKE ? OR invoke_target LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Job
	if err := query.Order("create_time DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// GetJob 查询单条计划任务。
func (s *Service) GetJob(id uint) (*Job, error) {
	var row Job
	if err := s.db.First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// CreateJob 新增计划任务。
func (s *Service) CreateJob(row *Job) error {
	row.CreatedAt = time.Now()
	row.UpdatedAt = time.Now()
	row.DelFlag = "0"
	if err := s.db.Create(row).Error; err != nil {
		return err
	}
	if row.Status == "0" && row.CronExpression != "" {
		return s.schedule(*row)
	}
	return nil
}

// UpdateJob 更新计划任务（只更新非零值字段，避免覆盖 del_flag/status）。
func (s *Service) UpdateJob(row *Job) error {
	row.UpdatedAt = time.Now()
	if err := s.db.Model(&Job{}).Where("id = ?", row.ID).Updates(map[string]any{
		"job_name":        row.Name,
		"job_group":       row.Group,
		"invoke_target":   row.InvokeTarget,
		"cron_expression": row.CronExpression,
		"misfire_policy":  row.MisfirePolicy,
		"concurrent":      row.Concurrent,
		"remark":          row.Remark,
		"update_by":       row.UpdatedBy,
		"update_time":     row.UpdatedAt,
	}).Error; err != nil {
		return err
	}
	s.unschedule(row.ID)
	if row.Status == "0" && row.CronExpression != "" {
		return s.schedule(*row)
	}
	return nil
}

// DeleteJob 删除计划任务（逻辑删除）。
func (s *Service) DeleteJob(id uint) error {
	s.unschedule(id)
	result := s.db.Model(&Job{}).Where("id = ?", id).Update("del_flag", "1")
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ToggleJob 启用/禁用计划任务。
func (s *Service) ToggleJob(id uint, enabled bool) error {
	status := "1"
	if enabled {
		status = "0"
	}
	result := s.db.Model(&Job{}).Where("id = ?", id).Update("status", status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	if enabled {
		row, err := s.GetJob(id)
		if err != nil {
			return err
		}
		return s.schedule(*row)
	}
	s.unschedule(id)
	return nil
}

// RunJob 立即执行一次计划任务。
func (s *Service) RunJob(id uint, operator string) error {
	row, err := s.GetJob(id)
	if err != nil {
		return err
	}
	go s.execute(*row, operator)
	return nil
}

// ---------------------------------------------------------------------------
// 日志
// ---------------------------------------------------------------------------

// ListJobLogs 分页查询任务执行日志。
func (s *Service) ListJobLogs(page, size int, jobName string) ([]JobLog, int64, error) {
	var total int64
	query := s.db.Model(&JobLog{})
	if jobName != "" {
		query = query.Where("job_name = ?", jobName)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []JobLog
	if err := query.Order("create_time DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// DeleteJobLogs 按主键批量删除任务执行日志（任务日志页面的多选清理）。
//
// sa_job_log 没有软删除列，因此这里是真的 DELETE；调用方按 RowsAffected 回报删除条数。
//
// 参数 Parameters:
//   - ids ([]int64): 日志主键列表。
//
// 返回 Returns:
//   - removed (int64): 实际删除的条数。
//   - err (error): 删除失败时返回非 nil。
func (s *Service) DeleteJobLogs(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := s.db.Where("id IN ?", ids).Delete(&JobLog{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// CleanJobLogs 清空任务执行日志。
func (s *Service) CleanJobLogs() error {
	return s.db.Exec("DELETE FROM sa_job_log").Error
}

// ---------------------------------------------------------------------------
// 调度内部
// ---------------------------------------------------------------------------

func (s *Service) schedule(j Job) error {
	if j.CronExpression == "" {
		return nil
	}
	if _, ok := s.taskRegistry[j.InvokeTarget]; !ok {
		return fmt.Errorf("未知任务目标: %s", j.InvokeTarget)
	}
	entryID, err := s.cron.AddFunc(j.CronExpression, func() {
		s.execute(j, "cron")
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.entries[j.ID] = entryID
	s.mu.Unlock()
	return nil
}

func (s *Service) unschedule(id uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entryID, ok := s.entries[id]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, id)
	}
}

func (s *Service) execute(j Job, triggerBy string) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	msg, err := s.taskRegistry[j.InvokeTarget](ctx, s)
	status := "0"
	exception := ""
	if err != nil {
		status = "1"
		exception = err.Error()
		msg = "执行失败: " + exception
	}

	duration := time.Since(start)
	jobMsg := fmt.Sprintf("[%s] %s 耗时 %s", triggerBy, msg, duration)

	log := JobLog{
		Name:          j.Name,
		Group:         j.Group,
		InvokeTarget:  j.InvokeTarget,
		JobMessage:    jobMsg,
		Status:        status,
		ExceptionInfo: exception,
		CreatedAt:     time.Now(),
	}
	_ = s.db.Create(&log)
}

// ---------------------------------------------------------------------------
// 内置任务实现
// ---------------------------------------------------------------------------

// taskCleanInfluxDB 清理 InfluxDB 中 N 天前的数据（默认 7 天）。
func taskCleanInfluxDB(ctx context.Context, svc *Service) (string, error) {
	retentionDays := 7
	start := "1970-01-01T00:00:00Z"
	stop := time.Now().AddDate(0, 0, -retentionDays).Format(time.RFC3339)

	// 使用 InfluxDB HTTP API 直接删除，无需 influx CLI
	deleteURL := fmt.Sprintf("%s/api/v2/delete?org=%s&bucket=%s",
		svc.influxURL, svc.influxOrg, svc.influxBucket)
	body := fmt.Sprintf(`{"start":"%s","stop":"%s"}`, start, stop)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deleteURL, strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("创建删除请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Token "+svc.influxToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("influx delete 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("influx delete 失败: HTTP %d, %s", resp.StatusCode, string(respBody))
	}
	return fmt.Sprintf("已清理 InfluxDB %d 天前的数据", retentionDays), nil
}

// taskCleanLoginLog 清理登录日志（sa_system_login_log 表，在权限库 PostgreSQL 中）。
func taskCleanLoginLog(ctx context.Context, svc *Service) (string, error) {
	if svc.webDB == nil {
		return "", fmt.Errorf("webDB 未初始化，无法清理登录日志")
	}
	retentionDays := 30
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result := svc.webDB.Exec("DELETE FROM sa_system_login_log WHERE login_time < ?", cutoff)
	if result.Error != nil {
		return "", result.Error
	}
	return fmt.Sprintf("已清理 %d 天前的登录日志，影响 %d 条", retentionDays, result.RowsAffected), nil
}

// taskCleanOperLog 清理操作日志（sa_system_oper_log 表，在权限库 PostgreSQL 中）。
func taskCleanOperLog(ctx context.Context, svc *Service) (string, error) {
	if svc.webDB == nil {
		return "", fmt.Errorf("webDB 未初始化，无法清理操作日志")
	}
	retentionDays := 30
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result := svc.webDB.Exec("DELETE FROM sa_system_oper_log WHERE create_time < ?", cutoff)
	if result.Error != nil {
		return "", result.Error
	}
	return fmt.Sprintf("已清理 %d 天前的操作日志，影响 %d 条", retentionDays, result.RowsAffected), nil
}

// ---------------------------------------------------------------------------
// 告警数据保留策略
// ---------------------------------------------------------------------------

// 告警数据保留策略的配置键（对应 sa_system_config."key"）。
//
// 为什么不放在 webserver.yaml：保留天数属于「运维按磁盘水位临时会调」的参数
// （磁盘吃紧就调小、要留档就调大）。放配置文件意味着每改一次都得重启服务；
// 放系统配置表则改完立即生效，且与站点名称、上传大小等参数在同一个页面。
//
// 注意：这两个键同时硬编码在 internal/app/sql/003_system_seed.sql 的种子数据里
// （SQL 无法引用 Go 常量），改名必须两边同步，否则配置页改了值而任务读不到。
const (
	// ConfigKeyAlertEventsRetentionDays 告警事件（alert_events）保留天数。
	ConfigKeyAlertEventsRetentionDays = "alert_retention_events_days"
	// ConfigKeyAlertClosedRetentionDays 已结束告警（alerts）保留天数。
	ConfigKeyAlertClosedRetentionDays = "alert_retention_alerts_days"
)

// 保留天数的兜底默认值：配置项缺失（尚未执行到种子 SQL）或取值非法时使用，
// 与种子数据里的初始值保持一致。
const (
	// defaultAlertEventsRetentionDays 告警事件默认保留天数。
	defaultAlertEventsRetentionDays = 90
	// defaultAlertClosedRetentionDays 已结束告警默认保留天数。
	defaultAlertClosedRetentionDays = 180
)

// taskCleanAlertData 按保留策略清理告警数据。
//
// 两条策略互相独立、也可各自关掉：
//   - alert_retention_events_days（M）：删除 alert_events 中 M 天前的事件；
//   - alert_retention_alerts_days（N）：删除 alerts 中「已结束」且结束于 N 天前的告警。
//
// 为什么只删已结束的告警：closed_at 为空的行是告警状态机的活动输入——告警中心列表、
// 自动恢复判定、人工确认都依赖它。删掉活动行会让同一条告警在下次评估时被当成全新告警
// 重新触发（去重键对应的活动行消失了），正是 uk_alerts_active 部分唯一索引要防的事。
//
// 取值为 0 或负数表示「该条策略不清理」，这样能只清事件、保留告警头，而不是只能全清或全留。
//
// M 与 N 的相互作用值得注意：若 M 大于 N，被删告警的详情时间线会先于告警本身消失；
// 想让历史告警的完整时间线可回看，把 M 设为不小于 N。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文（任务级超时由调度层统一控制）。
//   - svc (*Service): 计划任务服务（提供监控库会话与系统配置读取）。
//
// 返回 Returns:
//   - message (string): 清理结果说明，写入 sa_job_log。
//   - err (error): 任一策略执行失败时返回非 nil。
func taskCleanAlertData(ctx context.Context, svc *Service) (string, error) {
	if svc.db == nil {
		return "", fmt.Errorf("监控库未初始化，无法清理告警数据")
	}
	eventsDays := svc.systemConfigInt(ConfigKeyAlertEventsRetentionDays, defaultAlertEventsRetentionDays)
	closedDays := svc.systemConfigInt(ConfigKeyAlertClosedRetentionDays, defaultAlertClosedRetentionDays)

	parts := make([]string, 0, 2)
	if eventsDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -eventsDays)
		result := svc.db.WithContext(ctx).Exec("DELETE FROM alert_events WHERE created_at < ?", cutoff)
		if result.Error != nil {
			return "", fmt.Errorf("清理告警事件失败: %w", result.Error)
		}
		parts = append(parts, fmt.Sprintf("告警事件（保留 %d 天）删除 %d 条", eventsDays, result.RowsAffected))
	} else {
		parts = append(parts, "告警事件（保留天数为 0，跳过）")
	}

	if closedDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -closedDays)
		// 「已结束」的唯一判据是 closed_at 非空，不能用 state 过滤：
		// 人工确认结束的告警 state 可能仍是 critical（指标确实还超限），
		// 用 state <> 'ok' 会把这批刚结束的告警漏掉，用 state = 'ok' 又会误删活动告警。
		result := svc.db.WithContext(ctx).
			Exec("DELETE FROM alerts WHERE closed_at IS NOT NULL AND closed_at < ?", cutoff)
		if result.Error != nil {
			return "", fmt.Errorf("清理已结束告警失败: %w", result.Error)
		}
		parts = append(parts, fmt.Sprintf("已结束告警（保留 %d 天）删除 %d 条", closedDays, result.RowsAffected))
	} else {
		parts = append(parts, "已结束告警（保留天数为 0，跳过）")
	}

	return "告警数据清理完成：" + strings.Join(parts, "；"), nil
}

// systemConfigInt 读取 sa_system_config 中的整型配置项。
//
// 读不到就返回兜底值而不是报错：清理任务仅因为一个配置项缺失/写错就整条失败，
// 会让保留策略彻底停摆、磁盘反而更快写满——这与「保护磁盘」的目标相反。
//
// 参数 Parameters:
//   - key (string): 配置键。
//   - fallback (int): 读取失败或取值非法时的兜底值。
//
// 返回 Returns:
//   - value (int): 配置值；读不到时为兜底值。
func (s *Service) systemConfigInt(key string, fallback int) int {
	if s.webDB == nil {
		return fallback
	}
	var row struct {
		// Value 配置值；列可为 NULL，故用指针接。
		Value *string `gorm:"column:value"`
	}
	// delete_time IS NULL 必须显式写：这里只查单列、不走 model.SysConfig，
	// GORM 不会自动补软删除条件。虽然软删除时键会被改写成
	// 「<原值>#deleted#<id>」而不会命中本查询，但这层过滤成本为零，留着更稳。
	if err := s.webDB.Table("sa_system_config").
		Select("value").
		Where(`"key" = ? AND delete_time IS NULL`, key).
		Limit(1).
		Scan(&row).Error; err != nil {
		slog.Debug("读取告警保留配置失败，使用默认值", "key", key, "fallback", fallback, "err", err)
		return fallback
	}
	if row.Value == nil {
		return fallback
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(*row.Value))
	if err != nil {
		slog.Warn("告警保留配置取值非法，使用默认值", "key", key, "value", *row.Value, "fallback", fallback)
		return fallback
	}
	return parsed
}

// taskBackupSQLite 备份权限库。
//
// 任务名保留 backupSQLite 是刻意的历史兼容：sa_job 表里可能已有这一行，
// 改名会让老任务在调度时被当成「未知任务」而静默停止执行。
//
// 权限库已从 SQLite 文件迁到 PostgreSQL，并且与监控库同一个 database，
// 因此这里执行的 PostgreSQL 备份同时覆盖权限数据（sa_system_*）与监控数据。
func taskBackupSQLite(ctx context.Context, svc *Service) (string, error) {
	message, err := runPostgresBackup(ctx, svc)
	if err != nil {
		return "", err
	}
	return message + "（权限库与监控库同库，已一并备份）", nil
}

// taskBackupPostgres 备份监控库。
//
// 与权限库在同一个 database 内，所以这次备份实际上覆盖了全部业务表。
func taskBackupPostgres(ctx context.Context, svc *Service) (string, error) {
	return runPostgresBackup(ctx, svc)
}
