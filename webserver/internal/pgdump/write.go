package pgdump

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// sqlWriter 是带「首次错误即短路」语义的输出包装。
//
// 备份文件动辄几千行，逐行判断错误会让调用方被大量 if err != nil 淹没；
// 这里只记录首个错误，后续写入全部变成空操作，最后由 Dump 统一返回。
type sqlWriter struct {
	// w 底层缓冲写入器。
	w *bufio.Writer
	// err 首个写入错误。
	err error
}

// printf 按格式写出内容；已出错时不再写入。
func (s *sqlWriter) printf(format string, args ...any) {
	if s.err != nil {
		return
	}
	if _, err := fmt.Fprintf(s.w, format, args...); err != nil {
		s.err = err
	}
}

// dumper 持有一次导出的全部上下文。
type dumper struct {
	// w 输出写入器。
	w *sqlWriter
	// cat 元信息。
	cat catalog
	// opts 导出选项。
	opts Options
	// stats 导出统计。
	stats Stats
	// identity 按「schema.表.列」索引 identity 序列。
	identity map[string]*sequence
}

// newDumper 创建导出器并预算好对象数量（供文件头展示）。
func newDumper(w *sqlWriter, cat catalog, opts Options) *dumper {
	d := &dumper{w: w, cat: cat, opts: opts, identity: make(map[string]*sequence)}
	d.stats.Schemas = len(cat.schemas)
	d.stats.Tables = len(cat.tables)
	d.stats.Sequences = len(cat.sequences)
	for i := range cat.sequences {
		seq := &cat.sequences[i]
		if seq.identity && seq.ownsSequence() {
			d.identity[seq.ownerSchema+"."+seq.ownerTable+"."+seq.ownerColumn] = seq
		}
	}
	for _, t := range cat.tables {
		d.stats.Indexes += len(t.indexes)
		d.stats.Constraints += len(t.constraints) + len(t.foreignKeys)
	}
	return d
}

// ---------------------------------------------------------------------------
// 文件头与前后置语句
// ---------------------------------------------------------------------------

// writeHeader 写出文件头与执行环境设置。
//
// 参数 Parameters:
//   - now (time.Time): 导出时间。
func (d *dumper) writeHeader(now time.Time) {
	d.w.printf(`-- ===========================================================================
-- PostgreSQL 备份（结构 + 数据 + 索引 + 序列）
-- 数据库   : %s
-- 导出时间 : %s
-- 生成者   : %s
-- 对象统计 : schema %d / 表 %d / 序列 %d / 索引 %d / 约束 %d
-- 数据范围 : %s
-- ===========================================================================
-- 导入方式（先在目标实例建好空库，再执行本文件）：
--   psql -h <host> -p 5432 -U <user> -d <db> -v ON_ERROR_STOP=1 -f 本文件
--   或 pgAdmin：选中目标数据库 → Query Tool → 打开本文件 → 执行
--
-- 说明：
--   1. 文件整体包在 BEGIN/COMMIT 中，任何一条语句失败都会整体回滚，不会留下半成品；
--   2. 表以 DROP TABLE IF EXISTS ... CASCADE + CREATE TABLE 重建，会覆盖目标库的同名表；
--   3. 自增列用 GENERATED ... AS IDENTITY 还原，文件末尾的 setval 会把序列推进到
--      属主列的当前最大值，因此导入后可以直接新增数据，不会主键冲突；
--   4. search_path 被显式置空，所有对象都写成全限定名，导入不依赖目标库的 search_path；
--   5. 结构与数据取自同一个 REPEATABLE READ 只读快照，导出期间业务继续写入也不会前后不一致。
-- ===========================================================================

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET client_min_messages = warning;
SET row_security = off;

BEGIN;

`, d.opts.Database, now.Format("2006-01-02 15:04:05"), d.opts.Generator,
		d.stats.Schemas, d.stats.Tables, d.stats.Sequences, d.stats.Indexes, d.stats.Constraints, d.dataScope())
}

