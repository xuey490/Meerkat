// Command sqlite2pg 把 webserver 旧的 SQLite 权限库搬迁到 PostgreSQL。
//
// 为什么单独一个模块：主模块已经彻底移除 SQLite 依赖（权限库与监控库都走 PostgreSQL），
// 而搬迁工具必须能读旧库文件。把 SQLite 读取放在独立模块里，主模块的依赖树就不会
// 重新引入 sqlite 驱动。
//
// 用法（在本目录下执行）：
//
//	go run . -pg-dsn "host=localhost port=5432 user=postgres password=xxx dbname=postgres sslmode=disable"
//	go run . -pg-dsn "..." -dry-run          # 只统计行数，不写入
//	go run . -pg-dsn "..." -mode overwrite   # 先清空目标表再写入（覆盖式重跑）
//
// 迁移顺序要求：**先停旧服务 → 跑本工具 → 再启动新版本服务**。
// 目标表结构由新版本服务启动时的迁移流程创建（内嵌 SQL），本工具只搬数据。
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
	_ "modernc.org/sqlite"             // 纯 Go 的 SQLite driver（免 CGO）
)

const (
	// defaultSQLitePath 是旧权限库的默认位置。
	defaultSQLitePath = "data/webserver.db"
	// tablePrefix 限定只搬迁权限与治理表，避免把 SQLite 里的其它表带过去。
	tablePrefix = "sa_system_"
	// batchSize 是每个事务写入的行数：过大的事务会长时间持锁并推高内存占用。
	batchSize = 500
)

// 写入模式。
const (
	// modeInsertOnly 已存在的行跳过（默认）：可重复执行，失败后重跑不会破坏数据。
	modeInsertOnly = "insert-only"
	// modeOverwrite 先清空目标表再写入：用于覆盖式重跑。
	modeOverwrite = "overwrite"
)

// column 描述一个待搬迁的列及其在 PostgreSQL 中的类型。
type column struct {
	// name 列名。
	name string
	// pgType PostgreSQL 的 data_type（取自 information_schema）。
	pgType string
}

func main() {
	sqlitePath := flag.String("sqlite", defaultSQLitePath, "旧 SQLite 数据库文件路径")
	pgDSN := flag.String("pg-dsn", "", "PostgreSQL 连接串（必填）")
	mode := flag.String("mode", modeInsertOnly, "写入模式：insert-only（跳过已存在）| overwrite（先清空再写入）")
	dryRun := flag.Bool("dry-run", false, "只统计行数，不写入")
	check := flag.Bool("check", false, "只做迁移前检查：找出超过 PostgreSQL 列长度上限的数据，不写入")
	tableList := flag.String("tables", "", "可选：只搬迁逗号分隔的表名，默认全部 sa_system_* 表")
	flag.Parse()

	if strings.TrimSpace(*pgDSN) == "" {
		fmt.Fprintln(os.Stderr, "缺少 -pg-dsn 参数")
		flag.Usage()
		os.Exit(2)
	}
	if *mode != modeInsertOnly && *mode != modeOverwrite {
		fmt.Fprintf(os.Stderr, "不支持的 -mode：%s（可选 %s / %s）\n", *mode, modeInsertOnly, modeOverwrite)
		os.Exit(2)
	}
	if err := run(*sqlitePath, *pgDSN, *mode, *dryRun, *check, *tableList); err != nil {
		fmt.Fprintln(os.Stderr, "迁移失败:", err)
		os.Exit(1)
	}
}

// run 执行完整搬迁流程。
func run(sqlitePath, pgDSN, mode string, dryRun, check bool, tableList string) error {
	ctx := context.Background()

	src, err := openSQLite(sqlitePath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	// 先把 WAL 落盘：旧库开着 WAL，主文件里可能缺最后一批记录。
	if err := checkpoint(ctx, src); err != nil {
		return err
	}

	dst, err := sql.Open("pgx", pgDSN)
	if err != nil {
		return fmt.Errorf("连接 PostgreSQL 失败: %w", err)
	}
	defer func() { _ = dst.Close() }()
	if err := dst.PingContext(ctx); err != nil {
		return fmt.Errorf("PostgreSQL 不可用: %w", err)
	}

	tables, err := sourceTables(ctx, src, tableList)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return fmt.Errorf("没有找到可搬迁的表（前缀 %s，文件 %s）", tablePrefix, sqlitePath)
	}

	if check {
		fmt.Printf("检查 %d 张表的列长度（只读，不写入）\n", len(tables))
		return checkLengths(ctx, src, dst, tables)
	}

	fmt.Printf("开始搬迁 %d 张表（mode=%s dry_run=%v）\n", len(tables), mode, dryRun)
	total := 0
	for _, table := range tables {
		count, err := copyTable(ctx, src, dst, table, mode, dryRun)
		if err != nil {
			return fmt.Errorf("表 %s: %w", table, err)
		}
		total += count
		fmt.Printf("  %-28s %6d 行\n", table, count)
	}
	if dryRun {
		fmt.Printf("预演完成：共 %d 张表 %d 行（未写入任何数据）\n", len(tables), total)
		return nil
	}
	fmt.Printf("搬迁完成：共 %d 张表 %d 行\n", len(tables), total)
	fmt.Println("下一步：启动新版本服务，它会执行建表、种子与自增序列校正。")
	return nil
}

