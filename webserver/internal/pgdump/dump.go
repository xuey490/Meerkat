// Package pgdump 生成「可被 PostgreSQL 原样导入」的纯 SQL 备份。
//
// 为什么不直接调用 pg_dump：webserver 可能跑在没有安装 PostgreSQL 客户端工具的
// 容器里，计划任务的备份因此只能靠数据库连接自己拼 SQL。本包取定义的来源与
// pg_dump 一致 —— 用 pg_catalog 的 format_type / pg_get_expr / pg_get_constraintdef /
// pg_get_indexdef 拿 PostgreSQL 自己渲染的 DDL 原文，而不是从 information_schema
// 拼装类型与约束，因此不会再出现下面这些「拼出来但导不进去」的问题：
//
//  1. 类型名被拼成 MySQL 风格：information_schema 对 bigint/integer/smallint 会给
//     numeric_precision = 64/32/16，旧实现据此生成 `bigint(64)`，PostgreSQL 直接语法报错。
//  2. 自增列丢失：identity 列的 column_default 是 NULL，旧实现只输出 `integer NOT NULL`，
//     导入后新增数据既没有默认值也没有序列，主键必然冲突。
//  3. 索引、唯一约束、外键、序列全都没导出（旧产物只有建表 + INSERT）。
//  4. 序列值没重置：导入后第一条 INSERT 从 1 开始，撞上已存在的主键。
//
// 用法上只需给一个待导出库的连接与一个 io.Writer；内部的实现细节（快照事务、
// search_path 处理、字面量转义）都在本包内闭环。
package pgdump

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"
)

// defaultBatchSize 是单条 INSERT 默认包含的最大行数。
const defaultBatchSize = 200

// userSchemaCondition 是「排除系统 schema」的 SQL 条件（要求 pg_namespace 别名为 n）。
//
// 除 pg_catalog / information_schema 外还必须排掉 pg_toast：TOAST 表与其索引挂在
// pg_toast schema 下，但它们同样会出现在 pg_class 里，漏排会让备份多出一堆
// pg_toast_xxxxx 对象（52 张表会多出 71 个 TOAST 表与 71 个索引）。
const userSchemaCondition = `n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg\_%'`

// Options 控制导出范围与格式。
type Options struct {
	// Schemas 限定导出的 schema；为空时导出库里全部用户 schema。
	Schemas []string
	// ExcludeTables 要跳过的表，可写「表名」或「schema.表名」。
	ExcludeTables []string
	// ExcludeData 只导出结构、不导出数据的表（日志/事件类大表），写法同 ExcludeTables。
	ExcludeData []string
	// IncludeData 为 false 时全部表都只导结构。
	IncludeData bool
	// BatchSize 单条 INSERT 的最大行数；<=0 时取 200。
	BatchSize int
	// Database 数据库名，仅用于文件头注释。
	Database string
	// Generator 生成者标识，仅用于文件头注释。
	Generator string
	// Now 导出时间，零值时取当前时间。
	Now time.Time
}

// Stats 是一次导出的统计结果。
type Stats struct {
	// Schemas 导出的 schema 数。
	Schemas int
	// Tables 导出的表数。
	Tables int
	// Sequences 导出的序列数（含 identity 序列）。
	Sequences int
	// Indexes 导出的索引数（不含主键/唯一约束自带的索引）。
	Indexes int
	// Constraints 导出的约束数（主键/唯一/检查/外键）。
	Constraints int
	// Rows 导出的数据行数。
	Rows int64
}

// normalized 补齐零值选项。
func (o Options) normalized() Options {
	if o.BatchSize <= 0 {
		o.BatchSize = defaultBatchSize
	}
	if strings.TrimSpace(o.Generator) == "" {
		o.Generator = "webserver internal/pgdump"
	}
	return o
}

// sequence 是一个序列的定义（含它的属主列，用于生成 setval 与 OWNED BY）。
type sequence struct {
	// schema 序列所在 schema。
	schema string
	// name 序列名。
	name string
	// dataType 序列的数据类型（bigint / integer / smallint）。
	dataType string
	// start 起始值。
	start int64
	// increment 步长。
	increment int64
	// min 最小值。
	min int64
	// max 最大值。
	max int64
	// cache 预分配个数。
	cache int64
	// cycle 是否循环。
	cycle bool
	// ownerSchema 属主表所在 schema；无属主时为空。
	ownerSchema string
	// ownerTable 属主表名；无属主时为空。
	ownerTable string
	// ownerColumn 属主列名；无属主时为空。
	ownerColumn string
	// identity 为 true 表示这是 identity 列的序列（由 ADD GENERATED 隐式创建，不能单独 CREATE）。
	identity bool
	// always 为 true 表示 GENERATED ALWAYS（插入时必须 OVERRIDING SYSTEM VALUE）。
	always bool
	// lastValue 无属主序列的最后取值（仅用于 setval）。
	lastValue int64
}