// dataScope 描述本次导出的数据范围。
//
// 写在文件头是为了让拿到文件的人一眼分清「全量备份」与「初始化范本」：
// 范本会按配置跳过日志、告警、会话等运行期表的数据。
//
// 返回 Returns:
//   - scope (string): 数据范围说明。
func (d *dumper) dataScope() string {
	switch {
	case !d.opts.IncludeData:
		return "只导结构（未导出任何数据）"
	case len(d.opts.ExcludeData) > 0:
		return fmt.Sprintf("全量数据，但 %d 张表按配置只导结构（文件内以「跳过数据」标注）",
			len(d.opts.ExcludeData))
	default:
		return "全量数据"
	}
}

// writeSchemas 写出 CREATE SCHEMA 语句。
//
// 用 IF NOT EXISTS：导入脚本既可以跑在全新库上（public 已由 PostgreSQL 建好），
// 也可以重复执行。
func (d *dumper) writeSchemas() {
	if len(d.cat.schemas) == 0 {
		return
	}
	d.w.printf("--\n-- Schema\n--\n\n")
	for _, schema := range d.cat.schemas {
		d.w.printf("CREATE SCHEMA IF NOT EXISTS %s;\n", quoteIdent(schema))
	}
	d.w.printf("\n")
}

// writeDrops 写出对象清理语句，保证脚本可重复执行。
//
// 顺序很关键：先 DROP TABLE CASCADE（identity 序列会随表一起消失），
// 再 DROP SEQUENCE 兜住独立的 serial 序列，最后才 CREATE，避免重名。
func (d *dumper) writeDrops() {
	if len(d.cat.tables) == 0 && len(d.cat.sequences) == 0 {
		return
	}
	d.w.printf("--\n-- 清理同名对象（让本文件可重复执行）\n--\n\n")
	for _, t := range d.cat.tables {
		d.w.printf("DROP TABLE IF EXISTS %s CASCADE;\n", t.qualified())
	}
	for _, seq := range d.cat.sequences {
		d.w.printf("DROP SEQUENCE IF EXISTS %s CASCADE;\n", seq.qualified())
	}
	d.w.printf("\n")
}

// writeSequences 写出独立序列（identity 序列由 ADD GENERATED 隐式创建，不能单独建）。
//
// 返回 Returns:
//   - written (int): 写出的序列数。
func (d *dumper) writeSequences() int {
	written := 0
	started := false
	for _, seq := range d.cat.sequences {
		if seq.identity {
			continue
		}
		if !started {
			d.w.printf("--\n-- 序列（必须在建表之前：serial 列的默认值引用它）\n--\n\n")
			started = true
		}
		d.w.printf("CREATE SEQUENCE %s\n", seq.qualified())
		d.w.printf("    AS %s\n", seq.dataType)
		d.w.printf("    START WITH %d\n", seq.start)
		d.w.printf("    INCREMENT BY %d\n", seq.increment)
		d.w.printf("%s\n", sequenceBound("MINVALUE", "NO MINVALUE", seq.min, sequenceMinDefault(seq)))
		d.w.printf("%s\n", sequenceBound("MAXVALUE", "NO MAXVALUE", seq.max, sequenceMaxDefault(seq)))
		d.w.printf("    CACHE %d", seq.cache)
		if seq.cycle {
			d.w.printf("\n    CYCLE")
		}
		d.w.printf(";\n\n")
		written++
	}
	return written
}

// writeTables 写出建表语句、identity 列与立即生效的约束。
func (d *dumper) writeTables() {
	if len(d.cat.tables) == 0 {
		return
	}
	d.w.printf("--\n-- 表结构\n--\n")
	for i := range d.cat.tables {
		t := &d.cat.tables[i]
		d.w.printf("\n-- 表 %s\n", t.qualified())
		lines := make([]string, 0, len(t.columns))
		for _, column := range t.columns {
			lines = append(lines, "    "+d.columnDefinition(t, column))
		}
		d.w.printf("CREATE TABLE %s (\n%s\n);\n", t.qualified(), strings.Join(lines, ",\n"))
		d.writeIdentity(t)
		for _, item := range t.constraints {
			d.w.printf("ALTER TABLE ONLY %s ADD CONSTRAINT %s %s;\n",
				t.qualified(), quoteIdent(item.name), item.definition)
		}
	}
	d.w.printf("\n")
}

