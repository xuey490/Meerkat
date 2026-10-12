// Command dbscan 摸清 PostgreSQL 库的家底，并跨工作区核对每张表「是否真的在用」。
//
// 背景：项目初期规划的表里混着「设计了但没接上」的遗留设计。清理前需要两份事实：
//  1. 库里有什么：每个 schema 的表、列、索引、外键、表注释、行数（精确计数超时则退化为估算）
//     与样本行；
//  2. 代码里用没用：全工作区扫描表名字面量（GORM 的 TableName、原生 SQL、前端脚本等），
//     给出引用文件与行号；同时反向列出「代码引用了但库里没有」的表名。
//
// 结论口径：空表 + 零引用 = 清理候选；有引用但暂无数据 = 在用但当前无数据，不能删。
//
// 用法（在 webserver 目录下）：
//
//	go run ./tools/dbscan
//	go run ./tools/dbscan -sample 5 -out ../db-tables-report.md -json ../db-tables.json
//
// 连接信息默认读 configs/webserver.yaml 的 postgres 段，密码可用 MONITOR_POSTGRES_PASSWORD 覆盖。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"gopkg.in/yaml.v3"
)

// 采集参数。
const (
	// countTimeout 单表精确计数的上限；超时退化为 pg_class 的行数估算。
	countTimeout = 5 * time.Second
	// maxFileSize 参与扫描的单文件大小上限，超过的多半是导出物/二进制。
	//
	// 定在 8MB：最初的建表脚本 database/init.sql 有 3MB 出头（含 42 张表的完整定义），
	// 上限过低会把它整份跳过——而「规划过但库里没有」的表恰好只能从它里面看出来。
	maxFileSize = 8 << 20
	// maxRefPerTable 每个表在报告里保留的引用点数量。
	maxRefPerTable = 8
	// sampleCellLimit 样本单元格截断长度。
	sampleCellLimit = 120
)

// skipDirs 是扫描时跳过的目录名（导出物、依赖、二进制与无关资产）。
//
// 另外两类固定跳过（见 run）：
//   - 以 "." 开头的目录：.git / .playwright-cli / .specstory / .trae 等工具产物，
//     它们里面会出现大量表名（页面快照、AI 对话记录），计入会严重虚高引用数；
//   - -skip 指定的目录：默认是 Meerkat —— 它与 webserver/server/client 是同一套 module
//     的旧副本，保留在统计里会让「这张表在被使用」这类结论失去意义。
var skipDirs = map[string]bool{
	"node_modules": true, "dist": true, "data": true, "backup": true, "upload": true,
	"logs": true, "influxdb2": true, "Telegraf": true, "protoc64": true,
	"scripts": true, "doc": true,
	// 本工具自己的目录：它的注释里按设计会大量出现表名（举例说明「只被声明的表」等），
	// 计入统计就是自己引用自己。
	"dbscan": true,
}

// scanExts 是参与扫描的文件后缀白名单（只扫源码与配置，避免把产物当引用）。
var scanExts = map[string]bool{
	".go": true, ".sql": true, ".ts": true, ".vue": true, ".js": true, ".mjs": true, ".cjs": true,
	".json": true, ".yaml": true, ".yml": true, ".md": true, ".ps1": true, ".sh": true, ".py": true,
	".proto": true, ".conf": true, ".toml": true, ".example": true,
}

// sqlKeywordBlacklist 是反向核对时排除的非表名标识符（SQL 关键字与常用函数）。
var sqlKeywordBlacklist = map[string]bool{
	"select": true, "values": true, "dual": true, "lateral": true, "unnest": true,
	"generate_series": true, "pg_class": true, "pg_attribute": true, "pg_index": true,
	"pg_namespace": true, "information_schema": true, "current_schema": true, "jsonb_array_elements": true,
	"json_array_elements": true, "jsonb_each": true, "to_regclass": true, "pg_get_serial_sequence": true,
}

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

// columnInfo 是一列的结构信息。
type columnInfo struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Nullable string `json:"nullable"`
	Default  string `json:"default"`
	Position int    `json:"position"`
}

// indexInfo 是一个索引的结构信息（DDL 原文来自 pg_get_indexdef，避免解析 attnum 出错）。
type indexInfo struct {
	Name      string `json:"name"`
	Unique    bool   `json:"unique"`
	Primary   bool   `json:"primary"`
	DefineSQL string `json:"defineSql"`
}

// fkInfo 是一条外键。
type fkInfo struct {
	Name      string `json:"name"`
	Column    string `json:"column"`
	RefTable  string `json:"refTable"`
	RefColumn string `json:"refColumn"`
}

// refHit 是代码里对表名的一次引用。
type refHit struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
	// Kind 命中方式：literal（表名字面量）/ struct（由表名推导出的 GORM 模型名）。
	Kind string `json:"kind"`
}

// 命中方式常量。
const (
	// kindLiteral 代码里直接写了表名（GORM 的 TableName、原生 SQL、脚本等）。
	kindLiteral = "literal"
	// kindStruct GORM 默认命名推导：模型没有 TableName()，只有结构体名可查
	//（Collector 侧的 server/internal/state 就是这种写法）。
	kindStruct = "struct"
)

