package app

import (
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/logic"
	"github.com/company/monitor-webserver/internal/model"
)

// seedFS 内嵌权限库的建表与种子 SQL。
//
// 放在 internal/app/sql 下并随二进制发布，保证部署时无需额外拷贝 SQL 文件。
//
//go:embed sql/*.sql
var seedFS embed.FS

// seedFiles 是按依赖顺序执行的 SQL 文件清单。
//
// 001 建表与清理旧 web_* 表；002 写入监控与系统管理菜单及角色授权；
// 003 写入租户、角色、部门、岗位、字典与配置等基础数据。
var seedFiles = []string{
	"sql/001_schema.sql",
	"sql/002_menu_seed.sql",
	"sql/003_system_seed.sql",
}

// superAdminRoleCode 是内置超级管理员角色编码，与 003_system_seed.sql 保持一致。
const superAdminRoleCode = "super_admin"

// defaultAdminUsername 是内置管理员账号。
const defaultAdminUsername = "admin"

// defaultAdminPassword 是未配置 bootstrap_admin_password 时使用的初始密码。
//
// 与前端登录页预置的演示账号密码保持一致，便于首次部署直接登录。
const defaultAdminPassword = "Monitor123!"

// migrate 执行权限库的结构迁移与种子数据写入（幂等，可随每次启动重复执行）。
//
// 返回 Returns:
//   - err (error): SQL 执行失败或管理员账号初始化失败时返回非 nil。
//
// 注意 Side Effects：会删除模板遗留表 web_users/web_roles/web_menus 等，
// 并在数据库中写入 sa_system_* 表结构与基础数据。
func (a *App) migrate() error {
	cfg := a.cfg
	return Migrate(a.webDB, cfg.BootstrapAdminPassword)
}

// Migrate 执行权限库迁移与种子，并按需维护内置管理员账号。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//   - bootstrapAdminPassword (string): 非空时用其重置 admin 密码。
//
// 返回 Returns:
//   - err (error): SQL 执行失败或管理员账号初始化失败时返回非 nil。
func Migrate(db *gorm.DB, bootstrapAdminPassword string) error {
	for _, name := range seedFiles {
		content, err := seedFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read seed file %s: %w", name, err)
		}
		statements := splitSQLStatements(string(content))
		if err := db.Transaction(func(tx *gorm.DB) error {
			for _, statement := range statements {
				if err := tx.Exec(statement).Error; err != nil {
					return fmt.Errorf("exec %s: %w", name, err)
				}
			}
			return nil
		}); err != nil {
			return err
		}
		slog.Debug("webserver 权限库脚本执行完成", "file", name, "statements", len(statements))
	}
	if err := fixSoftDeletedKeys(db); err != nil {
		return err
	}
	if err := ensureSessionColumns(db); err != nil {
		return err
	}
	return seedAdmin(db, bootstrapAdminPassword)
}

// sessionColumns 是 sa_system_refresh_token 的会话元信息扩展列。
//
// 新建库由 001_schema.sql 直接建好；老库需要 ALTER TABLE 补齐（SQLite 无 ADD COLUMN IF NOT EXISTS）。
var sessionColumns = []struct {
	// name 列名。
	name string
	// ddl 补列语句。
	ddl string
}{
	{"ip", "ALTER TABLE sa_system_refresh_token ADD COLUMN ip VARCHAR(45)"},
	{"ip_location", "ALTER TABLE sa_system_refresh_token ADD COLUMN ip_location VARCHAR(255)"},
	{"device", "ALTER TABLE sa_system_refresh_token ADD COLUMN device VARCHAR(32)"},
	{"os", "ALTER TABLE sa_system_refresh_token ADD COLUMN os VARCHAR(50)"},
	{"browser", "ALTER TABLE sa_system_refresh_token ADD COLUMN browser VARCHAR(50)"},
	{"last_active_at", "ALTER TABLE sa_system_refresh_token ADD COLUMN last_active_at DATETIME"},
}

// ensureSessionColumns 为已有库补齐会话元信息列（幂等：先查 PRAGMA 再补）。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//
// 返回 Returns:
//   - err (error): 查询列信息或补列失败时返回非 nil。
func ensureSessionColumns(db *gorm.DB) error {
	existing, err := tableColumns(db, "sa_system_refresh_token")
	if err != nil {
		return err
	}
	for _, column := range sessionColumns {
		if existing[column.name] {
			continue
		}
		if err := db.Exec(column.ddl).Error; err != nil {
			return fmt.Errorf("add column %s: %w", column.name, err)
		}
	}
	return nil
}