// writeIdentity 写出 identity 列的 ADD GENERATED 语句。
//
// 之所以不在 CREATE TABLE 里内联，是为了连序列名与序列参数一起还原（pg_dump 亦如此）；
// 万一取不到该列的序列（理论上不会），退化为内联的 GENERATED 子句，保证自增能力不丢。
//
// 参数 Parameters:
//   - t (*table): 目标表。
func (d *dumper) writeIdentity(t *table) {
	for _, column := range t.columns {
		if column.identity == "" {
			continue
		}
		seq := d.identity[t.schema+"."+t.name+"."+column.name]
		if seq == nil {
			continue
		}
		generation := "BY DEFAULT"
		if seq.always {
			generation = "ALWAYS"
		}
		d.w.printf("ALTER TABLE %s ALTER COLUMN %s ADD GENERATED %s AS IDENTITY (\n",
			t.qualified(), quoteIdent(column.name), generation)
		d.w.printf("    SEQUENCE NAME %s\n", seq.qualified())
		d.w.printf("    START WITH %d\n", seq.start)
		d.w.printf("    INCREMENT BY %d\n", seq.increment)
		d.w.printf("%s\n", sequenceBound("MINVALUE", "NO MINVALUE", seq.min, sequenceMinDefault(*seq)))
		d.w.printf("%s\n", sequenceBound("MAXVALUE", "NO MAXVALUE", seq.max, sequenceMaxDefault(*seq)))
		d.w.printf("    CACHE %d", seq.cache)
		if seq.cycle {
			d.w.printf("\n    CYCLE")
		}
		d.w.printf("\n);\n")
	}
}

// columnDefinition 渲染单列定义。
//
// 参数 Parameters:
//   - t (*table): 列所属表（用于查 identity 序列）。
//   - c (column): 列定义。
//
// 返回 Returns:
//   - definition (string): 形如 `"id" integer DEFAULT 0 NOT NULL` 的片段。
func (d *dumper) columnDefinition(t *table, c column) string {
	var builder strings.Builder
	builder.WriteString(quoteIdent(c.name))
	builder.WriteString(" ")
	builder.WriteString(c.typeName)
	if c.collation != "" {
		builder.WriteString(" COLLATE ")
		builder.WriteString(qualifyCollation(c.collation))
	}
	switch {
	case c.identity != "":
		// 由 ADD GENERATED 语句补；仅当取不到序列时内联兜底。
		if d.identity[t.schema+"."+t.name+"."+c.name] == nil {
			generation := "BY DEFAULT"
			if c.alwaysIdentity() {
				generation = "ALWAYS"
			}
			builder.WriteString(" GENERATED ")
			builder.WriteString(generation)
			builder.WriteString(" AS IDENTITY")
		}
	case c.generated == "s":
		builder.WriteString(" GENERATED ALWAYS AS (")
		builder.WriteString(c.defaultExpr)
		builder.WriteString(") STORED")
	case c.defaultExpr != "":
		builder.WriteString(" DEFAULT ")
		builder.WriteString(c.defaultExpr)
	}
	if c.notNull {
		builder.WriteString(" NOT NULL")
	}
	return builder.String()
}

// writeData 写出全部表的数据。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - conn (*sql.Conn): 独占连接（与元信息同处一个快照事务）。
//
// 返回 Returns:
//   - err (error): 读取或转义失败时返回非 nil。
func (d *dumper) writeData(ctx context.Context, conn *sql.Conn) error {
	d.w.printf("--\n-- 数据\n--\n")
	for i := range d.cat.tables {
		if err := d.writeTableData(ctx, conn, &d.cat.tables[i]); err != nil {
			return err
		}
	}
	d.w.printf("\n")
	return nil
}

