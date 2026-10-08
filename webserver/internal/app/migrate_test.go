package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/company/monitor-webserver/internal/model"
)

// newTestDB 打开一个临时 SQLite 权限库。
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "webserver.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	// Windows 下 SQLite 文件会保持占用，测试结束前显式关闭连接。
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// TestMigrateSeedsPermissionData 验证迁移写入表结构与种子数据，并且可重复执行。
func TestMigrateSeedsPermissionData(t *testing.T) {
	db := newTestDB(t)
	if err := Migrate(db, ""); err != nil {
		t.Fatalf("首次迁移失败: %v", err)
	}

	var admin model.SysUser
	if err := db.Where("username = ?", defaultAdminUsername).First(&admin).Error; err != nil {
		t.Fatalf("内置管理员缺失: %v", err)
	}
	if admin.IsSuper != 1 {
		t.Fatalf("内置管理员 is_super 应为 1，实际 %d", admin.IsSuper)
	}
	if !strings.HasPrefix(admin.Password, "$2") {
		t.Fatalf("管理员密码应为 bcrypt 哈希，实际 %q", admin.Password)
	}

	var menuCount int64
	if err := db.Model(&model.SysMenu{}).Count(&menuCount).Error; err != nil {
		t.Fatalf("统计菜单失败: %v", err)
	}
	if menuCount < 40 {
		t.Fatalf("菜单种子数量异常: %d", menuCount)
	}

	// 监控中心菜单必须存在，否则重构后监控模块会从侧边栏消失。
	var monitorMenu model.SysMenu
	if err := db.Where("path = ? AND type = 1", "/monitor").First(&monitorMenu).Error; err != nil {
		t.Fatalf("监控中心目录菜单缺失: %v", err)
	}
	var monitorChildren int64
	if err := db.Model(&model.SysMenu{}).Where("parent_id = ?", monitorMenu.ID).Count(&monitorChildren).Error; err != nil {
		t.Fatalf("统计监控子菜单失败: %v", err)
	}
	if monitorChildren < 3 {
		t.Fatalf("监控子菜单数量异常: %d", monitorChildren)
	}

	var grantCount int64
	if err := db.Table("sa_system_role_menu").Count(&grantCount).Error; err != nil {
		t.Fatalf("统计角色菜单授权失败: %v", err)
	}
	if grantCount == 0 {
		t.Fatalf("角色菜单授权种子为空")
	}

	// 幂等：重复迁移不应报错，也不应重复插入。
	if err := Migrate(db, ""); err != nil {
		t.Fatalf("重复迁移失败: %v", err)
	}
	var users int64
	if err := db.Model(&model.SysUser{}).Where("username = ?", defaultAdminUsername).Count(&users).Error; err != nil {
		t.Fatalf("统计管理员失败: %v", err)
	}
	if users != 1 {
		t.Fatalf("重复迁移产生了重复管理员: %d", users)
	}
}

// TestSplitSQLStatements 验证 SQL 拆分能正确处理字符串字面量、注释与分号。
func TestSplitSQLStatements(t *testing.T) {
	script := `-- 行注释; 不应被当作分隔符
CREATE TABLE demo (id INTEGER PRIMARY KEY, note TEXT DEFAULT 'a;b');
/* 块注释;
   跨行 */
INSERT INTO demo (note) VALUES ('it''s ok');
`
	statements := splitSQLStatements(script)
	if len(statements) != 2 {
		t.Fatalf("语句条数应为 2，实际 %d: %#v", len(statements), statements)
	}
	if !strings.HasPrefix(statements[0], "CREATE TABLE demo") {
		t.Fatalf("第一条语句异常: %q", statements[0])
	}
	if !strings.Contains(statements[1], "it''s ok") {
		t.Fatalf("第二条语句应保留转义引号: %q", statements[1])
	}
}