// tableInfo 是一张表的完整巡检结果。
type tableInfo struct {
	Schema      string       `json:"schema"`
	Name        string       `json:"name"`
	Comment     string       `json:"comment"`
	Columns     []columnInfo `json:"columns"`
	Indexes     []indexInfo  `json:"indexes"`
	ForeignKeys []fkInfo     `json:"foreignKeys"`
	RowCount    int64        `json:"rowCount"`
	CountExact  bool         `json:"countExact"`
	CountNote   string       `json:"countNote"`
	Samples     [][]string   `json:"samples"`
	Refs        []refHit     `json:"refs"`
	RefCount    int          `json:"refCount"`
	// RefByProject 引用次数按子项目聚合（webserver / server / client / web …），
	// 用来判断「谁在用它」——同一张表被前端提到和被后端写入，含义完全不同。
	RefByProject map[string]int `json:"refByProject"`
}

// report 是最终产物（Markdown + JSON 共用同一份数据）。
type report struct {
	GeneratedAt   time.Time   `json:"generatedAt"`
	DataSource    string      `json:"dataSource"`
	Workspace     string      `json:"workspace"`
	ScannedFiles  int         `json:"scannedFiles"`
	SkippedDirs   []string    `json:"skippedDirs"`
	Tables        []tableInfo `json:"tables"`
	// PlannedOnlyTables 建表脚本里 CREATE TABLE 过、但当前库里不存在的表
	//（规划过没创建，或后来被删了；对照 database/init.sql 与各服务的建表语句）。
	PlannedOnlyTables []string `json:"plannedOnlyTables"`
	// MissingTables 代码 SQL 里引用过、但当前库里不存在的表名（疑似，二次核对用）。
	MissingTables []string `json:"missingTables"`
	TotalRows     int64    `json:"totalRows"`
}

// serverVersion、dbName 用于报告抬头。
type connInfo struct {
	DSN     string
	Version string
	Database string
}

func main() {
	configPath := flag.String("config", filepath.Join("configs", "webserver.yaml"), "webserver.yaml 路径（读取 postgres 段）")
	workspaceFlag := flag.String("workspace", "", "工作区根目录；默认取 config 所在项目的上一级")
	outPath := flag.String("out", "", "Markdown 报告输出路径；默认 <workspace>/db-tables-report.md")
	jsonPath := flag.String("json", "", "同时输出 JSON 的路径；默认关闭")
	sampleCount := flag.Int("sample", 3, "每张表抽取的样本行数；0 表示不抽")
	skipFlag := flag.String("skip", "Meerkat",
		"额外跳过的顶层目录（逗号分隔）。默认跳过 Meerkat：它是同一套 module 的旧副本")
	flag.Parse()

	absConfig, err := filepath.Abs(*configPath)
	if err != nil {
		fail("解析配置路径失败: %v", err)
	}
	workspace := *workspaceFlag
	if workspace == "" {
		// configs/webserver.yaml → webserver/ → 工作区根目录
		projectDir := filepath.Dir(filepath.Dir(absConfig))
		workspace = filepath.Dir(projectDir)
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		fail("解析工作区路径失败: %v", err)
	}
	if *outPath == "" {
		*outPath = filepath.Join(absWorkspace, "db-tables-report.md")
	}

	cfg, err := loadConfig(absConfig)
	if err != nil {
		fail("读取配置失败: %v", err)
	}
	db, info, err := openDB(cfg)
	if err != nil {
		fail("连接 PostgreSQL 失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	fmt.Printf("已连接 %s（%s）\n", info.Database, info.Version)

	tables, err := loadSchema(ctx, db)
	if err != nil {
		fail("读取表结构失败: %v", err)
	}
	fmt.Printf("发现表 %d 张，开始统计行数与样本…\n", len(tables))
	loadRowCounts(ctx, db, tables)
	loadSamples(ctx, db, tables, *sampleCount)

	skips := splitAndTrim(*skipFlag)
	fmt.Printf("扫描工作区 %s（跳过: %s）…\n", absWorkspace, strings.Join(skips, ", "))
	scan := newWorkspaceScan(absWorkspace, tableNames(tables), skips)
	scanned, err := scan.run([]string{*outPath, *jsonPath})
	if err != nil {
		fail("扫描工作区失败: %v", err)
	}
	scan.apply(tables)
	missing := scan.missingTables(tables)

	result := report{
		GeneratedAt:       time.Now(),
		DataSource:        info.DSN,
		Workspace:         absWorkspace,
		ScannedFiles:      scanned,
		SkippedDirs:       skips,
		Tables:            tables,
		PlannedOnlyTables: scan.plannedOnlyTables(tables),
		MissingTables:     missing,
	}
	for _, table := range tables {
		result.TotalRows += table.RowCount
	}

	if err := os.WriteFile(*outPath, []byte(renderMarkdown(result)), 0o644); err != nil {
		fail("写出报告失败: %v", err)
	}
	if *jsonPath != "" {
		raw, marshalErr := json.MarshalIndent(result, "", "  ")
		if marshalErr != nil {
			fail("序列化 JSON 失败: %v", marshalErr)
		}
		if err := os.WriteFile(*jsonPath, raw, 0o644); err != nil {
			fail("写出 JSON 失败: %v", err)
		}
	}
	printSummary(result, *outPath)
}

// loadConfig 读取 webserver.yaml 的 postgres 段。
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

// openDB 建立连接并读出版本号（DSN 里去掉密码，报告里可以安全展示）。
func openDB(cfg fileConfig) (*sql.DB, connInfo, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Postgres.Host, cfg.Postgres.Port, cfg.Postgres.User, cfg.Postgres.Password,
		cfg.Postgres.Database, cfg.Postgres.SSLMode)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, connInfo{}, err
	}
	db.SetMaxOpenConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, connInfo{}, err
	}
	info := connInfo{
		DSN: fmt.Sprintf("%s:%d/%s (user=%s)", cfg.Postgres.Host, cfg.Postgres.Port,
			cfg.Postgres.Database, cfg.Postgres.User),
		Database: cfg.Postgres.Database,
	}
	_ = db.QueryRowContext(ctx, "select version()").Scan(&info.Version)
	return db, info, nil
}