// qualified 返回序列在 SQL 中的全限定名。
func (s sequence) qualified() string { return qualified(s.schema, s.name) }

// column 是表的一列。
type column struct {
	// name 列名。
	name string
	// typeName format_type 渲染出的类型名（如 character varying(64)）。
	typeName string
	// defaultExpr 默认值表达式；无默认值或为 identity 列时为空。
	defaultExpr string
	// collation 非默认排序规则的全限定名；使用类型默认排序规则时为空。
	collation string
	// notNull 是否 NOT NULL。
	notNull bool
	// identity 取值 d（BY DEFAULT）/ a（ALWAYS）/ 空字符串。
	identity string
	// generated 取值为 s 时表示 STORED 生成列（不导出数据）。
	generated string
}

// constraint 是一条表约束。
type constraint struct {
	// name 约束名。
	name string
	// contype 约束类型：p 主键 / u 唯一 / c 检查 / x 排他 / f 外键。
	contype string
	// definition pg_get_constraintdef 渲染出的定义原文。
	definition string
}

// indexDef 是一个索引的 DDL 原文。
type indexDef struct {
	// name 索引名。
	name string
	// definition pg_get_indexdef 渲染出的 CREATE INDEX 原文。
	definition string
}

// table 是一张表及其全部附属定义。
type table struct {
	// schema 表所在 schema。
	schema string
	// name 表名。
	name string
	// oid pg_class.oid。
	oid int64
	// columns 列定义（按 attnum 顺序）。
	columns []column
	// constraints 立即创建的约束（主键/唯一/检查/排他）。
	constraints []constraint
	// foreignKeys 延后到数据之后创建的外键。
	foreignKeys []constraint
	// indexes 非约束索引。
	indexes []indexDef
	// skipData 是否只导结构。
	skipData bool
}

// qualified 返回表在 SQL 中的全限定名。
func (t table) qualified() string { return qualified(t.schema, t.name) }

// dataColumns 返回需要导出数据的列（STORED 生成列不能显式插入）。
func (t table) dataColumns() []column {
	columns := make([]column, 0, len(t.columns))
	for _, c := range t.columns {
		if c.generated == "s" {
			continue
		}
		columns = append(columns, c)
	}
	return columns
}

// hasAlwaysIdentity 判断表是否含 GENERATED ALWAYS 的 identity 列。
//
// 含此类列时必须给 INSERT 加 OVERRIDING SYSTEM VALUE，否则显式写入主键会被拒绝。
func (t table) hasAlwaysIdentity() bool {
	for _, c := range t.columns {
		if c.alwaysIdentity() {
			return true
		}
	}
	return false
}

// alwaysIdentity 判断该列是否为 GENERATED ALWAYS 的 identity 列。
func (c column) alwaysIdentity() bool { return c.identity == "a" }

// catalog 是一次导出所需的全部元信息。
type catalog struct {
	// schemas 需要导出的 schema 名（排序后）。
	schemas []string
	// sequences 序列定义（排序后）。
	sequences []sequence
	// tables 表定义（排序后）。
	tables []table
}

// identitySequence 按「schema.表.列」索引 identity 序列，供建表语句渲染 GENERATED 子句。
func (c catalog) identitySequence(schema, table, column string) *sequence {
	for i := range c.sequences {
		seq := &c.sequences[i]
		if !seq.identity {
			continue
		}
		if seq.ownerSchema == schema && seq.ownerTable == table && seq.ownerColumn == column {
			return seq
		}
	}
	return nil
}

// ownsSequence 判断序列是否由某列拥有（identity 或 serial 型默认值）。
func (s sequence) ownsSequence() bool {
	return s.ownerTable != "" && s.ownerColumn != ""
}