// writeTableData 写出一张表的数据（每 BatchSize 行合并在一条 INSERT 里）。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - conn (*sql.Conn): 独占连接。
//   - t (*table): 目标表。
//
// 返回 Returns:
//   - err (error): 读取或转义失败时返回非 nil。
func (d *dumper) writeTableData(ctx context.Context, conn *sql.Conn, t *table) error {
	columns := t.dataColumns()
	if t.skipData {
		d.w.printf("\n-- 表 %s：按配置只导结构，跳过数据\n", t.qualified())
		return nil
	}
	if len(columns) == 0 {
		d.w.printf("\n-- 表 %s：没有可导出的列\n", t.qualified())
		return nil
	}

	names := make([]string, len(columns))
	for i, column := range columns {
		names[i] = quoteIdent(column.name)
	}
	columnList := strings.Join(names, ", ")
	// GENERATED ALWAYS 的 identity 列拒绝显式写入，必须带 OVERRIDING SYSTEM VALUE。
	overriding := ""
	if t.hasAlwaysIdentity() {
		overriding = " OVERRIDING SYSTEM VALUE"
	}

	rows, err := conn.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s", columnList, t.qualified()))
	if err != nil {
		return fmt.Errorf("pgdump: 读取表 %s 数据失败: %w", t.qualified(), err)
	}
	defer func() { _ = rows.Close() }()

	d.w.printf("\n-- 表 %s 的数据\n", t.qualified())
	values := make([]any, len(columns))
	targets := make([]any, len(columns))
	for i := range values {
		targets[i] = &values[i]
	}
	rendered := make([]string, len(columns))
	batch := make([]string, 0, d.opts.BatchSize)
	var count int64

	flush := func() {
		if len(batch) == 0 {
			return
		}
		d.w.printf("INSERT INTO %s (%s)%s VALUES\n", t.qualified(), columnList, overriding)
		for i, row := range batch {
			if i == len(batch)-1 {
				d.w.printf("%s;\n", row)
				continue
			}
			d.w.printf("%s,\n", row)
		}
		batch = batch[:0]
	}

	for rows.Next() {
		if err := rows.Scan(targets...); err != nil {
			return fmt.Errorf("pgdump: 扫描表 %s 数据失败: %w", t.qualified(), err)
		}
		for i, value := range values {
			literal, renderErr := renderValue(columns[i].typeName, value)
			if renderErr != nil {
				return fmt.Errorf("pgdump: 表 %s 列 %s: %w", t.qualified(), columns[i].name, renderErr)
			}
			rendered[i] = literal
		}
		batch = append(batch, "    ("+strings.Join(rendered, ", ")+")")
		count++
		if len(batch) >= d.opts.BatchSize {
			flush()
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("pgdump: 遍历表 %s 数据失败: %w", t.qualified(), err)
	}
	flush()
	d.stats.Rows += count
	return nil
}

// writePostData 写出数据之后才能建立的对象：索引、外键、序列属主与序列值。
func (d *dumper) writePostData() {
	d.writeIndexes()
	d.writeForeignKeys()
	d.writeSequenceOwners()
	d.writeSequenceValues()
}

// writeIndexes 写出全部非约束索引。
func (d *dumper) writeIndexes() {
	total := 0
	for _, t := range d.cat.tables {
		total += len(t.indexes)
	}
	if total == 0 {
		return
	}
	d.w.printf("--\n-- 索引\n--\n\n")
	for _, t := range d.cat.tables {
		for _, index := range t.indexes {
			d.w.printf("%s;\n", index.definition)
		}
	}
	d.w.printf("\n")
}

// writeForeignKeys 写出外键约束。
//
// 放在数据之后是为了不依赖表的导入顺序：无论父表先写还是后写，此时数据都已就位。
func (d *dumper) writeForeignKeys() {
	total := 0
	for _, t := range d.cat.tables {
		total += len(t.foreignKeys)
	}
	if total == 0 {
		return
	}
	d.w.printf("--\n-- 外键\n--\n\n")
	for _, t := range d.cat.tables {
		for _, item := range t.foreignKeys {
			d.w.printf("ALTER TABLE ONLY %s ADD CONSTRAINT %s %s;\n",
				t.qualified(), quoteIdent(item.name), item.definition)
		}
	}
	d.w.printf("\n")
}

// writeSequenceOwners 写出序列与列的归属关系。
//
// 归属关系决定 DROP TABLE 时序列是否随表删除，也决定 pg_get_serial_sequence 能否
// 找到序列（webserver 启动时会用它校正自增序列），因此必须还原。
func (d *dumper) writeSequenceOwners() {
	written := 0
	for _, seq := range d.cat.sequences {
		if seq.identity || !seq.ownsSequence() {
			continue
		}
		if written == 0 {
			d.w.printf("--\n-- 序列归属\n--\n\n")
		}
		d.w.printf("ALTER SEQUENCE %s OWNED BY %s.%s;\n",
			seq.qualified(), qualified(seq.ownerSchema, seq.ownerTable), quoteIdent(seq.ownerColumn))
		written++
	}
	if written > 0 {
		d.w.printf("\n")
	}
}

// writeSequenceValues 校正所有序列的当前值。
//
// 这是「导入后第一条 INSERT 就主键冲突」的根治手段：identity/serial 列的显式取值
// 不会让序列前进，必须按属主列的 MAX 重新 setval。
func (d *dumper) writeSequenceValues() {
	if len(d.cat.sequences) == 0 {
		return
	}
	d.w.printf("--\n-- 序列当前值（按属主列的最大值对齐）\n--\n\n")
	for _, seq := range d.cat.sequences {
		if !seq.ownsSequence() {
			// 无属主序列（少见）只能按导出时的取值还原。
			d.w.printf("SELECT setval(%s, %d, true);\n", quoteStringLiteral(seq.qualified()), seq.lastValue)
			continue
		}
		d.w.printf("SELECT setval(%s, COALESCE((SELECT MAX(%s) FROM %s), 1), (SELECT COUNT(*) FROM %s) > 0);\n",
			quoteStringLiteral(seq.qualified()), quoteIdent(seq.ownerColumn),
			qualified(seq.ownerSchema, seq.ownerTable), qualified(seq.ownerSchema, seq.ownerTable))
	}
	d.w.printf("\n")
}

// writeFooter 写出提交语句、会话还原与文件尾。
func (d *dumper) writeFooter() {
	d.w.printf(`COMMIT;

-- 还原会话的 search_path：文件开头把它置空是为了让所有对象都按全限定名解析，
-- 但设置是会话级的，在 pgAdmin 的 Query Tool 里执行后会一直生效，
-- 之后手写的未限定表名 SQL 会莫名其妙地报「关系不存在」。
RESET search_path;

-- ===========================================================================
-- 备份结束：schema %d / 表 %d / 序列 %d / 索引 %d / 约束 %d / 数据 %d 行
-- ===========================================================================
`, d.stats.Schemas, d.stats.Tables, d.stats.Sequences, d.stats.Indexes, d.stats.Constraints, d.stats.Rows)
}

// ---------------------------------------------------------------------------
// 标识符与字面量渲染
// ---------------------------------------------------------------------------

// quoteIdent 按 SQL 规则引用标识符（内部双引号翻倍）。
//
// 不用 fmt 的 %q：那是 Go 的字符串转义（\t、\x00 之类），PostgreSQL 的标识符里
// 只有 "" 是合法转义，其余会被当作字面反斜杠而导致对象名错乱。
//
// 参数 Parameters:
//   - identifier (string): 原始标识符。
//
// 返回 Returns:
//   - quoted (string): 形如 "foo" 的安全标识符。
func quoteIdent(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// qualified 组合 schema 与对象名为全限定名。
//
// 参数 Parameters:
//   - schema (string): schema 名。
//   - name (string): 对象名。
//
// 返回 Returns:
//   - qualified (string): 形如 "public"."alerts" 的全限定名。
func qualified(schema, name string) string {
	return quoteIdent(schema) + "." + quoteIdent(name)
}

// qualifyCollation 把「schema.名称」形式的排序规则转成全限定引用。
//
// 参数 Parameters:
//   - collation (string): loadColumns 产出的排序规则名。
//
// 返回 Returns:
//   - quoted (string): 可直接拼进 DDL 的引用。
func qualifyCollation(collation string) string {
	parts := strings.SplitN(collation, ".", 2)
	if len(parts) != 2 {
		return quoteIdent(collation)
	}
	return qualified(parts[0], parts[1])
}

// quoteStringLiteral 把字符串渲染为 SQL 字符串字面量。
//
// 默认走标准形式（只把单引号翻倍）——文件头已 SET standard_conforming_strings = on，
// 反斜杠是普通字符，不需要额外转义。只有含 NUL 的字符串必须改用 E 前缀转义：
// 标准形式下无法表示 NUL，而 E 前缀里的 \000 八进制转义可以。
//
// 参数 Parameters:
//   - value (string): 原始字符串。
//
// 返回 Returns:
//   - literal (string): 字符串字面量。
func quoteStringLiteral(value string) string {
	if !strings.ContainsRune(value, 0) {
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	var builder strings.Builder
	builder.WriteString("E'")
	for _, char := range value {
		switch char {
		case '\'':
			builder.WriteString("''")
		case '\\':
			builder.WriteString(`\\`)
		case 0:
			builder.WriteString(`\000`)
		default:
			builder.WriteRune(char)
		}
	}
	builder.WriteString("'")
	return builder.String()
}

// quoteBytea 把字节串渲染为 bytea 字面量（十六进制输入格式）。
//
// 参数 Parameters:
//   - value ([]byte): 原始字节。
//
// 返回 Returns:
//   - literal (string): 形如 '\xdeadbeef'::bytea 的字面量。
func quoteBytea(value []byte) string {
	return `'\x` + hex.EncodeToString(value) + `'::bytea`
}

// valueKind 是字面量渲染时用到的类型分类。
type valueKind int

const (
	// kindString 字符串类（text / varchar / uuid 等），按字符串字面量输出。
	kindString valueKind = iota
	// kindNumber 数值类，直接输出数字。
	kindNumber
	// kindBool 布尔类。
	kindBool
	// kindBytea 二进制类。
	kindBytea
	// kindJSON JSON / JSONB，字符串字面量 + 显式类型转换。
	kindJSON
	// kindTimestampTZ 带时区的时间戳。
	kindTimestampTZ
	// kindTimestamp 不带时区的时间戳。
	kindTimestamp
	// kindDate 日期。
	kindDate
	// kindTime 时间（含 timetz）。
	kindTime
	// kindCast 其它类型：字符串字面量 + ::类型 转换。
	kindCast
)

// numericLiteralPattern 匹配 PostgreSQL 可以接受的数值字面量。
//
// 包含 NaN / Infinity：numeric、real 等类型支持它们。
var numericLiteralPattern = regexp.MustCompile(`^-?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$|^(NaN|Infinity|-Infinity)$`)

// kindOf 按 format_type 渲染出的类型名分类。
//
// 参数 Parameters:
//   - typeName (string): format_type 输出，例如 character varying(64)。
//
// 返回 Returns:
//   - kind (valueKind): 类型分类。
func kindOf(typeName string) valueKind {
	name := strings.ToLower(strings.TrimSpace(typeName))
	switch {
	case name == "bytea":
		return kindBytea
	case name == "boolean" || name == "bool":
		return kindBool
	case name == "smallint" || name == "integer" || name == "bigint" || name == "oid" ||
		name == "real" || name == "double precision" || name == "money" ||
		strings.HasPrefix(name, "numeric"):
		return kindNumber
	case name == "json" || name == "jsonb":
		return kindJSON
	case strings.HasPrefix(name, "timestamp") && strings.Contains(name, "with time zone"):
		return kindTimestampTZ
	case strings.HasPrefix(name, "timestamp"):
		return kindTimestamp
	case name == "date":
		return kindDate
	case strings.HasPrefix(name, "time"):
		return kindTime
	case strings.HasPrefix(name, "character"), name == "text", name == "name",
		name == "uuid", name == "inet", name == "cidr", name == "macaddr", name == "xml":
		return kindString
	default:
		return kindCast
	}
}

// renderValue 把驱动返回的值渲染成 SQL 字面量。
//
// 参数 Parameters:
//   - typeName (string): 列类型（format_type 输出）。
//   - value (any): 驱动返回的值。
//
// 返回 Returns:
//   - literal (string): SQL 字面量。
//   - err (error): 类型不支持或值非法时返回非 nil（宁可导出失败，也不写出导不进去的备份）。
func renderValue(typeName string, value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "NULL", nil
	case []byte:
		// 驱动对 text / jsonb / numeric 等也可能返回 []byte，因此先看列类型再决定。
		if kindOf(typeName) == kindBytea {
			return quoteBytea(typed), nil
		}
		return renderText(typeName, string(typed))
	case string:
		if kindOf(typeName) == kindBytea {
			return quoteBytea([]byte(typed)), nil
		}
		return renderText(typeName, typed)
	case time.Time:
		return renderTime(typeName, typed), nil
	case bool:
		if kindOf(typeName) == kindNumber {
			if typed {
				return "1", nil
			}
			return "0", nil
		}
		return strconv.FormatBool(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case int32:
		return strconv.FormatInt(int64(typed), 10), nil
	case int16:
		return strconv.FormatInt(int64(typed), 10), nil
	case int:
		return strconv.FormatInt(int64(typed), 10), nil
	case int8:
		return strconv.FormatInt(int64(typed), 10), nil
	case uint64:
		return strconv.FormatUint(typed, 10), nil
	case uint32:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(typed), 10), nil
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(typed), 'g', -1, 32), nil
	default:
		// pgx 对 numeric / uuid 等可能返回自定义类型，统一按文本处理。
		return renderText(typeName, fmt.Sprint(value))
	}
}

// renderText 把文本形态的值按列类型渲染。
//
// 参数 Parameters:
//   - typeName (string): 列类型。
//   - text (string): 值的文本形态。
//
// 返回 Returns:
//   - literal (string): SQL 字面量。
//   - err (error): 值不符合列类型时返回非 nil。
func renderText(typeName, text string) (string, error) {
	switch kindOf(typeName) {
	case kindNumber:
		if numericLiteralPattern.MatchString(text) {
			return text, nil
		}
		return "", fmt.Errorf("值 %q 不是合法数值", text)
	case kindBool:
		switch strings.ToLower(text) {
		case "true", "t", "1":
			return "true", nil
		case "false", "f", "0":
			return "false", nil
		default:
			return "", fmt.Errorf("值 %q 不是合法布尔", text)
		}
	case kindJSON:
		return quoteStringLiteral(text) + "::" + typeName, nil
	case kindBytea:
		return quoteBytea([]byte(text)), nil
	case kindCast:
		return quoteStringLiteral(text) + "::" + typeName, nil
	default:
		return quoteStringLiteral(text), nil
	}
}

// renderTime 把时间值按列类型渲染（保留微秒精度）。
//
// 带时区的列输出时区偏移，导入到其他时区的库上也表示同一时刻；
// 不带时区的列输出裸时间，避免被目标库的 TimeZone 二次换算。
//
// 参数 Parameters:
//   - typeName (string): 列类型。
//   - value (time.Time): 时间值。
//
// 返回 Returns:
//   - literal (string): SQL 字面量。
func renderTime(typeName string, value time.Time) string {
	switch kindOf(typeName) {
	case kindTimestampTZ:
		return quoteStringLiteral(value.Format("2006-01-02 15:04:05.999999-07:00"))
	case kindTimestamp:
		return quoteStringLiteral(value.Format("2006-01-02 15:04:05.999999"))
	case kindDate:
		return quoteStringLiteral(value.Format("2006-01-02"))
	case kindTime:
		return quoteStringLiteral(value.Format("15:04:05.999999"))
	default:
		return quoteStringLiteral(value.Format(time.RFC3339Nano))
	}
}

// sequenceMinDefault 返回该序列类型的默认最小值。
//
// 参数 Parameters:
//   - seq (sequence): 序列定义。
//
// 返回 Returns:
//   - value (int64): 默认最小值（递增序列为 1）。
func sequenceMinDefault(seq sequence) int64 { return 1 }

// sequenceMaxDefault 返回该序列类型的默认最大值。
//
// 参数 Parameters:
//   - seq (sequence): 序列定义。
//
// 返回 Returns:
//   - value (int64): 数据类型对应的最大值。
func sequenceMaxDefault(seq sequence) int64 {
	switch seq.dataType {
	case "smallint":
		return 32767
	case "integer":
		return 2147483647
	default:
		return int64(^uint64(0) >> 1)
	}
}

// sequenceBound 渲染 MINVALUE / MAXVALUE 行：等于类型默认值时用 NO MINVALUE / NO MAXVALUE。
//
// 参数 Parameters:
//   - keyword (string): MINVALUE 或 MAXVALUE。
//   - noKeyword (string): NO MINVALUE 或 NO MAXVALUE。
//   - value (int64): 实际取值。
//   - fallback (int64): 该类型的默认取值。
//
// 返回 Returns:
//   - line (string): 形如 "    NO MAXVALUE" 或 "    MAXVALUE 1000"。
func sequenceBound(keyword, noKeyword string, value, fallback int64) string {
	if value == fallback {
		return "    " + noKeyword
	}
	return fmt.Sprintf("    %s %d", keyword, value)
}
