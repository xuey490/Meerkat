// Package dto 定义 HTTP 请求参数与响应视图对象（VO），并承载
// 「数据库实体 ↔ 前端契约」的唯一翻译点。
//
// 契约事实来源：
//   - 数据库列名以 database/init.sql 为准（realname/phone/slug/is_hidden/type=1..4）；
//   - 前端字段以 web/src/api/system/*/types.ts 为准（nickname/mobile/perm/visible/type=C..E）。
//
// 本包只做结构定义与纯转换，不访问数据库、不引用 gin。
package dto

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// OptionItem 是前端通用下拉结构（label + value + 可选 children）。
type OptionItem struct {
	// Value 选项值。
	Value string `json:"value"`
	// Label 选项文本。
	Label string `json:"label"`
	// Children 子选项，用于下拉树。
	Children []OptionItem `json:"children,omitempty"`
}

// PageResult 是前后端统一的分页返回结构（对应前端 PageResult）。
type PageResult[T any] struct {
	// List 当前页数据。
	List []T `json:"list"`
	// Total 记录总数。
	Total int64 `json:"total"`
}

// PageQuery 是分页查询参数基类，供各模块查询 DTO 内嵌。
type PageQuery struct {
	// PageNum 页码，从 1 开始。
	PageNum int `json:"pageNum" form:"pageNum"`
	// PageSize 每页条数，上限 100。
	PageSize int `json:"pageSize" form:"pageSize"`
}

// FlagValue 兼容前端「数字 0/1」与「布尔 true/false」两种写法。
//
// 前端 MenuItem/MenuForm 中 alwaysShow、keepAlive 的类型为 number | boolean，
// 直接绑定到 int 会在提交布尔值时解析失败，因此统一用本类型承接。
type FlagValue int

// UnmarshalJSON 解析 JSON 中的 0/1、true/false 与 null。
//
// 参数 Parameters:
//   - data ([]byte): JSON 原始片段。
//
// 返回 Returns:
//   - err (error): 始终返回 nil；无法识别的取值按 0 处理，避免因表单细节导致请求失败。
func (f *FlagValue) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	switch raw {
	case "", "null":
		*f = 0
	case "true":
		*f = 1
	case "false":
		*f = 0
	default:
		value, err := strconv.Atoi(strings.Trim(raw, `"`))
		if err != nil {
			*f = 0
			return nil
		}
		*f = FlagValue(value)
	}
	return nil
}

// MarshalJSON 序列化为数字，保持与前端表格列的判断逻辑一致。
//
// 返回 Returns:
//   - data ([]byte): 数字字面量。
//   - err (error): 始终返回 nil。
func (f FlagValue) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Itoa(int(f))), nil
}

// DataScopeLabels 是数据权限范围的中文标签，键为 sa_system_role.data_scope 取值。
var DataScopeLabels = map[int]string{
	1: "全部数据",
	2: "本部门及下属数据",
	3: "本部门数据",
	4: "仅本人数据",
	5: "自定义部门数据",
}

// statusEnabled 表示启用状态的通用取值（1 启用 / 0 禁用）。
const (
	statusEnabled  = 1
	statusDisabled = 0
	// flagYes 对应 init.sql 中「是」的取值。
	flagYes = 1
	// flagNo 对应 init.sql 中「否」的取值。
	flagNo = 2
)

// textID 把数据库主键转换为前端字符串 ID。
//
// 参数 Parameters:
//   - id (int64): 主键。
//
// 返回 Returns:
//   - value (string): 十进制字符串。
func textID(id int64) string { return strconv.FormatInt(id, 10) }

// textIDs 把主键列表转换为字符串列表。
//
// 参数 Parameters:
//   - ids ([]int64): 主键列表。
//
// 返回 Returns:
//   - values ([]string): 字符串列表；入参为空时返回空切片。
func textIDs(ids []int64) []string {
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		values = append(values, textID(id))
	}
	return values
}

