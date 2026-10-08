package logic

import "github.com/company/monitor-webserver/internal/apperr"

// errInvalid 构造参数/校验类业务错误，供各领域操作复用。
//
// 参数 Parameters:
//   - message (string): 提示信息。
//
// 返回 Returns:
//   - err (*apperr.Error): 参数错误。
func errInvalid(message string) *apperr.Error { return apperr.Invalid(message) }

// errConflict 构造冲突类业务错误（如编码重复）。
//
// 参数 Parameters:
//   - message (string): 提示信息。
//
// 返回 Returns:
//   - err (*apperr.Error): 冲突错误。
func errConflict(message string) *apperr.Error { return apperr.Conflict(message) }