// loadSchema 一次性读入表、列、索引、外键（避免逐表 N 次往返）。
func loadSchema(ctx context.Context, db *sql.DB) ([]tableInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tables := make([]tableInfo, 0, 128)
	index := make(map[string]int, 128)
	rows, err := db.QueryContext(ctx, `
		select n.nspname, c.relname, coalesce(obj_description(c.oid, 'pg_class'), '')
		from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where c.relkind = 'r' and n.nspname not in ('pg_catalog', 'information_schema')
		order by n.nspname, c.relname`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var table tableInfo
		if err := rows.Scan(&table.Schema, &table.Name, &table.Comment); err != nil {
			_ = rows.Close()
			return nil, err
		}
		index[table.Schema+"."+table.Name] = len(tables)
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	if err := loadColumns(ctx, db, tables, index); err != nil {
		return nil, err
	}
	if err := loadIndexes(ctx, db, tables, index); err != nil {
		return nil, err
	}
	if err := loadForeignKeys(ctx, db, tables, index); err != nil {
		return nil, err
	}
	return tables, nil
}

// loadColumns 读取全部列定义。
func loadColumns(ctx context.Context, db *sql.DB, tables []tableInfo, index map[string]int) error {
	rows, err := db.QueryContext(ctx, `
		select table_schema, table_name, column_name, data_type, is_nullable,
		       coalesce(column_default, ''), ordinal_position
		from information_schema.columns
		where table_schema not in ('pg_catalog', 'information_schema')
		order by table_schema, table_name, ordinal_position`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			schema, table  string
			column         columnInfo
			nullable       string
		)
		if err := rows.Scan(&schema, &table, &column.Name, &column.DataType, &nullable,
			&column.Default, &column.Position); err != nil {
			return err
		}
		column.Nullable = nullable
		if position, ok := index[schema+"."+table]; ok {
			tables[position].Columns = append(tables[position].Columns, column)
		}
	}
	return rows.Err()
}

// loadIndexes 读取索引定义（直接取 DDL 原文，免去 int2vector 下标解析）。
func loadIndexes(ctx context.Context, db *sql.DB, tables []tableInfo, index map[string]int) error {
	rows, err := db.QueryContext(ctx, `
		select n.nspname, t.relname, i.relname, x.indisunique, x.indisprimary,
		       pg_get_indexdef(x.indexrelid)
		from pg_index x
		join pg_class t on t.oid = x.indrelid
		join pg_class i on i.oid = x.indexrelid
		join pg_namespace n on n.oid = t.relnamespace
		where n.nspname not in ('pg_catalog', 'information_schema')
		order by n.nspname, t.relname, i.relname`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			schema, table string
			info          indexInfo
		)
		if err := rows.Scan(&schema, &table, &info.Name, &info.Unique, &info.Primary, &info.DefineSQL); err != nil {
			return err
		}
		if position, ok := index[schema+"."+table]; ok {
			tables[position].Indexes = append(tables[position].Indexes, info)
		}
	}
	return rows.Err()
}

// loadForeignKeys 读取外键关系（判断「删表会不会连带影响」的关键信息）。
func loadForeignKeys(ctx context.Context, db *sql.DB, tables []tableInfo, index map[string]int) error {
	rows, err := db.QueryContext(ctx, `
		select tc.table_schema, tc.table_name, tc.constraint_name, kcu.column_name,
		       ccu.table_name, ccu.column_name
		from information_schema.table_constraints tc
		join information_schema.key_column_usage kcu
		  on kcu.constraint_name = tc.constraint_name and kcu.table_schema = tc.table_schema
		join information_schema.constraint_column_usage ccu
		  on ccu.constraint_name = tc.constraint_name and ccu.table_schema = tc.table_schema
		where tc.constraint_type = 'FOREIGN KEY'
		  and tc.table_schema not in ('pg_catalog', 'information_schema')
		order by tc.table_schema, tc.table_name, tc.constraint_name`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			schema, table string
			fk            fkInfo
		)
		if err := rows.Scan(&schema, &table, &fk.Name, &fk.Column, &fk.RefTable, &fk.RefColumn); err != nil {
			return err
		}
		if position, ok := index[schema+"."+table]; ok {
			tables[position].ForeignKeys = append(tables[position].ForeignKeys, fk)
		}
	}
	return rows.Err()
}

// loadRowCounts 统计每张表的行数：先精确计数（带语句超时），超时则退回 pg_class 估算。
func loadRowCounts(ctx context.Context, db *sql.DB, tables []tableInfo) {
	for i := range tables {
		table := &tables[i]
		exact, err := exactCount(ctx, db, table.Schema, table.Name)
		if err == nil {
			table.RowCount = exact
			table.CountExact = true
			continue
		}
		estimate, estimateErr := estimatedCount(ctx, db, table.Schema, table.Name)
		if estimateErr != nil {
			table.CountNote = "行数统计失败: " + err.Error()
			continue
		}
		table.RowCount = estimate
		table.CountNote = "精确计数超过 " + countTimeout.String() + "，以下为 pg_class 估算值"
	}
}