// openSQLite 打开旧库（只用于读取）。
func openSQLite(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("找不到 SQLite 数据库文件 %s: %w", path, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败: %w", err)
	}
	// 单连接：搬迁是顺序读，同时避免触发 SQLite 的并发写锁。
	db.SetMaxOpenConns(1)
	return db, nil
}

// checkpoint 把 WAL 中的数据合并回主库文件。
//
// 必须做：旧库开启了 WAL，data/webserver.db-wal 里可能有尚未落盘的数据，
// 直接读主文件会静默漏掉最后一批记录。
func checkpoint(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("wal_checkpoint 失败: %w", err)
	}
	return nil
}

// sourceTables 列出待搬迁的表。
func sourceTables(ctx context.Context, db *sql.DB, tableList string) ([]string, error) {
	if trimmed := strings.TrimSpace(tableList); trimmed != "" {
		names := make([]string, 0, 8)
		for _, name := range strings.Split(trimmed, ",") {
			if value := strings.TrimSpace(name); value != "" {
				names = append(names, value)
			}
		}
		return names, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master
		WHERE type = 'table' AND name LIKE ? ORDER BY name`, tablePrefix+"%")
	if err != nil {
		return nil, fmt.Errorf("查询 SQLite 表清单失败: %w", err)
	}
	defer func() { _ = rows.Close() }()
	names := make([]string, 0, 32)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// copyTable 搬迁一张表，返回写入（或预演统计）的行数。
func copyTable(ctx context.Context, src, dst *sql.DB, table, mode string, dryRun bool) (int, error) {
	columns, err := planColumns(ctx, src, dst, table)
	if err != nil {
		return 0, err
	}
	if mode == modeOverwrite && !dryRun {
		if _, err := dst.ExecContext(ctx, "TRUNCATE TABLE "+quoteIdent(table)); err != nil {
			return 0, fmt.Errorf("清空目标表失败: %w", err)
		}
	}

	rows, err := src.QueryContext(ctx,
		"SELECT "+joinQuoted(columns, false)+" FROM "+quoteIdent(table))
	if err != nil {
		return 0, fmt.Errorf("读取源数据失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	raw := make([]any, len(columns))
	rawPtrs := make([]any, len(columns))
	for i := range raw {
		rawPtrs[i] = &raw[i]
	}

	count := 0
	var (
		tx       *sql.Tx
		stmt     *sql.Stmt
		inTxRows int
	)
	commit := func() error {
		if tx == nil {
			return nil
		}
		if stmt != nil {
			_ = stmt.Close()
			stmt = nil
		}
		err := tx.Commit()
		tx = nil
		inTxRows = 0
		return err
	}

	for rows.Next() {
		if err := rows.Scan(rawPtrs...); err != nil {
			_ = commit()
			return count, fmt.Errorf("扫描源数据失败: %w", err)
		}
		count++
		if dryRun {
			continue
		}
		if tx == nil {
			tx, err = dst.BeginTx(ctx, nil)
			if err != nil {
				return count, fmt.Errorf("开启事务失败: %w", err)
			}
			stmt, err = tx.PrepareContext(ctx, insertStatement(table, columns))
			if err != nil {
				_ = tx.Rollback()
				tx = nil
				return count, fmt.Errorf("准备插入语句失败: %w", err)
			}
		}
		values := make([]any, len(columns))
		for i, col := range columns {
			converted, err := convertValue(raw[i], col.pgType)
			if err != nil {
				_ = commit()
				return count, fmt.Errorf("第 %d 行列 %s: %w", count, col.name, err)
			}
			values[i] = converted
		}
		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			_ = commit()
			return count, fmt.Errorf("写入第 %d 行失败: %w", count, err)
		}
		inTxRows++
		if inTxRows >= batchSize {
			if err := commit(); err != nil {
				return count, fmt.Errorf("提交事务失败: %w", err)
			}
		}
	}
	if err := rows.Err(); err != nil {
		_ = commit()
		return count, err
	}
	if err := commit(); err != nil {
		return count, fmt.Errorf("提交事务失败: %w", err)
	}
	return count, nil
}

// planColumns 计算源表与目标表的共同列。
//
// 取交集而不是直接用源表列：目标库可能因为版本更新多了扩展列（那些列有默认值），
// 也可能少了尚未迁移的列；只搬两边都存在的列最稳。
func planColumns(ctx context.Context, src, dst *sql.DB, table string) ([]column, error) {
	sourceCols, err := sourceColumns(ctx, src, table)
	if err != nil {
		return nil, err
	}
	targetTypes, err := targetColumnTypes(ctx, dst, table)
	if err != nil {
		return nil, err
	}
	if len(targetTypes) == 0 {
		return nil, fmt.Errorf("目标库中不存在表 %s，请先启动一次新版本服务完成建表", table)
	}
	shared := make([]column, 0, len(sourceCols))
	for _, name := range sourceCols {
		pgType, ok := targetTypes[name]
		if !ok {
			continue
		}
		shared = append(shared, column{name: name, pgType: pgType})
	}
	if len(shared) == 0 {
		return nil, fmt.Errorf("表 %s 与目标库没有任何同名列", table)
	}
	return shared, nil
}

// sourceColumns 读取 SQLite 表的列名（按定义顺序）。
func sourceColumns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+quoteIdent(table)+")")
	if err != nil {
		return nil, fmt.Errorf("读取表 %s 列信息失败: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	names := make([]string, 0, 16)
	for rows.Next() {
		var (
			cid, notNull, primaryKey int
			name, columnType         string
			defaultValue             any
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// targetColumnTypes 读取 PostgreSQL 表的列类型（列名 → data_type）。
func targetColumnTypes(ctx context.Context, db *sql.DB, table string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1`, table)
	if err != nil {
		return nil, fmt.Errorf("读取目标表 %s 列类型失败: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	types := make(map[string]string, 16)
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return nil, err
		}
		types[name] = dataType
	}
	return types, rows.Err()
}

