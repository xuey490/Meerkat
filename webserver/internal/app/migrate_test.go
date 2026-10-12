package app

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/testutil/pgtest"
)

// newTestDB 打开一个独立的 PostgreSQL 测试库（每个用例一个 schema，结束自动清理）。
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return pgtest.Open(t)
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

// TestMigrateAdvancesIdentitySequences 验证种子写入固定主键后，自增序列被推进到最大值。
//
// 这是迁到 PostgreSQL 最容易漏的坑：identity 序列不会因为显式写入了 id 而前进，
// 缺少校正时种子跑完后第一条真实 INSERT 会从 1 开始分配，直接撞上已占用的主键。
func TestMigrateAdvancesIdentitySequences(t *testing.T) {
	db := newTestDB(t)
	if err := Migrate(db, ""); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	// 覆盖「有种子数据」（menu/role/user）与「无种子数据」（login_log/notice）两类表：
	// 前者验证序列被推过已占用的 id，后者验证空表下序列仍从未使用的位置开始。
	for _, table := range []string{
		"sa_system_menu", "sa_system_role", "sa_system_user",
		"sa_system_login_log", "sa_system_notice",
	} {
		var nextID int64
		if err := db.Raw("SELECT nextval(pg_get_serial_sequence(?, 'id'))", table).Scan(&nextID).Error; err != nil {
			t.Fatalf("表 %s 读取自增序列失败: %v", table, err)
		}
		var maxID int64
		if err := db.Raw("SELECT COALESCE(MAX(id), 0) FROM " + table).Scan(&maxID).Error; err != nil {
			t.Fatalf("表 %s 查询最大 id 失败: %v", table, err)
		}
		if nextID <= maxID {
			t.Errorf("表 %s 自增序列未校正：下一个值 %d 未超过已占用的最大 id %d", table, nextID, maxID)
		}
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