// Dump 把 db 的内容导出为可被 psql 直接执行的纯 SQL，写入 out。
//
// 导出过程在「只读 + REPEATABLE READ」事务中进行，且与 pg_dump 一样把会话的
// search_path 置空后再读取 DDL 定义，因此：
//   - 读到的是全限定名的 DDL（nextval('public.x_id_seq') 而不是裸名），
//     文件在 search_path 为空的目标库上也能正确执行；
//   - 结构与数据来自同一快照，即使导出期间应用仍在写入也不会前后不一致。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - db (*sql.DB): 源库连接池。
//   - out (io.Writer): 输出目标（文件、缓冲等）。
//   - opts (Options): 导出选项。
//
// 返回 Returns:
//   - stats (Stats): 导出统计。
//   - err (error): 任一阶段失败时返回非 nil；已写出的内容可能不完整，调用方应丢弃。
func Dump(ctx context.Context, db *sql.DB, out io.Writer, opts Options) (Stats, error) {
	var stats Stats
	if db == nil {
		return stats, fmt.Errorf("pgdump: 数据库连接为 nil")
	}
	if out == nil {
		return stats, fmt.Errorf("pgdump: 输出目标为 nil")
	}
	opts = opts.normalized()

	conn, err := db.Conn(ctx)
	if err != nil {
		return stats, fmt.Errorf("pgdump: 获取连接失败: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// 快照 + search_path 清空都必须落在同一条连接上，退出时一并还原。
	cleanup, err := beginSnapshot(ctx, conn)
	if err != nil {
		return stats, err
	}
	defer cleanup()

	cat, err := loadCatalog(ctx, conn, opts)
	if err != nil {
		return stats, err
	}
	if len(cat.tables) == 0 && len(cat.sequences) == 0 {
		return stats, fmt.Errorf("pgdump: 目标 schema 下没有可导出的表或序列")
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	w := &sqlWriter{w: bufio.NewWriterSize(out, 1<<16)}
	d := newDumper(w, cat, opts)

	// 顺序即 pg_dump 的顺序：schema → 清理 → 序列 → 建表/约束 → 数据 → 索引/外键/setval。
	d.writeHeader(now)
	d.writeSchemas()
	d.writeDrops()
	d.writeSequences()
	d.writeTables()
	if opts.IncludeData {
		if err := d.writeData(ctx, conn); err != nil {
			return stats, err
		}
	}
	d.writePostData()
	d.writeFooter()

	if err := w.w.Flush(); err != nil {
		return stats, fmt.Errorf("pgdump: 刷新输出失败: %w", err)
	}
	if w.err != nil {
		return stats, fmt.Errorf("pgdump: 写出备份失败: %w", w.err)
	}
	return d.stats, nil
}

// beginSnapshot 在连接上开启只读快照事务并清空 search_path。
//
// search_path 置空是为了让 pg_get_expr / pg_get_constraintdef / pg_get_indexdef
// 输出全限定的对象名 —— 这是 pg_dump 的做法，也是本文件能在空 search_path 下
// 正确执行的前提（例如 serial 列的默认值会被渲染成 nextval('public.x_id_seq')）。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - conn (*sql.Conn): 独占连接。
//
// 返回 Returns:
//   - cleanup (func()): 回滚事务并还原 search_path（可安全执行多次）。
//   - err (error): 开启失败时返回非 nil。
func beginSnapshot(ctx context.Context, conn *sql.Conn) (func(), error) {
	var savedSearchPath string
	if err := conn.QueryRowContext(ctx, `SELECT current_setting('search_path')`).Scan(&savedSearchPath); err != nil {
		return func() {}, fmt.Errorf("pgdump: 读取 search_path 失败: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT set_config('search_path', '', false)`); err != nil {
		return func() {}, fmt.Errorf("pgdump: 清空 search_path 失败: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY`); err != nil {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), `SELECT set_config('search_path', $1, false)`, savedSearchPath)
		return func() {}, fmt.Errorf("pgdump: 开启快照事务失败: %w", err)
	}
	// 连接会回到连接池继续被业务复用，因此事务与会话状态都必须还原，
	// 否则后续业务查询会落在一个只读事务或空 search_path 里。
	return func() {
		bg := context.WithoutCancel(ctx)
		_, _ = conn.ExecContext(bg, `ROLLBACK`)
		_, _ = conn.ExecContext(bg, `SELECT set_config('search_path', $1, false)`, savedSearchPath)
	}, nil
}

// loadCatalog 读取本次导出需要的全部元信息。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - conn (*sql.Conn): 独占连接（已置于只读快照事务中）。
//   - opts (Options): 导出选项。
//
// 返回 Returns:
//   - cat (catalog): 元信息。
//   - err (error): 读取失败时返回非 nil。
func loadCatalog(ctx context.Context, conn *sql.Conn, opts Options) (catalog, error) {
	cat := catalog{}
	schemas, err := loadSchemas(ctx, conn, opts)
	if err != nil {
		return cat, err
	}
	cat.schemas = schemas
	sequences, err := loadSequences(ctx, conn, schemas)
	if err != nil {
		return cat, err
	}
	cat.sequences = sequences
	for i := range cat.sequences {
		if !cat.sequences[i].ownsSequence() {
			value, err := loadSequenceValue(ctx, conn, cat.sequences[i])
			if err != nil {
				return cat, err
			}
			cat.sequences[i].lastValue = value
		}
	}
	tables, err := loadTables(ctx, conn, schemas, opts)
	if err != nil {
		return cat, err
	}
	cat.tables = tables
	return cat, nil
}

// loadSchemas 读取要导出的 schema 清单。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - conn (*sql.Conn): 独占连接。
//   - opts (Options): 导出选项（Schemas 指定时只取指定值）。
//
// 返回 Returns:
//   - schemas ([]string): schema 名，按名称排序。
//   - err (error): 查询失败时返回非 nil。
func loadSchemas(ctx context.Context, conn *sql.Conn, opts Options) ([]string, error) {
	if len(opts.Schemas) > 0 {
		return append([]string(nil), opts.Schemas...), nil
	}
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT n.nspname
		FROM pg_namespace n
		WHERE %s
		  AND EXISTS (
		    SELECT 1 FROM pg_class c
		    WHERE c.relnamespace = n.oid AND c.relkind IN ('r', 'S') AND NOT c.relispartition
		  )
		ORDER BY n.nspname`, userSchemaCondition))
	if err != nil {
		return nil, fmt.Errorf("pgdump: 查询 schema 失败: %w", err)
	}
	defer rows.Close()
	var schemas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("pgdump: 读取 schema 失败: %w", err)
		}
		schemas = append(schemas, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgdump: 遍历 schema 失败: %w", err)
	}
	return schemas, nil
}

// schemaListLiteral 把 schema 清单渲染成 SQL 的 IN 列表。
//
// 值经字符串字面量转义，因此即使 schema 名来自配置也不会带出注入风险。
//
// 参数 Parameters:
//   - schemas ([]string): schema 名清单。
//
// 返回 Returns:
//   - literal (string): 形如 'public', 'meerkat' 的片段。
func schemaListLiteral(schemas []string) string {
	quoted := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		quoted = append(quoted, quoteStringLiteral(schema))
	}
	return strings.Join(quoted, ", ")
}

// loadSequences 读取指定 schema 下的全部序列及其属主列。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - conn (*sql.Conn): 独占连接。
//   - schemas ([]string): 已选定的 schema（为空表示不导出序列）。
//
// 返回 Returns:
//   - sequences ([]sequence): 序列定义，按 schema、名称排序。
//   - err (error): 查询失败时返回非 nil。
func loadSequences(ctx context.Context, conn *sql.Conn, schemas []string) ([]sequence, error) {
	if len(schemas) == 0 {
		return nil, nil
	}
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT n.nspname, c.relname, format_type(s.seqtypid, NULL),
		       s.seqstart, s.seqincrement, s.seqmin, s.seqmax, s.seqcache, s.seqcycle,
		       COALESCE(tn.nspname, ''), COALESCE(tc.relname, ''), COALESCE(a.attname, ''),
		       COALESCE(d.deptype::text, ''), COALESCE(a.attidentity::text, '')
		FROM pg_sequence s
		JOIN pg_class c ON c.oid = s.seqrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_depend d
		       ON d.classid = 'pg_class'::regclass AND d.objid = c.oid
		      AND d.refclassid = 'pg_class'::regclass AND d.deptype IN ('a', 'i')
		LEFT JOIN pg_class tc ON tc.oid = d.refobjid
		LEFT JOIN pg_namespace tn ON tn.oid = tc.relnamespace
		LEFT JOIN pg_attribute a ON a.attrelid = d.refobjid AND a.attnum = d.refobjsubid
		WHERE %s AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname`, userSchemaCondition, schemaListLiteral(schemas)))
	if err != nil {
		return nil, fmt.Errorf("pgdump: 查询序列失败: %w", err)
	}
	defer rows.Close()
	var sequences []sequence
	for rows.Next() {
		var (
			item        sequence
			deptype     string
			attidentity string
		)
		if err := rows.Scan(&item.schema, &item.name, &item.dataType,
			&item.start, &item.increment, &item.min, &item.max, &item.cache, &item.cycle,
			&item.ownerSchema, &item.ownerTable, &item.ownerColumn, &deptype, &attidentity); err != nil {
			return nil, fmt.Errorf("pgdump: 读取序列失败: %w", err)
		}
		// deptype=i 是 identity 列的序列（必须由 ADD GENERATED 创建）；
		// attidentity=a 表示该列是 GENERATED ALWAYS，插入时必须带 OVERRIDING SYSTEM VALUE。
		item.identity = deptype == "i"
		item.always = attidentity == "a"
		sequences = append(sequences, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgdump: 遍历序列失败: %w", err)
	}
	return sequences, nil
}

// loadSequenceValue 读取无属主序列的当前值（有属主序列的 setval 按属主列的 MAX 计算）。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - conn (*sql.Conn): 独占连接。
//   - seq (sequence): 目标序列。
//
// 返回 Returns:
//   - value (int64): 序列当前值。
//   - err (error): 读取失败时返回非 nil。
func loadSequenceValue(ctx context.Context, conn *sql.Conn, seq sequence) (int64, error) {
	var value int64
	if err := conn.QueryRowContext(ctx, fmt.Sprintf(`SELECT last_value FROM %s`, seq.qualified())).Scan(&value); err != nil {
		return 0, fmt.Errorf("pgdump: 读取序列 %s 当前值失败: %w", seq.qualified(), err)
	}
	return value, nil
}

// loadTables 读取指定 schema 下全部普通表（含列、约束与索引）。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - conn (*sql.Conn): 独占连接。
//   - schemas ([]string): 已选定的 schema。
//   - opts (Options): 导出选项（过滤表、决定是否导数据）。
//
// 返回 Returns:
//   - tables ([]table): 表定义，按 schema、名称排序。
//   - err (error): 查询失败时返回非 nil。
func loadTables(ctx context.Context, conn *sql.Conn, schemas []string, opts Options) ([]table, error) {
	if len(schemas) == 0 {
		return nil, nil
	}
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT n.nspname, c.relname, c.oid
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'r' AND NOT c.relispartition AND %s AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname`, userSchemaCondition, schemaListLiteral(schemas)))
	if err != nil {
		return nil, fmt.Errorf("pgdump: 查询表失败: %w", err)
	}
	type rawTable struct {
		schema string
		name   string
		oid    int64
	}
	var raw []rawTable
	for rows.Next() {
		var item rawTable
		if err := rows.Scan(&item.schema, &item.name, &item.oid); err != nil {
			rows.Close()
			return nil, fmt.Errorf("pgdump: 读取表失败: %w", err)
		}
		raw = append(raw, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("pgdump: 遍历表失败: %w", err)
	}
	rows.Close()

	tables := make([]table, 0, len(raw))
	for _, item := range raw {
		if opts.excluded(item.schema, item.name, opts.ExcludeTables) {
			continue
		}
		item2 := table{
			schema:   item.schema,
			name:     item.name,
			oid:      item.oid,
			skipData: !opts.IncludeData || opts.excluded(item.schema, item.name, opts.ExcludeData),
		}
		if item2.columns, err = loadColumns(ctx, conn, item.oid); err != nil {
			return nil, err
		}
		if item2.constraints, item2.foreignKeys, err = loadConstraints(ctx, conn, item.oid); err != nil {
			return nil, err
		}
		if item2.indexes, err = loadIndexes(ctx, conn, item.oid); err != nil {
			return nil, err
		}
		tables = append(tables, item2)
	}
	return tables, nil
}

// excluded 判断表是否命中过滤清单（支持「表名」与「schema.表名」两种写法）。
func (o Options) excluded(schema, name string, list []string) bool {
	full := schema + "." + name
	for _, item := range list {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if item == name || item == full {
			return true
		}
	}
	return false
}

// loadColumns 读取一张表的列定义。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - conn (*sql.Conn): 独占连接。
//   - oid (int64): 表的 pg_class.oid。
//
// 返回 Returns:
//   - columns ([]column): 列定义，按列顺序。
//   - err (error): 查询失败时返回非 nil。
func loadColumns(ctx context.Context, conn *sql.Conn, oid int64) ([]column, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT a.attname,
		       format_type(a.atttypid, a.atttypmod),
		       a.attnotnull,
		       a.attidentity::text,
		       a.attgenerated::text,
		       COALESCE(pg_get_expr(ad.adbin, ad.adrelid), ''),
		       CASE WHEN a.attcollation <> 0 AND a.attcollation <> t.typcollation
		            THEN COALESCE(cn.nspname || '.' || co.collname, co.collname)
		            ELSE '' END
		FROM pg_attribute a
		JOIN pg_type t ON t.oid = a.atttypid
		LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
		LEFT JOIN pg_collation co ON co.oid = a.attcollation
		LEFT JOIN pg_namespace cn ON cn.oid = co.collnamespace
		WHERE a.attrelid = $1 AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`, oid)
	if err != nil {
		return nil, fmt.Errorf("pgdump: 查询列定义失败: %w", err)
	}
	defer rows.Close()
	var columns []column
	for rows.Next() {
		var item column
		if err := rows.Scan(&item.name, &item.typeName, &item.notNull, &item.identity,
			&item.generated, &item.defaultExpr, &item.collation); err != nil {
			return nil, fmt.Errorf("pgdump: 读取列定义失败: %w", err)
		}
		columns = append(columns, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgdump: 遍历列定义失败: %w", err)
	}
	return columns, nil
}

// loadConstraints 读取一张表的约束，并把外键单独摘出来（外键必须等数据写完再建）。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - conn (*sql.Conn): 独占连接。
//   - oid (int64): 表的 pg_class.oid。
//
// 返回 Returns:
//   - constraints ([]constraint): 主键/唯一/检查/排他约束，按类型与名称排序。
//   - foreignKeys ([]constraint): 外键约束。
//   - err (error): 查询失败时返回非 nil。
func loadConstraints(ctx context.Context, conn *sql.Conn, oid int64) ([]constraint, []constraint, error) {
	// 不含 contype='n'：NOT NULL 在 PostgreSQL 18 也存进 pg_constraint，
	// 但列上的 NOT NULL 已经写在 CREATE TABLE 里，重复添加会报约束重名。
	rows, err := conn.QueryContext(ctx, `
		SELECT con.conname, con.contype::text, pg_get_constraintdef(con.oid, true)
		FROM pg_constraint con
		WHERE con.conrelid = $1 AND con.contype IN ('p', 'u', 'c', 'x', 'f')
		ORDER BY CASE con.contype WHEN 'p' THEN 1 WHEN 'u' THEN 2 WHEN 'c' THEN 3 WHEN 'x' THEN 4 ELSE 5 END,
		         con.conname`, oid)
	if err != nil {
		return nil, nil, fmt.Errorf("pgdump: 查询约束失败: %w", err)
	}
	defer rows.Close()
	var (
		constraints []constraint
		foreignKeys []constraint
	)
	for rows.Next() {
		var item constraint
		if err := rows.Scan(&item.name, &item.contype, &item.definition); err != nil {
			return nil, nil, fmt.Errorf("pgdump: 读取约束失败: %w", err)
		}
		if item.contype == "f" {
			foreignKeys = append(foreignKeys, item)
			continue
		}
		constraints = append(constraints, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("pgdump: 遍历约束失败: %w", err)
	}
	return constraints, foreignKeys, nil
}

// loadIndexes 读取一张表的非约束索引。
//
// 主键/唯一/排他约束会在建表后由 ADD CONSTRAINT 自动建索引，
// 这里必须把 conindid 指向的索引排除，否则同一索引会被建两次。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - conn (*sql.Conn): 独占连接。
//   - oid (int64): 表的 pg_class.oid。
//
// 返回 Returns:
//   - indexes ([]indexDef): 索引定义，按名称排序。
//   - err (error): 查询失败时返回非 nil。
func loadIndexes(ctx context.Context, conn *sql.Conn, oid int64) ([]indexDef, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT ic.relname, pg_get_indexdef(i.indexrelid)
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = ic.relnamespace
		LEFT JOIN pg_constraint con
		       ON con.conindid = i.indexrelid AND con.contype IN ('p', 'u', 'x')
		WHERE i.indrelid = $1
		  AND con.oid IS NULL
		  AND i.indisvalid
		  AND i.indisready
		  AND n.nspname NOT LIKE 'pg\_%'
		ORDER BY ic.relname`, oid)
	if err != nil {
		return nil, fmt.Errorf("pgdump: 查询索引失败: %w", err)
	}
	defer rows.Close()
	var indexes []indexDef
	for rows.Next() {
		var item indexDef
		if err := rows.Scan(&item.name, &item.definition); err != nil {
			return nil, fmt.Errorf("pgdump: 读取索引失败: %w", err)
		}
		indexes = append(indexes, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgdump: 遍历索引失败: %w", err)
	}
	return indexes, nil
}
