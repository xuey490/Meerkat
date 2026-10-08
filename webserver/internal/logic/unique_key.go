package logic

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/apperr"
)

// deletedKeyMarker 是业务唯一键在软删除后的后缀标记。
//
// 背景：sa_system_user.username、sa_system_role.code、sa_system_dict_type.code、
// sa_system_config.key 都建了唯一索引（见 app/sql/001_schema.sql），而删除走软删除，
// 只写 delete_time。唯一索引不区分 delete_time，被删除的行仍占着这个键，
// 于是同名的用户 / 角色 / 字典 / 配置再也创建不出来（表现为 500「创建用户失败」）。
//
// 处理方式：软删除时把业务键改写为「<原值>#deleted#<主键>」，
// 既不丢审计行，又立即释放键位；启动时再对历史软删除行做一次同样的订正。
const deletedKeyMarker = "#deleted#"

// ConflictErr 构造「资源冲突」的业务错误。
//
// 参数 Parameters:
//   - message (string): 提示信息，如 "用户名已存在"。
//
// 返回 Returns:
//   - err (*apperr.Error): 业务错误。
func ConflictErr(message string) *apperr.Error { return apperr.Conflict(message) }

// IsUniqueViolation 判断错误是否为唯一约束冲突。
//
// 参数 Parameters:
//   - err (error): 数据库返回的错误。
//
// 返回 Returns:
//   - yes (bool): 唯一索引冲突时为 true。
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") && strings.Contains(message, "constraint")
}

// UniqueOrInternal 把唯一约束冲突转换为可读的业务错误，其余错误按内部错误包装。
//
// 为什么需要它：唯一索引包含软删除行，任何绕过前置校验的写入（并发、导入、
// 历史脏数据）都会以约束错误形式失败；若不转换，前端只会看到「创建用户失败」这类
// 500 提示，无法判断真实原因。
//
// 参数 Parameters:
//   - err (error): 数据库返回的错误。
//   - conflictMessage (string): 唯一冲突时返回给前端的提示。
//   - internalMessage (string): 其他错误时返回给前端的提示。
//
// 返回 Returns:
//   - err (error): 业务错误；入参为 nil 时返回 nil。
func UniqueOrInternal(err error, conflictMessage, internalMessage string) error {
	if err == nil {
		return nil
	}
	if IsUniqueViolation(err) {
		return ConflictErr(conflictMessage)
	}
	return InternalErr(internalMessage, err)
}

// FreeSoftDeletedKeys 订正历史数据：把已软删除行的业务唯一键改写为带标记的形式。
//
// 幂等：只处理键中不含标记的行，重复执行不会二次改写。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话（SQLite）。
//   - table (string): 表名，仅允许内部常量传入。
//   - keyColumn (string): 业务唯一键列名；需要转义的列名请自行带引号（如 `"key"`）。
//
// 返回 Returns:
//   - err (error): 执行失败时返回原始错误。
func FreeSoftDeletedKeys(db *gorm.DB, table, keyColumn string) error {
	statement := "UPDATE " + table +
		" SET " + keyColumn + " = " + keyColumn + " || ? || id" +
		" WHERE delete_time IS NOT NULL AND instr(" + keyColumn + ", ?) = 0"
	return db.Exec(statement, deletedKeyMarker, deletedKeyMarker).Error
}

// softDeleteFreeKey 软删除指定主键，并在同一条语句里释放业务唯一键。
//
// 参数 Parameters:
//   - tx (*gorm.DB): 事务会话。
//   - table (string): 表名，仅允许内部常量传入。
//   - keyColumn (string): 业务唯一键列名。
//   - ids ([]int64): 待删除的主键列表。
//
// 返回 Returns:
//   - err (error): 执行失败时返回原始错误；ids 为空时返回 nil。
func softDeleteFreeKey(tx *gorm.DB, table, keyColumn string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	statement := "UPDATE " + table +
		" SET delete_time = ?, " + keyColumn + " = " + keyColumn + " || ? || id" +
		" WHERE id IN ? AND delete_time IS NULL"
	return tx.Exec(statement, time.Now(), deletedKeyMarker, ids).Error
}