// tableColumns 返回指定表的列名集合。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//   - table (string): 表名（内部常量，不接受外部输入）。
//
// 返回 Returns:
//   - columns (map[string]bool): 列名集合。
//   - err (error): 查询失败时返回非 nil。
func tableColumns(db *gorm.DB, table string) (map[string]bool, error) {
	rows, err := db.Raw("PRAGMA table_info(" + table + ")").Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	columns := make(map[string]bool, 16)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

// fixSoftDeletedKeys 订正历史数据：释放被软删除行占用的业务唯一键。
//
// 为什么必须有这一步：username / code / key 上的唯一索引不含 delete_time，
// 历史软删除行会永久占用这些键，导致同名用户、同编码角色/字典、同键配置再也无法创建
// （典型症状是前端提示「创建用户失败」）。此处把历史行的键改写成
// `<原值>#deleted#<主键>`，键位立即释放；幂等，可随每次启动执行。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//
// 返回 Returns:
//   - err (error): 任一步骤失败时返回非 nil。
func fixSoftDeletedKeys(db *gorm.DB) error {
	targets := []struct {
		table  string
		column string
	}{
		{"sa_system_user", "username"},
		{"sa_system_role", "code"},
		{"sa_system_dict_type", "code"},
		{"sa_system_config", `"key"`},
	}
	for _, target := range targets {
		if err := logic.FreeSoftDeletedKeys(db, target.table, target.column); err != nil {
			return fmt.Errorf("fix soft-deleted key %s.%s: %w", target.table, target.column, err)
		}
	}
	return nil
}

// seedAdmin 保证存在可登录的超级管理员账号，并补齐用户关联数据。
//
// 密码策略：始终使用 Go bcrypt 现场生成（init.sql 中的 PHP $2y$ 哈希无法复用于登录）。
// 当配置了 bootstrap_admin_password 时，会用它重置已有 admin 的密码，便于运维找回入口。
//
// 返回 Returns:
//   - err (error): 任一写库操作失败时返回非 nil。
func seedAdmin(db *gorm.DB, bootstrapAdminPassword string) error {
	var role model.SysRole
	if err := db.Where("code = ?", superAdminRoleCode).First(&role).Error; err != nil {
		return fmt.Errorf("load role %s: %w", superAdminRoleCode, err)
	}
	password := strings.TrimSpace(bootstrapAdminPassword)
	if password == "" {
		password = defaultAdminPassword
	}
	var user model.SysUser
	err := db.Where("username = ?", defaultAdminUsername).First(&user).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if hashErr != nil {
			return hashErr
		}
		deptID := int64(1)
		now := time.Now()
		user = model.SysUser{
			Username:  defaultAdminUsername,
			Password:  string(hash),
			Realname:  "系统管理员",
			Gender:    "1",
			Avatar:    "",
			Dashboard: "work",
			DeptID:    &deptID,
			IsSuper:   1,
			Status:    1,
			AuditFields: model.AuditFields{
				CreatedBy:  &deptID,
				UpdatedBy:  &deptID,
				CreateTime: &now,
				UpdateTime: &now,
			},
		}
		if createErr := db.Create(&user).Error; createErr != nil {
			return fmt.Errorf("create admin user: %w", createErr)
		}
		slog.Info("webserver 已创建初始管理员账号", "username", defaultAdminUsername,
			"passwordSource", passwordSource(bootstrapAdminPassword))
	case err != nil:
		return fmt.Errorf("load admin user: %w", err)
	default:
		if strings.TrimSpace(bootstrapAdminPassword) != "" {
			hash, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if hashErr != nil {
				return hashErr
			}
			if updateErr := db.Model(&model.SysUser{}).Where("id = ?", user.ID).Updates(map[string]any{
				"password":    string(hash),
				"update_time": time.Now(),
			}).Error; updateErr != nil {
				return fmt.Errorf("reset admin password: %w", updateErr)
			}
			slog.Warn("webserver 已按 bootstrap_admin_password 重置管理员密码", "username", defaultAdminUsername)
		}
	}

	deptID := int64(1)
	if user.DeptID == nil {
		if err := db.Model(&model.SysUser{}).Where("id = ?", user.ID).Updates(map[string]any{
			"dept_id": deptID, "is_super": 1, "update_time": time.Now(),
		}).Error; err != nil {
			return fmt.Errorf("normalize admin user: %w", err)
		}
	}
	relations := []string{
		`INSERT OR IGNORE INTO sa_system_user_role (user_id, role_id, status, tenant_id, created_by, updated_by, create_time, update_time)
		 VALUES (?, ?, 1, 0, ?, ?, datetime('now','localtime'), datetime('now','localtime'))`,
		`INSERT OR IGNORE INTO sa_system_user_dept (tenant_id, user_id, dept_id, created_by, updated_by, create_time, update_time)
		 VALUES (0, ?, ?, ?, ?, datetime('now','localtime'), datetime('now','localtime'))`,
		`INSERT OR IGNORE INTO sa_system_user_post (user_id, post_id, status, created_by, updated_by, tenant_id, create_time, update_time)
		 VALUES (?, 1, 1, ?, ?, 0, datetime('now','localtime'), datetime('now','localtime'))`,
		`INSERT OR IGNORE INTO sa_system_user_tenant (user_id, tenant_id, is_default, join_time, is_super, created_by, updated_by, create_time, update_time, status)
		 VALUES (?, 1, 1, datetime('now','localtime'), 1, ?, ?, datetime('now','localtime'), datetime('now','localtime'), 1)`,
	}
	args := [][]any{
		{user.ID, role.ID, user.ID, user.ID},
		{user.ID, deptID, user.ID, user.ID},
		{user.ID, user.ID, user.ID},
		{user.ID, user.ID, user.ID},
	}
	for index, statement := range relations {
		if err := db.Exec(statement, args[index]...).Error; err != nil {
			return fmt.Errorf("seed admin relation %d: %w", index, err)
		}
	}
	return nil
}

