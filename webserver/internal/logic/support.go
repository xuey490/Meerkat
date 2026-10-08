// Package logic 是领域操作层：只做数据库读写与数据加工，不感知 HTTP。
//
// 约定：
//   - 入参为 dto 或基础类型，出参为 model 实体或基础类型，错误一律返回 apperr；
//   - 不认识 gin、不写响应、不做权限判断（权限属于 service 与 middleware）；
//   - 事务边界尽量小，跨表写入放在同一个事务里保证一致性。
package logic

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/model"
)

// NotFoundErr 构造「资源不存在」的业务错误。
//
// 参数 Parameters:
//   - message (string): 提示信息，如 "岗位不存在"。
//
// 返回 Returns:
//   - err (*apperr.Error): 业务错误。
func NotFoundErr(message string) *apperr.Error { return apperr.NotFound(message) }

// InternalErr 把数据库错误包装为内部错误，保留原始错误便于日志排查。
//
// 参数 Parameters:
//   - message (string): 面向用户的提示信息。
//   - cause (error): 底层错误。
//
// 返回 Returns:
//   - err (*apperr.Error): 业务错误。
func InternalErr(message string, cause error) *apperr.Error {
	return apperr.Internal(message, cause)
}

// TranslateDBError 把 GORM 错误翻译为业务错误：记录不存在走 NotFound，其余走 Internal。
//
// 参数 Parameters:
//   - err (error): GORM 返回的错误。
//   - notFoundMessage (string): 记录不存在时的提示信息。
//   - internalMessage (string): 其他错误的提示信息。
//
// 返回 Returns:
//   - err (error): 业务错误；入参为 nil 时返回 nil。
func TranslateDBError(err error, notFoundMessage, internalMessage string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperr.NotFound(notFoundMessage)
	}
	return apperr.Internal(internalMessage, err)
}

// Stamp 构造新增记录时的审计字段。
//
// 参数 Parameters:
//   - operator (*model.SysUser): 当前操作人，可为 nil。
//
// 返回 Returns:
//   - fields (model.AuditFields): 已填充创建人/更新人与时间。
func Stamp(operator *model.SysUser) model.AuditFields {
	now := time.Now()
	actor := ActorID(operator)
	return model.AuditFields{
		CreatedBy:  actor,
		UpdatedBy:  actor,
		CreateTime: &now,
		UpdateTime: &now,
	}
}

// Touch 构造更新记录时的审计字段（只改更新人与更新时间）。
//
// 参数 Parameters:
//   - operator (*model.SysUser): 当前操作人，可为 nil。
//
// 返回 Returns:
//   - updatedBy (*int64): 更新人 ID，无人操作时为指向 0 的指针（保证 NOT NULL 列可写入）。
//   - updateTime (*time.Time): 当前时间。
func Touch(operator *model.SysUser) (*int64, *time.Time) {
	now := time.Now()
	return ActorID(operator), &now
}

// ActorID 返回审计列的写入值：操作人存在时为其主键，否则为指向 0 的指针。
//
// 为什么不用 nil：sa_system_user_post / sa_system_user_tenant 等表的
// created_by、updated_by 是 `NOT NULL DEFAULT 0`，而 GORM 插入时会带上结构体里的
// 全部字段，显式写入 NULL 会直接触发 NOT NULL 约束失败（列默认值不会生效）。
// 因此审计字段一律给出确定的数值，需要用「未知」语义的场景改判 0 即可。
//
// 参数 Parameters:
//   - operator (*model.SysUser): 当前操作人，可为 nil。
//
// 返回 Returns:
//   - id (*int64): 非空指针（操作人缺失时指向 0）。
func ActorID(operator *model.SysUser) *int64 {
	if operator != nil && operator.ID > 0 {
		id := operator.ID
		return &id
	}
	anonymous := int64(0)
	return &anonymous
}

// OperatorID 返回操作人主键指针，便于写入可空审计列。
//
// 参数 Parameters:
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (*int64): 操作人 ID；操作人为空时返回 nil。
func OperatorID(operator *model.SysUser) *int64 {
	if operator == nil || operator.ID <= 0 {
		return nil
	}
	id := operator.ID
	return &id
}
