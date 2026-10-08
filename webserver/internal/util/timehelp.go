package util

import (
	"strings"
	"time"
)

// TimeLayout 是前端表格与详情统一使用的时间展示格式。
const TimeLayout = "2006-01-02 15:04:05"

// dateLayout 是日期选择器常用的纯日期格式。
const dateLayout = "2006-01-02"

// TimeText 把可空时间格式化为前端展示字符串。
//
// 参数 Parameters:
//   - value (*time.Time): 数据库时间列对应的值，可能为 nil。
//
// 返回 Returns:
//   - text (string): nil 或零值返回空串，否则按 TimeLayout 格式化。
func TimeText(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format(TimeLayout)
}

// TimeValueText 把非空时间格式化为前端展示字符串。
//
// 参数 Parameters:
//   - value (time.Time): 时间值。
//
// 返回 Returns:
//   - text (string): 零值返回空串。
func TimeValueText(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(TimeLayout)
}

// NowPtr 返回当前本地时间的指针，便于写入可空时间列。
//
// 返回 Returns:
//   - value (*time.Time): 指向当前时间的指针。
func NowPtr() *time.Time {
	now := time.Now()
	return &now
}

// TimePtr 返回给定时间的指针；入参为零值时返回 nil（写入 NULL）。
//
// 参数 Parameters:
//   - value (time.Time): 待包装的时间。
//
// 返回 Returns:
//   - ptr (*time.Time): 指针或 nil。
func TimePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	result := value
	return &result
}

// ParseQueryTime 解析单个时间参数，支持 "2006-01-02"、"2006-01-02 15:04:05" 与 RFC3339。
//
// 参数 Parameters:
//   - raw (string): 查询参数原始值。
//
// 返回 Returns:
//   - value (*time.Time): 解析成功返回指针，失败或为空返回 nil。
func ParseQueryTime(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	layouts := []string{TimeLayout, dateLayout, time.RFC3339}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return &parsed
		}
	}
	return nil
}

// ParseTimePair 解析「开始,结束」形式的时间范围参数。
//
// 前端用 qs 的 arrayFormat=repeat 序列化数组，同一字段可能出现两次，也可能出现带逗号的单值，
// 调用方负责把两种形式都摊平成切片后传入。
//
// 参数 Parameters:
//   - values ([]string): 时间范围原始值集合。
//
// 返回 Returns:
//   - start (*time.Time): 起始时间，未提供时为 nil。
//   - end (*time.Time): 结束时间，未提供时为 nil。
func ParseTimePair(values []string) (start, end *time.Time) {
	raw := make([]string, 0, len(values))
	for _, value := range values {
		raw = append(raw, strings.Split(value, ",")...)
	}
	clean := make([]string, 0, len(raw))
	for _, item := range raw {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	if len(clean) > 0 {
		start = ParseQueryTime(clean[0])
	}
	if len(clean) > 1 {
		end = ParseQueryTime(clean[1])
	}
	return start, end
}