// exactCount 用独立事务 + statement_timeout 做精确计数，超时即失败（不拖垮整轮巡检）。
func exactCount(ctx context.Context, db *sql.DB, schema, table string) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("set local statement_timeout = %d", countTimeout.Milliseconds())); err != nil {
		return 0, err
	}
	var count int64
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`select count(*) from %s`, qualified(schema, table))).Scan(&count); err != nil {
		return 0, err
	}
	return count, tx.Commit()
}

// estimatedCount 读取 pg_class 的行数估算（大表精确计数代价过高时的兜底）。
func estimatedCount(ctx context.Context, db *sql.DB, schema, table string) (int64, error) {
	var estimate int64
	err := db.QueryRowContext(ctx,
		`select coalesce(reltuples, 0)::bigint from pg_class where oid = $1::regclass`,
		schema+"."+table).Scan(&estimate)
	return estimate, err
}

// loadSamples 抽取样本行（配合行数判断「空表」与「有数据但没人看」两种遗留形态）。
func loadSamples(ctx context.Context, db *sql.DB, tables []tableInfo, limit int) {
	if limit <= 0 {
		return
	}
	for i := range tables {
		table := &tables[i]
		if table.RowCount == 0 {
			continue
		}
		query := fmt.Sprintf("select * from %s limit %d", qualified(table.Schema, table.Name), limit)
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			continue
		}
		columns, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			continue
		}
		samples := make([][]string, 0, limit)
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for index := range values {
				pointers[index] = &values[index]
			}
			if err := rows.Scan(pointers...); err != nil {
				break
			}
			cells := make([]string, len(columns))
			for index, value := range values {
				cells[index] = formatCell(value)
			}
			samples = append(samples, cells)
		}
		_ = rows.Close()
		table.Samples = samples
	}
}

// formatCell 把任意列值渲染成单行可读文本。
func formatCell(value any) string {
	switch typed := value.(type) {
	case nil:
		return "NULL"
	case []byte:
		if len(typed) == 0 {
			return ""
		}
		return fmt.Sprintf("<%d 字节>", len(typed))
	case time.Time:
		return typed.Format("2006-01-02 15:04:05")
	case bool:
		return strconv.FormatBool(typed)
	case string:
		return truncate(typed, sampleCellLimit)
	default:
		return truncate(fmt.Sprint(typed), sampleCellLimit)
	}
}

// truncate 截断长文本并转义 Markdown 表格里的竖线与换行。
func truncate(value string, limit int) string {
	value = strings.NewReplacer("\r", " ", "\n", " ", "|", "\\|").Replace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

// qualified 返回带引号的 "schema"."table"，避免保留字与大小写问题。
func qualified(schema, table string) string {
	return `"` + schema + `"."` + table + `"`
}

// tableNames 提取表名列表（用于构造扫描正则）。
func tableNames(tables []tableInfo) []string {
	names := make([]string, 0, len(tables))
	for _, table := range tables {
		names = append(names, table.Name)
	}
	return names
}

// workspaceScan 承担工作区代码扫描。
type workspaceScan struct {
	root       string
	names      []string
	re         *regexp.Regexp
	structRe   *regexp.Regexp
	structOf   map[string]string
	sqlRe      *regexp.Regexp
	ddlRe      *regexp.Regexp
	hits       map[string][]refHit
	counts     map[string]int
	byProject  map[string]map[string]int
	sqlHits    map[string]int
	ddlHits    map[string]int
	extraSkips map[string]bool
	files      int
}

// newWorkspaceScan 构造扫描器：表名联合正则按长度倒序拼装，
// 保证 sa_system_user_role 不会被 sa_system_user 抢先匹配。
func newWorkspaceScan(root string, names []string, extraSkips []string) *workspaceScan {
	sorted := make([]string, len(names))
	copy(sorted, names)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	alternatives := make([]string, 0, len(sorted))
	for _, name := range sorted {
		alternatives = append(alternatives, regexp.QuoteMeta(name))
	}
	pattern := `(?i)\b(` + strings.Join(alternatives, "|") + `)\b`

	// 第二种命中方式：由表名反推 GORM 默认命名下的结构体名。
	structOf := make(map[string]string, 64)
	structIdentifiers := make([]string, 0, 64)
	for _, name := range sorted {
		for _, identifier := range derivedIdentifiers(name) {
			key := strings.ToLower(identifier)
			if _, exists := structOf[key]; exists {
				continue
			}
			structOf[key] = name
			structIdentifiers = append(structIdentifiers, regexp.QuoteMeta(identifier))
		}
	}
	var structRe *regexp.Regexp
	if len(structIdentifiers) > 0 {
		structRe = regexp.MustCompile(`(?i)\b(` + strings.Join(structIdentifiers, "|") + `)\b`)
	}

	skips := make(map[string]bool, len(extraSkips))
	for _, name := range extraSkips {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			skips[trimmed] = true
		}
	}
	return &workspaceScan{
		root:       root,
		names:      sorted,
		re:         regexp.MustCompile(pattern),
		structRe:   structRe,
		structOf:   structOf,
		sqlRe:      regexp.MustCompile(`(?i)\b(?:from|join|into|update|table|truncate)\s+([a-z_][a-z0-9_]*)`),
		ddlRe:      regexp.MustCompile(`(?i)create table (?:if not exists )?["` + "`" + `]?([a-z_][a-z0-9_]*)`),
		hits:       make(map[string][]refHit, len(sorted)),
		counts:     make(map[string]int, len(sorted)),
		byProject:  make(map[string]map[string]int, len(sorted)),
		sqlHits:    make(map[string]int, 64),
		ddlHits:    make(map[string]int, 64),
		extraSkips: skips,
	}
}

