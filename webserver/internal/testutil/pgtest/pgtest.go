// Package pgtest 为需要 PostgreSQL 的测试提供互相隔离的数据库会话。
//
// 隔离方式用「独立 schema + search_path」而不是新建 database：
// 前者不需要建库权限，且内嵌 SQL 里的裸表名会自动落到该 schema，业务 SQL 无需改写。
package pgtest

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// defaultDSN 与 configs/webserver.yaml 的 postgres 段保持一致，便于本地直接跑测试。
const defaultDSN = "host=localhost port=5432 user=postgres password=123456 dbname=postgres sslmode=disable"

// envDSN 是覆盖默认连接串的环境变量。
const envDSN = "WEBSERVER_TEST_PG_DSN"

// schemaSeq 给同一进程内的多个用例生成不同的 schema 名，避免并发测试撞名。
var schemaSeq atomic.Int64

// Open 创建一个只属于当前测试的 PostgreSQL 会话。
//
// 每个用例拿到独立 schema，测试结束自动 DROP SCHEMA ... CASCADE，
// 因此用例之间不会互相污染，也不需要手工清库。
//
// 参数 Parameters:
//   - t (*testing.T): 当前测试。
//
// 返回 Returns:
//   - db (*gorm.DB): 已指向独立 schema 的会话。
func Open(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv(envDSN))
	if dsn == "" {
		dsn = defaultDSN
	}
	schema := fmt.Sprintf("test_%d_%d", time.Now().UnixNano()%1_000_000_000, schemaSeq.Add(1))

	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("连接测试 PostgreSQL 失败（可用 %s 指定连接串）: %v", envDSN, err)
	}
	if err := admin.Exec("CREATE SCHEMA " + quoteIdent(schema)).Error; err != nil {
		closeDB(admin)
		t.Fatalf("创建测试 schema %s 失败: %v", schema, err)
	}

	// search_path 写在连接串里而不是执行 SET：GORM 会用连接池里的多条连接，
	// 会话级的 SET 只对其中一条生效，其余连接仍会落到 public schema。
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		dropSchema(admin, schema)
		closeDB(admin)
		t.Fatalf("打开测试库失败: %v", err)
	}

	t.Cleanup(func() {
		closeDB(db)
		dropSchema(admin, schema)
		closeDB(admin)
	})
	return db
}

// quoteIdent 引用 SQL 标识符。
func quoteIdent(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// dropSchema 删除测试 schema 及其全部对象。
func dropSchema(admin *gorm.DB, schema string) {
	if admin == nil {
		return
	}
	_ = admin.Exec("DROP SCHEMA IF EXISTS " + quoteIdent(schema) + " CASCADE").Error
}

// closeDB 关闭连接池。
func closeDB(db *gorm.DB) {
	if db == nil {
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
