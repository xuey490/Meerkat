// Package util 提供无业务语义的通用工具：文本、时间、分页、ID 解析、User-Agent 解析等。
//
// 本包只依赖标准库，不引用 gin / gorm，供 logic、service、api 各层共用，
// 避免同一段小逻辑在多处重复实现。
package util

import (
	"strconv"
	"strings"
)

// TextID 把数据库主键转换为前端契约使用的字符串 ID。
//
// 参数 Parameters:
//   - id (int64): 数据库主键。
//
// 返回 Returns:
//   - value (string): 十进制字符串。
func TextID(id int64) string { return strconv.FormatInt(id, 10) }

// TextIDs 把主键列表转换为字符串列表。
//
// 参数 Parameters:
//   - ids ([]int64): 数据库主键列表。
//
// 返回 Returns:
//   - values ([]string): 同序字符串列表；入参为空时返回空切片（非 nil）。
func TextIDs(ids []int64) []string {
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		values = append(values, strconv.FormatInt(id, 10))
	}
	return values
}

// EmptyInt64Slice 返回非 nil 的空 int64 切片，保证 JSON 序列化为 [] 而不是 null。
//
// 返回 Returns:
//   - values ([]int64): 长度为 0 的切片。
func EmptyInt64Slice() []int64 { return make([]int64, 0) }

// ParseIDList 解析形如 "1,2,3" 的 ID 列表。
//
// 前端批量删除接口用逗号分隔的路径参数传 ID；非法片段与小于等于 0 的值会被忽略。
//
// 参数 Parameters:
//   - raw (string): 逗号分隔的 ID 字符串。
//
// 返回 Returns:
//   - ids ([]int64): 解析结果（未去重）。
func ParseIDList(raw string) []int64 {
	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || value <= 0 {
			continue
		}
		ids = append(ids, value)
	}
	return ids
}

// LikeKeyword 把用户输入包装成 SQL LIKE 模式，并转义 % 与 _ 通配符。
//
// 参数 Parameters:
//   - keyword (string): 用户输入的关键字。
//
// 返回 Returns:
//   - pattern (string): 形如 "%abc%" 的模式；输入为空时返回空串。
func LikeKeyword(keyword string) string {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return ""
	}
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(keyword) + "%"
}

// TextValue 安全解引用可能为 nil 的字符串。
//
// 参数 Parameters:
//   - value (*string): 待解引用的指针。
//
// 返回 Returns:
//   - text (string): nil 时返回空串。
func TextValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// IntValue 安全解引用可能为 nil 的整数。
//
// 参数 Parameters:
//   - value (*int): 待解引用的指针。
//
// 返回 Returns:
//   - result (int): nil 时返回 0。
func IntValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

// Int64Value 安全解引用可能为 nil 的 int64。
//
// 参数 Parameters:
//   - value (*int64): 待解引用的指针。
//
// 返回 Returns:
//   - result (int64): nil 时返回 0。
func Int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// NonEmptyPtr 把可能为空的字符串转换为指针，空串返回 nil（数据库写入 NULL）。
//
// 参数 Parameters:
//   - value (string): 原始字符串。
//
// 返回 Returns:
//   - ptr (*string): 去空格后非空时返回指针，否则返回 nil。
func NonEmptyPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// Int64Ptr 把可能为 0 的整数转换为指针，0 返回 nil（数据库写入 NULL）。
//
// 参数 Parameters:
//   - value (int64): 原始整数。
//
// 返回 Returns:
//   - ptr (*int64): 大于 0 时返回指针，否则返回 nil。
func Int64Ptr(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

// AtoiSafe 把字符串安全转换为整数，失败时返回兜底值。
//
// 参数 Parameters:
//   - value (string): 原始字符串。
//   - fallback (int): 解析失败时的兜底值。
//
// 返回 Returns:
//   - result (int): 解析结果或兜底值。
func AtoiSafe(value string, fallback int) int {
	if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		return parsed
	}
	return fallback
}

// 通用「是否」取值：与 init.sql 中 is_hidden / is_keep_alive 等列一致。
const (
	// FlagYes 是。
	FlagYes = 1
	// FlagNo 否。
	FlagNo = 2
)

// BoolFlag 把布尔转换为 init.sql 的 1 是 / 2 否 标记。
//
// 参数 Parameters:
//   - value (bool): true 表示「是」。
//
// 返回 Returns:
//   - flag (int): 1 或 2。
func BoolFlag(value bool) int {
	if value {
		return FlagYes
	}
	return FlagNo
}

// FlagBool 把 init.sql 的 1 是 / 2 否 标记转换为布尔。
//
// 参数 Parameters:
//   - flag (int): 列值。
//
// 返回 Returns:
//   - value (bool): 等于 1 时返回 true。
func FlagBool(flag int) bool { return flag == FlagYes }

// NormalizeStatus 收敛前端提交的状态值，非法值按启用处理。
//
// 参数 Parameters:
//   - status (int): 前端提交的状态（1 启用 / 0 禁用）。
//
// 返回 Returns:
//   - value (int): 0 或 1。
func NormalizeStatus(status int) int {
	if status == 0 {
		return 0
	}
	return 1
}
