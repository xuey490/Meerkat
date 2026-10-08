package job

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// Service 提供计划任务的调度、管理与执行能力。
type Service struct {
	db          *gorm.DB
	webDB       *gorm.DB
	cron        *cron.Cron
	entries     map[uint]cron.EntryID
	mu          sync.RWMutex
	taskRegistry map[string]TaskFunc
	influxURL   string
	influxToken string
	influxOrg   string
	influxBucket string
	sqlitePath  string
	pgConfig    PostgresConfig
	backupDir   string
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
func New(db, webDB *gorm.DB, influxURL, influxToken, influxOrg, influxBucket, sqlitePath string, pg PostgresConfig) *Service {
	backupDir := "backup"
	if sqlitePath != "" {
		backupDir = filepath.Join(filepath.Dir(sqlitePath), "backup")
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
		sqlitePath:   sqlitePath,
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
	s.taskRegistry["backupSQLite"] = taskBackupSQLite
	s.taskRegistry["backupPostgres"] = taskBackupPostgres
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
		"job_name":       row.Name,
		"job_group":      row.Group,
		"invoke_target":  row.InvokeTarget,
		"cron_expression": row.CronExpression,
		"misfire_policy": row.MisfirePolicy,
		"concurrent":     row.Concurrent,
		"remark":         row.Remark,
		"update_by":      row.UpdatedBy,
		"update_time":    row.UpdatedAt,
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

// taskCleanLoginLog 清理登录日志（sa_system_login_log 表，在 SQLite 中）。
func taskCleanLoginLog(ctx context.Context, svc *Service) (string, error) {
	if svc.webDB == nil {
		return "", fmt.Errorf("webDB 未初始化，无法清理 SQLite 登录日志")
	}
	retentionDays := 30
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result := svc.webDB.Exec("DELETE FROM sa_system_login_log WHERE login_time < ?", cutoff)
	if result.Error != nil {
		return "", result.Error
	}
	return fmt.Sprintf("已清理 %d 天前的登录日志，影响 %d 条", retentionDays, result.RowsAffected), nil
}

// taskCleanOperLog 清理操作日志（sa_system_oper_log 表，在 SQLite 中）。
func taskCleanOperLog(ctx context.Context, svc *Service) (string, error) {
	if svc.webDB == nil {
		return "", fmt.Errorf("webDB 未初始化，无法清理 SQLite 操作日志")
	}
	retentionDays := 30
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result := svc.webDB.Exec("DELETE FROM sa_system_oper_log WHERE create_time < ?", cutoff)
	if result.Error != nil {
		return "", result.Error
	}
	return fmt.Sprintf("已清理 %d 天前的操作日志，影响 %d 条", retentionDays, result.RowsAffected), nil
}

// taskBackupSQLite 备份 SQLite 数据库。
func taskBackupSQLite(ctx context.Context, svc *Service) (string, error) {
	if svc.sqlitePath == "" {
		return "", fmt.Errorf("sqlite_path 未配置")
	}
	if err := os.MkdirAll(svc.backupDir, 0o755); err != nil {
		return "", err
	}
	// 年月日 + 4 位随机数，如 webserver_20261007_7391.db
	timestamp := time.Now().Format("20060102")
	randNum := strconv.Itoa(1000 + int(time.Now().UnixNano()%9000))
	dest := filepath.Join(svc.backupDir, fmt.Sprintf("webserver_%s_%s.db", timestamp, randNum))

	// 通过 webDB 执行 SQLite 的 backup API，无需外部 sqlite3 CLI
	if svc.webDB == nil {
		return "", fmt.Errorf("webDB 未初始化，无法执行 SQLite 备份")
	}
	sqlDB, err := svc.webDB.DB()
	if err != nil {
		return "", fmt.Errorf("获取底层 SQL DB 失败: %w", err)
	}

	// 先执行 checkpoint 确保 WAL 数据落盘
	if _, err := sqlDB.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return "", fmt.Errorf("wal_checkpoint 失败: %w", err)
	}

	// 使用 Go 标准库复制数据库文件（此时文件已处于一致状态）
	srcFile, err := os.Open(svc.sqlitePath)
	if err != nil {
		return "", fmt.Errorf("打开源数据库失败: %w", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("创建备份文件失败: %w", err)
	}
	defer destFile.Close()

	if _, err := destFile.ReadFrom(srcFile); err != nil {
		return "", fmt.Errorf("复制数据库文件失败: %w", err)
	}
	return fmt.Sprintf("SQLite 已备份到 %s", dest), nil
}

// taskBackupPostgres 备份 PostgreSQL 数据库。
func taskBackupPostgres(ctx context.Context, svc *Service) (string, error) {
	pg := svc.pgConfig
	if err := os.MkdirAll(svc.backupDir, 0o755); err != nil {
		return "", err
	}
	// 年月日 + 4 位随机数，如 postgres_20261007_7391.sql
	timestamp := time.Now().Format("20060102")
	randNum := strconv.Itoa(1000 + int(time.Now().UnixNano()%9000))
	dest := filepath.Join(svc.backupDir, fmt.Sprintf("postgres_%s_%s.sql", timestamp, randNum))

	// 通过底层 *sql.DB 执行 pg_dump 等价的纯 SQL 备份，无需外部 pg_dump CLI
	sqlDB, err := svc.db.DB()
	if err != nil {
		return "", fmt.Errorf("获取底层 SQL DB 失败: %w", err)
	}

	// 查询所有用户表（排除系统 schema）
	rows, err := sqlDB.QueryContext(ctx, `
		SELECT schemaname, tablename
		FROM pg_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY schemaname, tablename
	`)
	if err != nil {
		return "", fmt.Errorf("查询表列表失败: %w", err)
	}
	defer rows.Close()

	type tableInfo struct {
		schema string
		name   string
	}
	var tables []tableInfo
	for rows.Next() {
		var t tableInfo
		if err := rows.Scan(&t.schema, &t.name); err != nil {
			return "", fmt.Errorf("扫描表信息失败: %w", err)
		}
		tables = append(tables, t)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("遍历表列表失败: %w", err)
	}

	// 创建备份文件
	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("创建备份文件失败: %w", err)
	}
	defer f.Close()

	// 写入备份头
	fmt.Fprintf(f, "-- PostgreSQL Backup\n")
	fmt.Fprintf(f, "-- Database: %s\n", pg.Database)
	fmt.Fprintf(f, "-- Time: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	// 逐个表导出：先 DROP/CREATE，再 INSERT
	for _, t := range tables {
		fullName := fmt.Sprintf("%q.%q", t.schema, t.name)
		// 查询表数据
		dataRows, err := sqlDB.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s", fullName))
		if err != nil {
			return "", fmt.Errorf("查询表 %s 失败: %w", fullName, err)
		}

		cols, err := dataRows.Columns()
		if err != nil {
			dataRows.Close()
			return "", fmt.Errorf("获取表 %s 列信息失败: %w", fullName, err)
		}

		fmt.Fprintf(f, "\n-- Table: %s\n", fullName)

		// 1. DROP TABLE IF EXISTS
		fmt.Fprintf(f, "DROP TABLE IF EXISTS %s CASCADE;\n", fullName)

		// 2. 获取 CREATE TABLE 语句
		var createSQL string
		if err := sqlDB.QueryRowContext(ctx, fmt.Sprintf(
			"SELECT pg_catalog.pg_get_tabledef(%q::regclass::oid, true, true)", fullName),
		).Scan(&createSQL); err != nil {
			// pg_get_tabledef 可能不存在，回退到 information_schema 方式
			createSQL, err = buildCreateTableSQL(ctx, sqlDB, t.schema, t.name)
			if err != nil {
				return "", fmt.Errorf("生成表 %s 建表语句失败: %w", fullName, err)
			}
		}
		fmt.Fprintf(f, "%s;\n", createSQL)

		// 3. 导出 INSERT 语句
		values := make([]any, len(cols))
		valuePtrs := make([]any, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		for dataRows.Next() {
			if err := dataRows.Scan(valuePtrs...); err != nil {
				dataRows.Close()
				return "", fmt.Errorf("扫描表 %s 数据失败: %w", fullName, err)
			}

			fmt.Fprintf(f, "INSERT INTO %s (", fullName)
			for i, col := range cols {
				if i > 0 {
					fmt.Fprint(f, ", ")
				}
				fmt.Fprintf(f, "%q", col)
			}
			fmt.Fprint(f, ") VALUES (")
			for i, v := range values {
				if i > 0 {
					fmt.Fprint(f, ", ")
				}
				switch val := v.(type) {
				case nil:
					fmt.Fprint(f, "NULL")
				case []byte:
					s := string(val)
					fmt.Fprintf(f, "'%s'", escapeSQLString(s))
				case string:
					fmt.Fprintf(f, "'%s'", escapeSQLString(val))
				case time.Time:
					fmt.Fprintf(f, "'%s'", val.Format("2006-01-02 15:04:05"))
				case bool:
					if val {
						fmt.Fprint(f, "true")
					} else {
						fmt.Fprint(f, "false")
					}
				default:
					fmt.Fprintf(f, "%v", val)
				}
			}
			fmt.Fprintln(f, ");")
		}
		dataRows.Close()
		if err := dataRows.Err(); err != nil {
			return "", fmt.Errorf("遍历表 %s 数据失败: %w", fullName, err)
		}
	}

	return fmt.Sprintf("PostgreSQL 已备份到 %s", dest), nil
}

// buildCreateTableSQL 通过 information_schema 生成 CREATE TABLE 语句。
func buildCreateTableSQL(ctx context.Context, db *sql.DB, schema, table string) (string, error) {
	// 查询列信息
	colRows, err := db.QueryContext(ctx, `
		SELECT column_name, data_type, character_maximum_length, numeric_precision, numeric_scale, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`, schema, table)
	if err != nil {
		return "", err
	}
	defer colRows.Close()

	var cols []string
	for colRows.Next() {
		var colName, dataType, isNullable string
		var maxLen, numPrec, numScale sql.NullInt64
		var colDefault sql.NullString
		if err := colRows.Scan(&colName, &dataType, &maxLen, &numPrec, &numScale, &isNullable, &colDefault); err != nil {
			return "", err
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "    %q %s", colName, dataType)
		if maxLen.Valid && maxLen.Int64 > 0 {
			fmt.Fprintf(&sb, "(%d)", maxLen.Int64)
		} else if numPrec.Valid {
			if numScale.Valid && numScale.Int64 > 0 {
				fmt.Fprintf(&sb, "(%d,%d)", numPrec.Int64, numScale.Int64)
			} else {
				fmt.Fprintf(&sb, "(%d)", numPrec.Int64)
			}
		}
		if isNullable == "NO" {
			sb.WriteString(" NOT NULL")
		}
		if colDefault.Valid {
			fmt.Fprintf(&sb, " DEFAULT %s", colDefault.String)
		}
		cols = append(cols, sb.String())
	}
	if err := colRows.Err(); err != nil {
		return "", err
	}

	// 查询主键
	pkRows, err := db.QueryContext(ctx, `
		SELECT kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = $1 AND tc.table_name = $2
		ORDER BY kcu.ordinal_position
	`, schema, table)
	if err != nil {
		return "", err
	}
	defer pkRows.Close()

	var pks []string
	for pkRows.Next() {
		var col string
		if err := pkRows.Scan(&col); err != nil {
			return "", err
		}
		pks = append(pks, fmt.Sprintf("%q", col))
	}
	if len(pks) > 0 {
		cols = append(cols, fmt.Sprintf("    PRIMARY KEY (%s)", strings.Join(pks, ", ")))
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "CREATE TABLE %q.%q (\n%s\n)", schema, table, strings.Join(cols, ",\n"))
	return sb.String(), nil
}

// escapeSQLString 转义 SQL 字符串中的单引号。
func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
