package job

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/company/monitor-webserver/internal/pgdump"
)

// runPostgresBackup 执行一次 PostgreSQL 备份，产物是可直接导入的纯 SQL。
//
// 生成逻辑在 internal/pgdump：它用 pg_catalog 的 format_type / pg_get_expr /
// pg_get_constraintdef / pg_get_indexdef 取 DDL 原文，因而不需要外部 pg_dump CLI，
// 同时又具备 pg_dump 级的完整性（类型名、自增列、序列、索引、约束、序列值全都有）。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - svc (*Service): 计划任务服务（提供连接与备份目录）。
//
// 返回 Returns:
//   - message (string): 备份结果说明。
//   - err (error): 备份失败时返回非 nil。
func runPostgresBackup(ctx context.Context, svc *Service) (string, error) {
	if err := os.MkdirAll(svc.backupDir, 0o755); err != nil {
		return "", fmt.Errorf("创建备份目录失败: %w", err)
	}
	// 年月日 + 4 位随机数，如 postgres_20261013_7391.sql
	timestamp := time.Now().Format("20060102")
	randNum := strconv.Itoa(1000 + int(time.Now().UnixNano()%9000))
	dest := filepath.Join(svc.backupDir, fmt.Sprintf("postgres_%s_%s.sql", timestamp, randNum))

	sqlDB, err := svc.db.DB()
	if err != nil {
		return "", fmt.Errorf("获取底层 SQL DB 失败: %w", err)
	}

	file, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("创建备份文件失败: %w", err)
	}
	stats, dumpErr := pgdump.Dump(ctx, sqlDB, file, pgdump.Options{
		Database:    svc.pgConfig.Database,
		IncludeData: true,
	})
	// 关闭错误同样要处理：缓冲数据可能还在文件里没落盘。
	if closeErr := file.Close(); dumpErr == nil && closeErr != nil {
		dumpErr = fmt.Errorf("关闭备份文件失败: %w", closeErr)
	}
	if dumpErr != nil {
		// 失败的产物不留在备份目录里，避免被当成可用备份。
		_ = os.Remove(dest)
		return "", fmt.Errorf("导出 PostgreSQL 失败: %w", dumpErr)
	}

	return fmt.Sprintf("PostgreSQL 已备份到 %s（表 %d / 序列 %d / 索引 %d / 约束 %d / 数据 %d 行）",
		dest, stats.Tables, stats.Sequences, stats.Indexes, stats.Constraints, stats.Rows), nil
}
