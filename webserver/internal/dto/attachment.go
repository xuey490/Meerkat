package dto

import (
	"github.com/company/monitor-webserver/internal/model"
)

// CategoryItem 是附件分类树节点（对齐前端 AttachmentAPI.getCategoryTree）。
type CategoryItem struct {
	// ID 分类 ID。
	ID string `json:"id"`
	// ParentID 父分类 ID，"0" 表示根分类。
	ParentID string `json:"parentId"`
	// Name 分类名称。
	Name string `json:"name"`
	// Sort 排序值。
	Sort int `json:"sort"`
	// Status 状态：1 启用 / 0 禁用。
	Status int `json:"status"`
	// Remark 备注。
	Remark string `json:"remark"`
	// CreateTime 创建时间。
	CreateTime string `json:"createTime"`
	// Children 子分类；叶子节点不输出该字段。
	Children []CategoryItem `json:"children,omitempty"`
}

// CategoryForm 对齐前端附件分类表单（新增与编辑共用）。
type CategoryForm struct {
	// ID 分类 ID，新增时为空。
	ID string `json:"id"`
	// ParentID 父分类 ID，空或 "0" 表示挂在根下。
	ParentID string `json:"parentId"`
	// Name 分类名称。
	Name string `json:"name"`
	// Sort 排序值。
	Sort int `json:"sort"`
	// Status 状态；开关类控件可能提交 1/"1"/true，用 FlexInt 兼容。
	Status FlexInt `json:"status"`
	// Remark 备注。
	Remark string `json:"remark"`
}

// AttachmentItem 是附件列表项（对齐前端附件表格）。
type AttachmentItem struct {
	// ID 附件 ID。
	ID string `json:"id"`
	// CategoryID 所属分类 ID。
	CategoryID string `json:"categoryId"`
	// CategoryName 所属分类名称；分类不存在时为空串。
	CategoryName string `json:"categoryName"`
	// OriginName 原文件名（用户可见名）。
	OriginName string `json:"originName"`
	// ObjectName 存储对象名。
	ObjectName string `json:"objectName"`
	// MimeType 资源类型。
	MimeType string `json:"mimeType"`
	// Suffix 文件后缀（不含点）。
	Suffix string `json:"suffix"`
	// SizeByte 字节数。
	SizeByte int64 `json:"sizeByte"`
	// SizeInfo 可读大小。
	SizeInfo string `json:"sizeInfo"`
	// URL 访问地址。
	URL string `json:"url"`
	// StoragePath 相对存储路径。
	StoragePath string `json:"storagePath"`
	// Remark 备注。
	Remark string `json:"remark"`
	// CreateTime 上传时间。
	CreateTime string `json:"createTime"`
	// IsImage 是否图片（前端据此决定走缩略图预览还是新窗口打开）。
	IsImage bool `json:"isImage"`
}

// AttachmentQuery 是附件分页查询条件。
type AttachmentQuery struct {
	// PageQuery 分页参数。
	PageQuery
	// CategoryID 分类 ID 文本；空表示不限分类。
	CategoryID string `json:"categoryId"`
	// Keywords 文件名 / 备注模糊搜索。
	Keywords string `json:"keywords"`
}

// AttachmentForm 对齐前端附件修改表单（重命名 / 移动分类 / 备注）。
type AttachmentForm struct {
	// ID 附件 ID。
	ID string `json:"id"`
	// OriginName 新的文件名。
	OriginName string `json:"originName"`
	// CategoryID 目标分类 ID；空表示不修改所属分类。
	CategoryID string `json:"categoryId"`
	// Remark 备注。
	Remark string `json:"remark"`
}

// FileInfo 是通用文件上传接口的返回体，字段名与前端 src/api/file/types.ts 完全一致，
// 使模板自带的 SingleImageUpload / MultiImageUpload / FileUpload 组件无需改动即可复用。
type FileInfo struct {
	// Name 原文件名。
	Name string `json:"name"`
	// URL 访问地址。
	URL string `json:"url"`
}

// NewCategoryItem 把分类实体转换为前端树节点。
//
// 参数 Parameters:
//   - row (*model.SysCategory): 分类实体。
//
// 返回 Returns:
//   - item (CategoryItem): 树节点。
func NewCategoryItem(row *model.SysCategory) CategoryItem {
	return CategoryItem{
		ID:         textID(row.ID),
		ParentID:   textID(row.ParentID),
		Name:       row.CategoryName,
		Sort:       row.Sort,
		Status:     row.Status,
		Remark:     row.Remark,
		CreateTime: timeText(row.CreateTime),
	}
}

// NewAttachmentItem 把附件实体转换为前端列表项。
//
// 参数 Parameters:
//   - row (*model.SysAttachment): 附件实体。
//   - categoryName (string): 所属分类名称。
//
// 返回 Returns:
//   - item (AttachmentItem): 列表项。
func NewAttachmentItem(row *model.SysAttachment, categoryName string) AttachmentItem {
	mimeType := textValue(row.MimeType)
	return AttachmentItem{
		ID:           textID(row.ID),
		CategoryID:   textID(row.CategoryID),
		CategoryName: categoryName,
		OriginName:   textValue(row.OriginName),
		ObjectName:   row.ObjectName,
		MimeType:     mimeType,
		Suffix:       textValue(row.Suffix),
		SizeByte:     int64Of(row.SizeByte),
		SizeInfo:     textValue(row.SizeInfo),
		URL:          row.URL,
		StoragePath:  row.StoragePath,
		Remark:       textValue(row.Remark),
		CreateTime:   timeText(row.CreateTime),
		IsImage:      isImageMime(mimeType),
	}
}

// isImageMime 判断资源类型是否为图片。
//
// 参数 Parameters:
//   - mimeType (string): 资源类型。
//
// 返回 Returns:
//   - yes (bool): 图片类型时为 true。
func isImageMime(mimeType string) bool {
	return len(mimeType) >= 6 && mimeType[:6] == "image/"
}
