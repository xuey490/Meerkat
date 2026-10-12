// Command pgdump 把 PostgreSQL 导出为「可直接导入」的纯 SQL
// （结构 + 数据 + 索引 + 约束 + 序列 + setval）。
//
// 与计划任务里的 backupPostgres 共用 internal/pgdump，因此手工导出的结果与定时备份
// 完全一致；主要用途是生成仓库根目录的 database/init.sql（新环境初始化脚本）。
//
// 用法（在 webserver 目录下）：
//
//	go run ./tools/pgdump -out ../database/init.sql
//	go run ./tools/pgdump -schemas public -out dump.sql
//	go run ./tools/pgdump -exclude-data sa_system_oper_log,sa_system_login_log,alerts -out dump.sql
//	go run ./tools/pgdump -no-data -out schema-only.sql
//
// 连接信息默认读 configs/webserver.yaml 的 postgres 段，密码可用 MONITOR_POSTGRES_PASSWORD 覆盖。
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"gopkg.in/yaml.v3"

	"github.com/company/monitor-webserver/internal/pgdump"
)

// fileConfig 是 webserver.yaml 中本工具需要的那部分配置。
type fileConfig struct {
	Postgres struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		User     string `yaml:"user"`
		Password string `yaml:"password"`
		Database string `yaml:"database"`
		SSLMode  string `yaml:"ssl_mode"`
	} `yaml:"postgres"`
}

func main() {
	configPath := flag.String("config", filepath.Join("configs", "webserver.yaml"), "webserver.yaml 路径（读取 postgres 段）")
	outPath := flag.String("out", "", "输出文件路径；留空则写到标准输出")
	schemas := flag.String("schemas", "", "限定导出的 schema（逗号分隔）；留空表示全部用户 schema")
	excludeTables := flag.String("exclude-tables", "", "跳过的表（逗号分隔，支持 表名 或 schema.表名）")
	excludeData := flag.String("exclude-data", "", "只导结构、不导数据的表（逗号分隔）")
	noData := flag.Bool("no-data", false, "所有表都只导结构")
	batchSize := flag.Int("batch", 200, "单条 INSERT 的最大行数")
	dbName := flag.String("dbname", "", "覆盖要连接的库名（默认取配置里的 postgres.database）")
	database := flag.String("database", "", "覆盖文件头里的库名（默认与 -dbname 一致）")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fail("读取配置失败: %v", err)
	}
	if value := strings.TrimSpace(*dbName); value != "" {
		cfg.Postgres.Database = value
	}
	db, err := openDB(cfg)
	if err != nil {
		fail("连接数据库失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	name := *database
	if name == "" {
		name = cfg.Postgres.Database
	}
	opts := pgdump.Options{
		Schemas:       splitList(*schemas),
		ExcludeTables: splitList(*excludeTables),
		ExcludeData:   splitList(*excludeData),
		IncludeData:   !*noData,
		BatchSize:     *batchSize,
		Database:      name,
	}

	// 直接写文件而不是先攒在内存里：大库导出时内存占用会与库大小成正比。
	var (
		writer io.Writer = os.Stdout
		file   *os.File
	)
	if *outPath != "" {
		created, createErr := os.Create(*outPath)
		if createErr != nil {
			fail("创建输出文件失败: %v", createErr)
		}
		file, writer = created, created
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	stats, err := pgdump.Dump(ctx, db, writer, opts)
	if err != nil {
		if file != nil {
			// 半截的备份比没有备份更危险：失败产物直接丢掉。
			_ = file.Close()
			_ = os.Remove(*outPath)
		}
		fail("导出失败: %v", err)
	}
	if file != nil {
		// 缓冲内容必须落盘，关闭错误同样要处理。
		if closeErr := file.Close(); closeErr != nil {
			fail("关闭输出文件失败: %v", closeErr)
		}
	}

	target := *outPath
	if target == "" {
		target = "标准输出"
	}
	fmt.Printf("已导出到 %s：schema %d / 表 %d / 序列 %d / 索引 %d / 约束 %d / 数据 %d 行\n",
		target, stats.Schemas, stats.Tables, stats.Sequences, stats.Indexes, stats.Constraints, stats.Rows)
}

// loadConfig 读取 webserver.yaml 的 postgres 段。
//
// 参数 Parameters:
//   - path (string): 配置文件路径。
//
// 返回 Returns:
//   - cfg (fileConfig): 连接配置。
//   - err (error): 读取或解析失败时返回非 nil。
func loadConfig(path string) (fileConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fileConfig{}, err
	}
	var cfg fileConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return fileConfig{}, err
	}
	if cfg.Postgres.Host == "" {
		cfg.Postgres.Host = "localhost"
	}
	if cfg.Postgres.Port == 0 {
		cfg.Postgres.Port = 5432
	}
	if cfg.Postgres.SSLMode == "" {
		cfg.Postgres.SSLMode = "disable"
	}
	// 与 webserver 的配置加载保持一致：环境变量优先。
	if value := os.Getenv("MONITOR_POSTGRES_PASSWORD"); value != "" {
		cfg.Postgres.Password = value
	}
	if cfg.Postgres.Database == "" {
		return fileConfig{}, fmt.Errorf("%s 缺少 postgres.database", path)
	}
	return cfg, nil
}

// openDB 建立连接并确认可用。
//
// 参数 Parameters:
//   - cfg (fileConfig): 连接配置。
//
// 返回 Returns:
//   - db (*sql.DB): 连接池。
//   - err (error): 连接失败时返回非 nil。
func openDB(cfg fileConfig) (*sql.DB, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Postgres.Host, cfg.Postgres.Port, cfg.Postgres.User, cfg.Postgres.Password,
		cfg.Postgres.Database, cfg.Postgres.SSLMode)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// splitList 把逗号分隔的参数切成清单（去空白、丢空项）。
//
// 参数 Parameters:
//   - value (string): 原始参数。
//
// 返回 Returns:
//   - items ([]string): 清单。
func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	items := make([]string, 0, 8)
	for _, part := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}

// fail 打印错误并退出。
//
// 参数 Parameters:
//   - format (string): 格式串。
//   - args (...any): 参数。
func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
