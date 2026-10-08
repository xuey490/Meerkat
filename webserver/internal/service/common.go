package service

import (
	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/util"
)

// requiredID 解析必填的单值主键，非法时返回参数错误。
//
// 参数 Parameters:
//   - idText (string): 路径参数中的 ID 文本。
//   - message (string): 非法时的提示信息，如 "岗位 ID 无效"。
//
// 返回 Returns:
//   - id (int64): 解析后的主键。
//   - err (error): 非法时返回业务错误。
func requiredID(idText, message string) (int64, error) {
	ids := util.ParseIDList(idText)
	if len(ids) == 0 {
		return 0, apperr.Invalid(message)
	}
	return ids[0], nil
}

// optionalID 解析可选的单值主键，非法或为空时返回 0。
//
// 参数 Parameters:
//   - idText (string): ID 文本。
//
// 返回 Returns:
//   - id (int64): 解析结果，空或非法时返回 0。
func optionalID(idText string) int64 {
	ids := util.ParseIDList(idText)
	if len(ids) == 0 {
		return 0
	}
	return ids[0]
}