// insertStatement 生成插入语句。
//
// 统一带 ON CONFLICT DO NOTHING：insert-only 模式下它就是「已存在则跳过」，
// 保证工具可重复执行；overwrite 模式下目标表已清空，这个子句不会触发。
func insertStatement(table string, columns []column) string {
	names := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, col := range columns {
		names[i] = quoteIdent(col.name)
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING",
		quoteIdent(table), strings.Join(names, ", "), strings.Join(placeholders, ", "))
}

// convertValue 把 SQLite 读出的值转换为目标列类型对应的 Go 值。
//
// 为什么必须显式转换：pgx 在扩展协议下会按目标列类型编码参数，
// 把 SQLite 的 0/1（int64）直接塞进 PostgreSQL 的 boolean 列会因类型不匹配失败。
//
// 参数 Parameters:
//   - value (any): SQLite 驱动返回的值（int64 / float64 / []byte / string / time.Time / nil）。
//   - pgType (string): 目标列的 information_schema.data_type。
//
// 返回 Returns:
//   - converted (any): 可直接传给 pgx 的值。
//   - err (error): 解析失败时返回非 nil（宁可中止也不要静默写入错误数据）。
func convertValue(value any, pgType string) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case int64:
		switch pgType {
		case "boolean":
			return typed != 0, nil
		case "smallint", "integer", "bigint":
			return typed, nil
		case "numeric", "real", "double precision":
			return float64(typed), nil
		default:
			return strconv.FormatInt(typed, 10), nil
		}
	case float64:
		switch pgType {
		case "numeric", "real", "double precision":
			return typed, nil
		default:
			return strconv.FormatFloat(typed, 'f', -1, 64), nil
		}
	case bool:
		return typed, nil
	case time.Time:
		return typed, nil
	}

	text := ""
	switch typed := value.(type) {
	case []byte:
		text = string(typed)
	case string:
		text = typed
	default:
		text = fmt.Sprint(typed)
	}
	if text == "" && pgType != "text" && !strings.Contains(pgType, "char") {
		// SQLite 的空字符串与 NULL 在部分列上语义不同，这里只在明确是文本列时保留空串。
		return nil, nil
	}
	switch pgType {
	case "boolean":
		return text == "1" || strings.EqualFold(text, "true") || strings.EqualFold(text, "t"), nil
	case "smallint", "integer", "bigint":
		parsed, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("解析整数 %q 失败: %w", text, err)
		}
		return parsed, nil
	case "numeric", "real", "double precision":
		parsed, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return nil, fmt.Errorf("解析数值 %q 失败: %w", text, err)
		}
		return parsed, nil
	case "timestamp without time zone", "timestamp with time zone", "date":
		return parseSourceTime(text)
	case "bytea":
		return []byte(text), nil
	default:
		return text, nil
	}
}

