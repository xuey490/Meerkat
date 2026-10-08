package dto

import "github.com/company/monitor-webserver/internal/model"

// PostItem 是岗位列表项（前端岗位页使用）。
type PostItem struct {
	// ID 岗位 ID。
	ID string `json:"id"`
	// Name 岗位名称。
	Name string `json:"name"`
	// Code 岗位编码。
	Code string `json:"code"`
	// Sort 排序。
	Sort int `json:"sort"`
	// Status 状态：1 正常 / 0 停用。
	Status int `json:"status"`
	// Remark 备注。
	Remark string `json:"remark"`
	// CreateTime 创建时间。
	CreateTime string `json:"createTime"`
}

// PostForm 是岗位表单。
type PostForm struct {
	// ID 岗位 ID。
	ID string `json:"id"`
	// Name 岗位名称。
	Name string `json:"name"`
	// Code 岗位编码。
	Code string `json:"code"`
	// Sort 排序。
	Sort int `json:"sort"`
	// Status 状态。
	Status int `json:"status"`
	// Remark 备注。
	Remark string `json:"remark"`
}

// PostQuery 是岗位分页查询参数。
type PostQuery struct {
	// PageQuery 分页参数。
	PageQuery
	// Keywords 关键字（岗位名称/编码）。
	Keywords string `json:"keywords" form:"keywords"`
	// Status 状态过滤。
	Status string `json:"status" form:"status"`
}

// NewPostItem 把岗位实体转换为前端列表项。
//
// 参数 Parameters:
//   - row (*model.SysPost): 岗位实体。
//
// 返回 Returns:
//   - item (PostItem): 岗位列表项。
func NewPostItem(row *model.SysPost) PostItem {
	return PostItem{
		ID:         textID(row.ID),
		Name:       textValue(row.Name),
		Code:       textValue(row.Code),
		Sort:       row.Sort,
		Status:     row.Status,
		Remark:     textValue(row.Remark),
		CreateTime: timeText(row.CreateTime),
	}
}

// NewPostForm 把岗位实体转换为编辑表单。
//
// 参数 Parameters:
//   - row (*model.SysPost): 岗位实体。
//
// 返回 Returns:
//   - form (PostForm): 岗位表单。
func NewPostForm(row *model.SysPost) PostForm {
	return PostForm{
		ID:     textID(row.ID),
		Name:   textValue(row.Name),
		Code:   textValue(row.Code),
		Sort:   row.Sort,
		Status: row.Status,
		Remark: textValue(row.Remark),
	}
}