// derivedIdentifiers 由表名推导 GORM 默认命名下的 Go 结构体名。
//
// 为什么必须有这一步：Collector（server/internal/state）的模型没有写 TableName()，
// 表名是 GORM 把 CamelCase 转 snake_case 再复数化得到的，代码里不存在字面量表名。
// 只扫字面量会把 outbox_events（事务性发件箱，正在高频读写）误判成「零引用」。
//
// 推导规则（生成所有组合，宁多勿漏，命中后再按表名归并）：
//   - 每段首字母大写；短段（长度 ≤ 3）额外生成全大写变体，覆盖 IP/ID/OS/CPU 这类缩写；
//   - 最后一段去复数：ies → y（histories → History）、s → 空（documents → Document）。
func derivedIdentifiers(table string) []string {
	if !strings.Contains(table, "_") {
		// 单词表名（agents、alerts）推导出的 Agent/Alert 太通用，靠字面量命中已足够。
		return nil
	}
	segments := strings.Split(table, "_")
	variants := make([][]string, 0, len(segments))
	for index, segment := range segments {
		if segment == "" {
			return nil
		}
		forms := []string{title(segment)}
		if len(segment) <= 3 {
			forms = append(forms, strings.ToUpper(segment))
		}
		if index == len(segments)-1 {
			forms = append(forms, singularForms(segment)...)
		}
		variants = append(variants, dedupe(forms))
	}
	combos := []string{""}
	for _, forms := range variants {
		next := make([]string, 0, len(combos)*len(forms))
		for _, prefix := range combos {
			for _, form := range forms {
				next = append(next, prefix+form)
			}
		}
		combos = next
		if len(combos) > 32 {
			combos = combos[:32]
		}
	}
	return combos
}

// singularForms 返回最后一段的单数形式（处理 ies → y 与结尾 s）。
func singularForms(segment string) []string {
	switch {
	case strings.HasSuffix(segment, "ies") && len(segment) > 3:
		base := segment[:len(segment)-3] + "y"
		return []string{title(base)}
	case strings.HasSuffix(segment, "s") && len(segment) > 1:
		base := segment[:len(segment)-1]
		return []string{title(base)}
	default:
		return nil
	}
}