// sourceTimeLayouts 是 SQLite 里时间列的常见存储格式。
var sourceTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// parseSourceTime 解析 SQLite 的时间文本。
//
// 参数 Parameters:
//   - text (string): 时间文本。
//
// 返回 Returns:
//   - parsed (any): time.Time；空串返回 nil。
//   - err (error): 全部格式都解析失败时返回非 nil。
func parseSourceTime(text string) (any, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, nil
	}
	for _, layout := range sourceTimeLayouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed, nil
		}
	}
	return nil, fmt.Errorf("无法解析时间 %q", trimmed)
}

// joinQuoted 把列名拼成逗号分隔的引用列表。
func joinQuoted(columns []column, _ bool) string {
	names := make([]string, len(columns))
	for i, col := range columns {
		names[i] = quoteIdent(col.name)
	}
	return strings.Join(names, ", ")
}

// quoteIdent 引用 SQL 标识符，避免关键字（如 key）与大小写问题。
func quoteIdent(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// checkLengths 找出超过 PostgreSQL 列长度上限的数据。
//
// 为什么需要：SQLite 不强制 VARCHAR(n) 的长度，超长数据能正常写进去；
// PostgreSQL 严格强制，超长会直接报 22001（value too long）。迁移前先查一遍，
// 避免搬到一半失败、留下半迁移状态。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - src (*sql.DB): 旧 SQLite 库。
//   - dst (*sql.DB): 目标 PostgreSQL 库。
//   - tables ([]string): 待检查的表。
//
// 返回 Returns:
//   - err (error): 检查过程本身出错时返回非 nil；发现超长数据只报告、不返回错误。
func checkLengths(ctx context.Context, src, dst *sql.DB, tables []string) error {
	problems := 0
	for _, table := range tables {
		limits, err := targetColumnLimits(ctx, dst, table)
		if err != nil {
			return err
		}
		if len(limits) == 0 {
			return fmt.Errorf("目标库中不存在表 %s，请先启动一次新版本服务完成建表", table)
		}
		for name, limit := range limits {
			if limit <= 0 {
				continue
			}
			var maxLength sql.NullInt64
			query := fmt.Sprintf("SELECT MAX(LENGTH(%s)) FROM %s", quoteIdent(name), quoteIdent(table))
			if err := src.QueryRowContext(ctx, query).Scan(&maxLength); err != nil {
				return fmt.Errorf("统计 %s.%s 长度失败: %w", table, name, err)
			}
			if maxLength.Valid && maxLength.Int64 > limit {
				fmt.Printf("  ! %s.%s：实际最长 %d 字符，PostgreSQL 只允许 %d\n",
					table, name, maxLength.Int64, limit)
				problems++
			}
		}
	}
	if problems == 0 {
		fmt.Println("检查通过：没有超出 PostgreSQL 列长度上限的数据。")
		return nil
	}
	fmt.Printf("发现 %d 处超长列：请放宽上表列出的 PostgreSQL 长度限制（或确认可截断）后再搬迁。\n", problems)
	return nil
}

// targetColumnLimits 返回 PostgreSQL 表各字符列的长度上限（0 表示不限长）。
func targetColumnLimits(ctx context.Context, db *sql.DB, table string) (map[string]int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT column_name, COALESCE(character_maximum_length, 0)
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1`, table)
	if err != nil {
		return nil, fmt.Errorf("读取目标表 %s 列长度失败: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	limits := make(map[string]int64, 16)
	for rows.Next() {
		var name string
		var limit int64
		if err := rows.Scan(&name, &limit); err != nil {
			return nil, err
		}
		limits[name] = limit
	}
	return limits, rows.Err()
}