// passwordSource 描述管理员初始密码的来源，仅用于日志（不打印密码本身）。
//
// 参数 Parameters:
//   - configured (string): 配置中的 bootstrap_admin_password。
//
// 返回 Returns:
//   - source (string): "config" 表示来自配置，"default" 表示使用内置默认值。
func passwordSource(configured string) string {
	if strings.TrimSpace(configured) != "" {
		return "config"
	}
	return "default"
}

// splitSQLStatements 把 SQL 脚本拆分为可逐条执行的语句。
//
// 需要处理的边界：字符串字面量中的分号、行注释（-- 到行尾）与块注释（/* */）。
//
// 参数 Parameters:
//   - script (string): 完整 SQL 脚本内容。
//
// 返回 Returns:
//   - statements ([]string): 去除注释与空白后的语句列表。
func splitSQLStatements(script string) []string {
	statements := make([]string, 0, 32)
	var builder strings.Builder
	runes := []rune(script)
	inSingleQuote := false
	inBlockComment := false
	inLineComment := false
	for index := 0; index < len(runes); index++ {
		char := runes[index]
		next := rune(0)
		if index+1 < len(runes) {
			next = runes[index+1]
		}
		switch {
		case inLineComment:
			if char == '\n' {
				inLineComment = false
				builder.WriteRune(char)
			}
			continue
		case inBlockComment:
			if char == '*' && next == '/' {
				inBlockComment = false
				index++
			}
			continue
		case inSingleQuote:
			builder.WriteRune(char)
			if char == '\'' {
				// SQL 中的 '' 表示转义的单引号，需要连续判断避免误判结束。
				if next == '\'' {
					builder.WriteRune(next)
					index++
					continue
				}
				inSingleQuote = false
			}
			continue
		}
		switch {
		case char == '-' && next == '-':
			inLineComment = true
			index++
		case char == '/' && next == '*':
			inBlockComment = true
			index++
		case char == '\'':
			inSingleQuote = true
			builder.WriteRune(char)
		case char == ';':
			if statement := strings.TrimSpace(builder.String()); statement != "" {
				statements = append(statements, statement)
			}
			builder.Reset()
		default:
			builder.WriteRune(char)
		}
	}
	if statement := strings.TrimSpace(builder.String()); statement != "" {
		statements = append(statements, statement)
	}
	return statements
}