// title 把首字母大写（其余原样保留）。
func title(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

// dedupe 去重并保持顺序。
func dedupe(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// run 遍历工作区；selfPaths 是本次生成的产物路径（必须跳过，否则报告会引用自己，
// 把 db-tables.json / db-tables-report.md 统计成「引用方」，污染全部计数）。
func (s *workspaceScan) run(selfPaths []string) (int, error) {
	skip := make(map[string]bool, len(selfPaths))
	for _, path := range selfPaths {
		if abs, err := filepath.Abs(path); err == nil {
			skip[abs] = true
		}
	}
	err := filepath.WalkDir(s.root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			// 隐藏目录（.git / .playwright-cli / .specstory / .trae …）一律跳过：
			// 它们不是代码，却塞满了页面快照与对话记录，会把引用数抬到几十上百倍。
			if skipDirs[name] || s.extraSkips[name] || (path != s.root && strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !scanExts[strings.ToLower(filepath.Ext(name))] {
			return nil
		}
		// 本工具历次产出的报告/JSON 也按文件名前缀跳过：它们里面全是表名，
		// 而且不一定在本次的 -out/-json 里（上一次的产物仍留在工作区）。
		if strings.HasPrefix(strings.ToLower(name), "db-tables") {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil || info.Size() > maxFileSize {
			return nil
		}
		absPath, _ := filepath.Abs(path)
		if skip[absPath] {
			return nil
		}
		s.scanFile(path)
		return nil
	})
	return s.files, err
}

// scanFile 扫描单个文件：一次正则遍历，按匹配位置换算行号。
func (s *workspaceScan) scanFile(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	content := string(raw)
	s.files++

	relative, relErr := filepath.Rel(s.root, path)
	if relErr != nil {
		relative = path
	}
	relative = filepath.ToSlash(relative)
	project := strings.SplitN(relative, "/", 2)[0]

	lineStarts := lineStartOffsets(content)
	for _, match := range s.re.FindAllStringSubmatchIndex(content, -1) {
		table := strings.ToLower(content[match[2]:match[3]])
		s.record(relative, project, table, kindLiteral, content, lineStarts, match[0])
	}
	if s.structRe != nil {
		for _, match := range s.structRe.FindAllStringSubmatchIndex(content, -1) {
			table := s.structOf[strings.ToLower(content[match[2]:match[3]])]
			if table == "" {
				continue
			}
			s.record(relative, project, table, kindStruct, content, lineStarts, match[0])
		}
	}
	for _, match := range s.sqlRe.FindAllStringSubmatchIndex(content, -1) {
		name := strings.ToLower(content[match[2]:match[3]])
		if sqlKeywordBlacklist[name] {
			continue
		}
		s.sqlHits[name]++
	}
	for _, match := range s.ddlRe.FindAllStringSubmatchIndex(content, -1) {
		s.ddlHits[strings.ToLower(content[match[2]:match[3]])]++
	}
}

// plannedOnlyTables 返回建表脚本里定义过、但当前库里没有的表。
//
// 这是「项目起初规划但没落地」的直接证据：database/init.sql 里那批 Alembic / APScheduler /
// Casbin / 视频平台 / 内容管理等表就属于这一类——规划脚本抄了模板，功能从没做。
func (s *workspaceScan) plannedOnlyTables(tables []tableInfo) []string {
	known := make(map[string]bool, len(tables))
	for _, table := range tables {
		known[table.Name] = true
	}
	planned := make([]string, 0, 32)
	for name := range s.ddlHits {
		if known[name] || strings.HasPrefix(name, "pg_") || sqlKeywordBlacklist[name] {
			continue
		}
		planned = append(planned, name)
	}
	sort.Strings(planned)
	return planned
}

// record 登记一次命中（计数 + 抽样引用点）。
func (s *workspaceScan) record(relative, project, table, kind, content string, lineStarts []int, offset int) {
	if table == "" {
		return
	}
	s.counts[table]++
	if s.byProject[table] == nil {
		s.byProject[table] = make(map[string]int, 4)
	}
	s.byProject[table][project]++
	if len(s.hits[table]) >= maxRefPerTable {
		return
	}
	line := lineOfOffset(lineStarts, offset)
	s.hits[table] = append(s.hits[table], refHit{
		File: relative,
		Line: line,
		Text: truncate(lineText(content, lineStarts, line), 160),
		Kind: kind,
	})
}

// apply 把扫描结果写回表信息。
func (s *workspaceScan) apply(tables []tableInfo) {
	for i := range tables {
		table := &tables[i]
		table.Refs = s.hits[table.Name]
		table.RefCount = s.counts[table.Name]
		table.RefByProject = s.byProject[table.Name]
	}
}

// projectSummary 把「引用构成」渲染成 webserver(12)、server(3) 形式（按次数倒序）。
func projectSummary(counts map[string]int) string {
	if len(counts) == 0 {
		return "—"
	}
	projects := make([]string, 0, len(counts))
	for project := range counts {
		projects = append(projects, project)
	}
	sort.Slice(projects, func(i, j int) bool {
		if counts[projects[i]] != counts[projects[j]] {
			return counts[projects[i]] > counts[projects[j]]
		}
		return projects[i] < projects[j]
	})
	parts := make([]string, 0, len(projects))
	for _, project := range projects {
		parts = append(parts, fmt.Sprintf("%s(%d)", project, counts[project]))
	}
	return strings.Join(parts, "、")
}

// missingTables 返回「代码里引用过、但库里没有」的表名（可能是历史迁移残留或命名写错）。
func (s *workspaceScan) missingTables(tables []tableInfo) []string {
	known := make(map[string]bool, len(tables))
	for _, table := range tables {
		known[table.Name] = true
	}
	missing := make([]string, 0, 16)
	for name := range s.sqlHits {
		if known[name] || strings.HasPrefix(name, "pg_") || sqlKeywordBlacklist[name] {
			continue
		}
		// 只保留出现 2 次以上的：单次命中多半是 CTE 名、别名或注释里的词。
		if s.sqlHits[name] < 2 {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)
	return missing
}

// lineStartOffsets 预计算每行起始字节偏移（把匹配偏移换算成行号）。
func lineStartOffsets(content string) []int {
	offsets := make([]int, 1, 512)
	for index, char := range content {
		if char == '\n' {
			offsets = append(offsets, index+1)
		}
	}
	return offsets
}

// lineOfOffset 二分查找偏移所在的行号（从 1 开始）。
func lineOfOffset(offsets []int, offset int) int {
	return sort.Search(len(offsets), func(i int) bool { return offsets[i] > offset })
}

// lineText 取出某一行的文本。
func lineText(content string, offsets []int, line int) string {
	start := 0
	if line >= 1 && line-1 < len(offsets) {
		start = offsets[line-1]
	}
	end := len(content)
	if line < len(offsets) {
		end = offsets[line]
	}
	if start > len(content) {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(content[start:min(end, len(content))], "\n", ""))
}

// renderMarkdown 生成人读报告。
func renderMarkdown(result report) string {
	var builder strings.Builder
	builder.WriteString("# 数据库表巡检报告\n\n")
	fmt.Fprintf(&builder, "- 生成时间：%s\n", result.GeneratedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&builder, "- 数据源：%s\n", result.DataSource)
	fmt.Fprintf(&builder, "- 工作区：`%s`（扫描源文件 %d 个）\n", result.Workspace, result.ScannedFiles)
	fmt.Fprintf(&builder, "- 表数量：%d 张，数据总行数：%d\n\n", len(result.Tables), result.TotalRows)

	empty, unused := 0, 0
	for _, table := range result.Tables {
		if table.RowCount == 0 {
			empty++
		}
		if table.RefCount == 0 {
			unused++
		}
	}
	builder.WriteString("## 一、结论速览\n\n")
	fmt.Fprintf(&builder, "- 空表：**%d** 张\n", empty)
	fmt.Fprintf(&builder, "- 代码零引用：**%d** 张\n", unused)
	candidates := cleanCandidates(result.Tables)
	fmt.Fprintf(&builder, "- 清理候选（空表 **且** 零引用）：**%d** 张\n\n", len(candidates))

	builder.WriteString("### 清理候选（空表 + 零引用，可直接删）\n\n")
	printCandidateList(&builder, candidates)
	builder.WriteString("### 只被声明、没有读写路径（疑似清理候选：功能规划过但没接上）\n\n")
	printTableNames(&builder, filterTables(result.Tables, func(t tableInfo) bool {
		return t.RowCount == 0 && declarationOnly(t)
	}))
	builder.WriteString("### 空表但确有读写引用（在用但暂无数据，不要删）\n\n")
	printTableNames(&builder, filterTables(result.Tables, func(t tableInfo) bool {
		return t.RowCount == 0 && t.RefCount > 0 && !declarationOnly(t)
	}))
	builder.WriteString("### 有数据但代码零引用（先确认无人再写，再决定）\n\n")
	printTableNames(&builder, filterTables(result.Tables, func(t tableInfo) bool {
		return t.RowCount > 0 && t.RefCount == 0
	}))
	if len(result.SkippedDirs) > 0 {
		fmt.Fprintf(&builder, "\n> 扫描已跳过目录：%s（含隐藏目录）。这些目录里的表名出现不计入引用数。\n\n",
			strings.Join(result.SkippedDirs, "、"))
	}

	builder.WriteString("> 引用统计的口径：既统计表名字面量（GORM 的 `TableName()`、原生 SQL、脚本），\n")
	builder.WriteString("> 也统计由表名推导出的 GORM 默认模型名（如 `outbox_events` ↔ `OutboxEvent`）——\n")
	builder.WriteString("> Collector 侧的模型没写 `TableName()`，只看字面量会把正在高频读写的表误判成零引用。\n\n")

	builder.WriteString("## 二、全部表清单\n\n")
	builder.WriteString("| 表名 | 行数 | 列 | 索引 | 代码引用 | 主要引用方 | 结论 |\n")
	builder.WriteString("| --- | ---: | ---: | ---: | ---: | --- | --- |\n")
	for _, table := range result.Tables {
		fmt.Fprintf(&builder, "| `%s` | %d%s | %d | %d | %d | %s | %s |\n",
			table.Name, table.RowCount, countSuffix(table), len(table.Columns), len(table.Indexes),
			table.RefCount, projectSummary(table.RefByProject), shortVerdict(table))
	}
	builder.WriteString("\n")

	builder.WriteString("## 三、逐表明细\n\n")
	for _, table := range result.Tables {
		renderTableDetail(&builder, table)
	}

	if len(result.PlannedOnlyTables) > 0 || len(result.MissingTables) > 0 {
		builder.WriteString("## 四、规划/引用过但库里没有的表\n\n")
		if len(result.PlannedOnlyTables) > 0 {
			builder.WriteString("### 建表脚本里 CREATE TABLE 过、库里不存在（规划了但没落地）\n\n")
			builder.WriteString("多见于最初抄来的模板脚本（Alembic / APScheduler / Casbin / 内容管理 / 代码生成器等），\n")
			builder.WriteString("对应功能从未实现，因此库里没有对应的表。\n\n")
			for _, name := range result.PlannedOnlyTables {
				fmt.Fprintf(&builder, "- `%s`\n", name)
			}
			builder.WriteString("\n")
		}
		if len(result.MissingTables) > 0 {
			builder.WriteString("### 代码 SQL 里引用过、库里不存在（疑似，需二次核对）\n\n")
			builder.WriteString("这些名字出现在 FROM/JOIN/INTO/UPDATE/TABLE 之后且出现 ≥2 次，\n")
			builder.WriteString("可能是历史迁移残留、命名写错，或由其它服务按需建表。\n\n")
			for _, name := range result.MissingTables {
				fmt.Fprintf(&builder, "- `%s`\n", name)
			}
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

// countSuffix 标出「估算值」。
func countSuffix(table tableInfo) string {
	if table.CountExact {
		return ""
	}
	return "（估算）"
}

// cleanCandidates 返回空表且代码零引用的表。
func cleanCandidates(tables []tableInfo) []tableInfo {
	candidates := make([]tableInfo, 0, 16)
	for _, table := range tables {
		if table.RowCount == 0 && table.RefCount == 0 {
			candidates = append(candidates, table)
		}
	}
	return candidates
}

// printCandidateList 输出候选表及其列数与表注释（列多说明当初设计得挺认真，删除前值得再确认）。
func printCandidateList(builder *strings.Builder, candidates []tableInfo) {
	if len(candidates) == 0 {
		builder.WriteString("没有「空表 + 零引用」的表，无需清理。\n\n")
		return
	}
	for _, table := range candidates {
		comment := ""
		if table.Comment != "" {
			comment = "（表注释：" + table.Comment + "）"
		}
		fmt.Fprintf(builder, "- `%s`：%d 列%s\n", table.Name, len(table.Columns), comment)
	}
	builder.WriteString("\n")
}

// printTableNames 输出一组表的「表名 + 行数 + 引用构成」。
func printTableNames(builder *strings.Builder, tables []tableInfo) {
	if len(tables) == 0 {
		builder.WriteString("（无）\n\n")
		return
	}
	for _, table := range tables {
		fmt.Fprintf(builder, "- `%s`：%d 行，引用 %d 次（%s）\n",
			table.Name, table.RowCount, table.RefCount, projectSummary(table.RefByProject))
	}
	builder.WriteString("\n")
}

// filterTables 按条件筛表。
func filterTables(tables []tableInfo, keep func(tableInfo) bool) []tableInfo {
	out := make([]tableInfo, 0, len(tables))
	for _, table := range tables {
		if keep(table) {
			out = append(out, table)
		}
	}
	return out
}

// shortVerdict 是表清单里的结论列（短标签）。
func shortVerdict(table tableInfo) string {
	switch {
	case table.RowCount == 0 && table.RefCount == 0:
		return "清理候选"
	case table.RowCount == 0 && declarationOnly(table):
		return "仅声明(空)"
	case table.RowCount == 0:
		return "在用(空)"
	case table.RefCount == 0:
		return "有数据/无引用"
	default:
		return "正常"
	}
}

// renderTableDetail 输出单表的列、索引、外键、样本与引用点。
func renderTableDetail(builder *strings.Builder, table tableInfo) {
	fmt.Fprintf(builder, "### `%s`\n\n", table.Name)
	if table.Comment != "" {
		fmt.Fprintf(builder, "- 表注释：%s\n", table.Comment)
	}
	state := "空表"
	if table.RowCount > 0 {
		state = fmt.Sprintf("%d 行", table.RowCount)
	}
	fmt.Fprintf(builder, "- 行数：%s %s\n", state, table.CountNote)
	fmt.Fprintf(builder, "- 代码引用：%d 次（%s）\n", table.RefCount, projectSummary(table.RefByProject))
	fmt.Fprintf(builder, "- 结论：%s\n\n", verdict(table))

	builder.WriteString("| 列 | 类型 | 可空 | 默认值 |\n| --- | --- | --- | --- |\n")
	for _, column := range table.Columns {
		fmt.Fprintf(builder, "| `%s` | %s | %s | %s |\n",
			column.Name, column.DataType, column.Nullable, truncate(column.Default, 60))
	}
	builder.WriteString("\n")

	if len(table.Indexes) > 0 {
		builder.WriteString("索引：\n\n")
		for _, index := range table.Indexes {
			kind := "普通"
			if index.Primary {
				kind = "主键"
			} else if index.Unique {
				kind = "唯一"
			}
			fmt.Fprintf(builder, "- [%s] `%s`：`%s`\n", kind, index.Name, index.DefineSQL)
		}
		builder.WriteString("\n")
	}
	if len(table.ForeignKeys) > 0 {
		builder.WriteString("外键：\n\n")
		for _, fk := range table.ForeignKeys {
			fmt.Fprintf(builder, "- `%s`（`%s` → `%s`.`%s`）\n", fk.Name, fk.Column, fk.RefTable, fk.RefColumn)
		}
		builder.WriteString("\n")
	}
	if len(table.Samples) > 0 {
		builder.WriteString("样本行：\n\n")
		header := make([]string, 0, len(table.Columns))
		for _, column := range table.Columns {
			header = append(header, column.Name)
		}
		fmt.Fprintf(builder, "| %s |\n", strings.Join(header, " | "))
		fmt.Fprintf(builder, "| %s |\n", strings.TrimSuffix(strings.Repeat("--- | ", len(header)), " "))
		for _, row := range table.Samples {
			fmt.Fprintf(builder, "| %s |\n", strings.Join(row, " | "))
		}
		builder.WriteString("\n")
	}
	if len(table.Refs) > 0 {
		builder.WriteString("引用点（`[struct]` 表示命中由表名推导的 GORM 模型名，其余为表名字面量）：\n\n")
		for _, hit := range table.Refs {
			marker := ""
			if hit.Kind == kindStruct {
				marker = "[struct] "
			}
			fmt.Fprintf(builder, "- %s`%s:%d` — %s\n", marker, hit.File, hit.Line, hit.Text)
		}
		builder.WriteString("\n")
	}
}

// verdict 给出单表的处置建议。
func verdict(table tableInfo) string {
	switch {
	case table.RowCount == 0 && table.RefCount == 0:
		return "**清理候选**：空表且代码零引用"
	case table.RowCount == 0 && declarationOnly(table):
		return "**疑似清理候选**：空表，且引用只出现在模型声明与 AutoMigrate/建表语句里，" +
			"没有读写路径（删表前确认对应功能是否已废弃）"
	case table.RowCount == 0:
		return "在用但当前无数据：代码有读写引用，不能删"
	case table.RefCount == 0:
		return "有数据但代码零引用：可能是历史数据表，删除前先确认无人再写"
	default:
		return "正常使用中"
	}
}

// declarationOnly 判断某表的引用是否**全部**只是「声明」：
// 模型结构体定义、AutoMigrate 注册或 CREATE TABLE。
//
// 判定前提是引用点被完整采样（RefCount == len(Refs)）：只看到一部分引用时不能下这个结论。
// 这是「空表 + 有引用」里最有价值的一档——表建了、模型也写了，但没有任何读写代码，
// 典型的「规划了但没接上」，例如 Collector 里只被 AutoMigrate 注册的 agent_ip_histories。
func declarationOnly(table tableInfo) bool {
	if table.RefCount == 0 || table.RefCount != len(table.Refs) {
		return false
	}
	for _, hit := range table.Refs {
		text := hit.Text
		switch {
		case strings.Contains(text, "AutoMigrate"):
		case strings.HasPrefix(text, "type "):
		case strings.Contains(strings.ToUpper(text), "CREATE TABLE"):
		default:
			return false
		}
	}
	return true
}

// printSummary 在终端输出一屏结论。
func printSummary(result report, outPath string) {
	empty, unused := 0, 0
	for _, table := range result.Tables {
		if table.RowCount == 0 {
			empty++
		}
		if table.RefCount == 0 {
			unused++
		}
	}
	candidates := cleanCandidates(result.Tables)
	fmt.Printf("\n=== 巡检完成 ===\n")
	fmt.Printf("表 %d 张：空表 %d、零引用 %d、清理候选 %d、仅规划未落地 %d\n",
		len(result.Tables), empty, unused, len(candidates), len(result.PlannedOnlyTables))
	if len(candidates) > 0 {
		names := make([]string, 0, len(candidates))
		for _, table := range candidates {
			names = append(names, table.Name)
		}
		fmt.Printf("候选：%s\n", strings.Join(names, ", "))
	}
	fmt.Printf("报告已写入：%s\n", outPath)
}

// splitAndTrim 按逗号切分并去除空白项。
func splitAndTrim(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// fail 打印错误并退出。
func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
