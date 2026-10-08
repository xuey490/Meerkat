package dto

import (
	"strings"

	"github.com/company/monitor-webserver/internal/model"
)

// DictTypeItem 对齐前端 DictTypeItem（字典类型列表项）。
type DictTypeItem struct {
	// ID 字典类型 ID。
	ID string `json:"id"`
	// Name 字典名称。
	Name string `json:"name"`
	// DictCode 字典编码。
	DictCode string `json:"dictCode"`
	// Status 状态。
	Status int `json:"status"`
	// Remark 备注。
	Remark string `json:"remark"`
}

// DictTypeForm 对齐前端 DictTypeForm。
type DictTypeForm struct {
	// ID 字典类型 ID。
	ID string `json:"id"`
	// Name 字典名称。
	Name string `json:"name"`
	// DictCode 字典编码。
	DictCode string `json:"dictCode"`
	// Status 状态。
	Status int `json:"status"`
	// Remark 备注。
	Remark string `json:"remark"`
}

// DictItem 对齐前端 DictItem（字典项列表项）。
type DictItem struct {
	// ID 字典项 ID。
	ID string `json:"id"`
	// DictCode 所属字典编码。
	DictCode string `json:"dictCode"`
	// Label 字典标签。
	Label string `json:"label"`
	// Value 字典值。
	Value string `json:"value"`
	// Status 状态。
	Status int `json:"status"`
	// Sort 排序。
	Sort int `json:"sort"`
	// TagType 标签样式（前端字面量）。
	TagType string `json:"tagType"`
	// Color 标签颜色。
	Color string `json:"color"`
	// Remark 备注。
	Remark string `json:"remark"`
}

// DictItemForm 对齐前端 DictItemForm（TagType 为前端枚举字面量，写入时转短码）。
type DictItemForm struct {
	// ID 字典项 ID。
	ID string `json:"id"`
	// DictCode 所属字典编码。
	DictCode string `json:"dictCode"`
	// Label 字典标签。
	Label string `json:"label"`
	// Value 字典值。
	Value string `json:"value"`
	// Status 状态。
	Status int `json:"status"`
	// Sort 排序。
	Sort int `json:"sort"`
	// TagType 标签样式（前端字面量）。
	TagType string `json:"tagType"`
}

// DictItemOption 对齐前端 DictItemOption（字典项下拉数据源）。
type DictItemOption struct {
	// Value 字典值。
	Value string `json:"value"`
	// Label 字典标签。
	Label string `json:"label"`
	// TagType 标签样式（前端字面量）。
	TagType string `json:"tagType,omitempty"`
}

// DictQuery 是字典类型分页查询参数。
type DictQuery struct {
	// PageQuery 分页参数。
	PageQuery
	// Keywords 关键字（字典名称/编码）。
	Keywords string `json:"keywords" form:"keywords"`
}

// DictItemQuery 是字典项分页查询参数。
type DictItemQuery struct {
	// PageQuery 分页参数。
	PageQuery
	// Keywords 关键字（标签/值）。
	Keywords string `json:"keywords" form:"keywords"`
}

// NewDictTypeItem 把字典类型实体转换为前端列表项。
//
// 参数 Parameters:
//   - row (*model.SysDictType): 字典类型实体。
//
// 返回 Returns:
//   - item (DictTypeItem): 字典类型列表项。
func NewDictTypeItem(row *model.SysDictType) DictTypeItem {
	return DictTypeItem{
		ID:       textID(row.ID),
		Name:     textValue(row.Name),
		DictCode: textValue(row.Code),
		Status:   row.Status,
		Remark:   textValue(row.Remark),
	}
}

// NewDictItem 把字典数据实体转换为前端列表项。
//
// 参数 Parameters:
//   - row (*model.SysDictData): 字典数据实体。
//
// 返回 Returns:
//   - item (DictItem): 字典项列表项。
func NewDictItem(row *model.SysDictData) DictItem {
	return DictItem{
		ID:       textID(row.ID),
		DictCode: textValue(row.Code),
		Label:    textValue(row.Label),
		Value:    textValue(row.Value),
		Status:   row.Status,
		Sort:     row.Sort,
		TagType:  DictTagTypeFromCode(textValue(row.TagType)),
		Color:    textValue(row.Color),
		Remark:   textValue(row.Remark),
	}
}

// NewDictItemOption 把字典数据实体转换为下拉选项。
//
// 参数 Parameters:
//   - row (*model.SysDictData): 字典数据实体。
//
// 返回 Returns:
//   - option (DictItemOption): 字典项下拉数据。
func NewDictItemOption(row *model.SysDictData) DictItemOption {
	return DictItemOption{
		Value:   textValue(row.Value),
		Label:   textValue(row.Label),
		TagType: DictTagTypeFromCode(textValue(row.TagType)),
	}
}

// DictTagTypeToCode 把前端标签类型字面量编码为数据库短码。
//
// 参数 Parameters:
//   - value (string): primary/success/warning/info/danger/"" 等。
//
// 返回 Returns:
//   - code (string): N/P/S/W/I/D 之一。
func DictTagTypeToCode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "primary":
		return "P"
	case "success":
		return "S"
	case "warning":
		return "W"
	case "info":
		return "I"
	case "danger", "error":
		return "D"
	default:
		return "N"
	}
}

// DictTagTypeFromCode 把数据库短码解码为前端标签类型字面量。
//
// 参数 Parameters:
//   - code (string): N/P/S/W/I/D 之一。
//
// 返回 Returns:
//   - value (string): 前端可用的 el-tag type；未知返回空串。
func DictTagTypeFromCode(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "P":
		return "primary"
	case "S":
		return "success"
	case "W":
		return "warning"
	case "I":
		return "info"
	case "D":
		return "danger"
	default:
		return ""
	}
}