// textValue 安全解引用可空字符串。
//
// 参数 Parameters:
//   - value (*string): 可空字符串。
//
// 返回 Returns:
//   - text (string): nil 时返回空串。
func textValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// intOf 安全解引用可空整数。
//
// 参数 Parameters:
//   - value (*int): 可空整数。
//
// 返回 Returns:
//   - result (int): nil 时返回 0。
func intOf(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

// int64Of 安全解引用可空整数（int64）。
//
// 参数 Parameters:
//   - value (*int64): 可空整数。
//
// 返回 Returns:
//   - result (int64): nil 时返回 0。
func int64Of(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// flagBool 把 init.sql 的 1 是 / 2 否 标记转换为布尔值。
//
// 参数 Parameters:
//   - flag (int): 1 是 / 2 否。
//
// 返回 Returns:
//   - enabled (bool): 1 时返回 true。
func flagBool(flag int) bool { return flag == flagYes }

// parseIDText 解析逗号分隔的主键文本。
//
// 参数 Parameters:
//   - raw (string): 形如 "1,2,3" 的文本。
//
// 返回 Returns:
//   - ids ([]int64): 解析结果；非法片段被跳过。
func parseIDText(raw string) []int64 {
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
	ids := make([]int64, 0, len(fields))
	for _, field := range fields {
		value, err := strconv.ParseInt(strings.TrimSpace(field), 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, value)
	}
	return ids
}

// FlexInt 兼容 JSON 中「数字 / 字符串 / 布尔」三种写法的整数字段。
//
// 为什么需要它：字典下拉（DictSelect）会把字典项原始值直接写回表单，而
// sa_system_dict_data.value 是字符串，于是性别、公告类型这类由字典驱动的字段
// 在请求体里是 "1" 而不是 1；el-switch 之类的控件则提交数字或布尔。
// 若字段直接声明为 int，ShouldBindJSON 会失败并返回「请求参数格式错误」。
//
// 序列化时统一输出数字：状态类字段回显给 el-switch（active-value=1）需要数字，
// 而 DictSelect 内部用 String(option.value) === String(value) 比较，数字同样能正确回显。
type FlexInt int

// UnmarshalJSON 解析 1、"1"、true、null 等写法；无法识别时按 0 处理。
//
// 参数 Parameters:
//   - data ([]byte): JSON 原始片段。
//
// 返回 Returns:
//   - err (error): 始终返回 nil，避免因单个字段格式导致整个表单提交失败。
func (f *FlexInt) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	switch raw {
	case "", "null":
		*f = 0
		return nil
	case "true":
		*f = 1
		return nil
	case "false":
		*f = 0
		return nil
	}
	raw = strings.Trim(raw, `"`)
	if raw == "" {
		*f = 0
		return nil
	}
	if value, err := strconv.Atoi(raw); err == nil {
		*f = FlexInt(value)
		return nil
	}
	// 兼容 "1.0" 这类浮点写法，取整数部分。
	if value, err := strconv.ParseFloat(raw, 64); err == nil {
		*f = FlexInt(int(value))
		return nil
	}
	*f = 0
	return nil
}

// MarshalJSON 输出数字字面量。
//
// 返回 Returns:
//   - data ([]byte): 十进制数字。
//   - err (error): 始终返回 nil。
func (f FlexInt) MarshalJSON() ([]byte, error) { return []byte(strconv.Itoa(int(f))), nil }

// FlexStrings 是兼容「数字数组 / 字符串数组 / 混合数组 / 逗号分隔字符串 / 单个值」的 ID 列表。
//
// 为什么需要它：前端 UserForm.roleIds 声明为 number[]（如 [3]），岗位/角色选择器
// 提交的却可能是 ["3"]；后端若声明为 []string，[3] 会直接导致绑定失败。
type FlexStrings []string

// UnmarshalJSON 解析 [3]、["3"]、[3,"4"]、3、"3"、"3,4"、null 等写法。
//
// 参数 Parameters:
//   - data ([]byte): JSON 原始片段。
//
// 返回 Returns:
//   - err (error): 数组结构非法时返回非 nil，其余情况一律降级为空列表。
func (f *FlexStrings) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		var items []json.RawMessage
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			return err
		}
		values := make([]string, 0, len(items))
		for _, item := range items {
			text := strings.Trim(strings.TrimSpace(string(item)), `"`)
			if text == "" || text == "null" {
				continue
			}
			values = append(values, text)
		}
		*f = values
		return nil
	}
	values := make([]string, 0, 4)
	for _, part := range strings.Split(strings.Trim(raw, `"`), ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	if len(values) == 0 {
		return nil
	}
	*f = values
	return nil
}

// IDs 把 ID 文本列表解析为主键列表，非法片段自动跳过。
//
// 返回 Returns:
//   - ids ([]int64): 主键列表。
func (f FlexStrings) IDs() []int64 { return parseIDText(strings.Join(f, ",")) }

// timeText 把可空时间格式化为前端展示文本。
//
// 参数 Parameters:
//   - value (*time.Time): 可空时间。
//
// 返回 Returns:
//   - text (string): 形如 2026-10-01 12:00:00；nil 时返回空串。
func timeText(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02 15:04:05")
}
