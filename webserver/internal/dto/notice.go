package dto

import "github.com/company/monitor-webserver/internal/model"

// 公告发布状态（sa_system_notice.publish_status）。
const (
	// NoticeDraft 草稿。
	NoticeDraft = 0
	// NoticePublished 已发布。
	NoticePublished = 1
	// NoticeRevoked 已撤回。
	NoticeRevoked = 2
)

// NoticeItem 对齐前端 NoticeItem（公告列表项）。
type NoticeItem struct {
	// ID 公告 ID。
	ID string `json:"id"`
	// Title 公告标题。
	Title string `json:"title"`
	// Content 公告内容。
	Content string `json:"content"`
	// Type 公告类型。
	Type int `json:"type"`
	// Level 通知等级。
	Level string `json:"level"`
	// PublishStatus 发布状态。
	PublishStatus int `json:"publishStatus"`
	// IsRead 是否已读：1 已读 / 0 未读。
	IsRead int `json:"isRead"`
	// PublishTime 发布时间。
	PublishTime string `json:"publishTime"`
	// RevokeTime 撤回时间。
	RevokeTime string `json:"revokeTime"`
	// CreateTime 创建时间。
	CreateTime string `json:"createTime"`
}

// NoticeForm 对齐前端 NoticeForm。
type NoticeForm struct {
	// ID 公告 ID。
	ID string `json:"id"`
	// Title 公告标题。
	Title string `json:"title"`
	// Content 公告内容。
	Content string `json:"content"`
	// Type 公告类型；字典下拉 notice_type 提交字符串，用 FlexInt 兼容。
	Type FlexInt `json:"type"`
	// Level 通知等级；字典下拉 notice_level 本身即为字符串。
	Level string `json:"level"`
	// Status 状态。
	Status FlexInt `json:"status"`
	// TargetType 通告目标类型。
	TargetType FlexInt `json:"targetType"`
	// TargetUsers 目标用户 ID 集合。
	TargetUsers []int64 `json:"targetUsers"`
}

// NoticeDetail 对齐前端 NoticeDetail。
type NoticeDetail struct {
	// ID 公告 ID。
	ID string `json:"id"`
	// Title 公告标题。
	Title string `json:"title"`
	// Content 公告内容。
	Content string `json:"content"`
	// Type 公告类型。
	Type int `json:"type"`
	// Level 通知等级。
	Level string `json:"level"`
	// PublishStatus 发布状态。
	PublishStatus int `json:"publishStatus"`
	// TargetUserIDs 目标用户 ID 文本。
	TargetUserIDs string `json:"targetUserIds"`
	// PublisherName 发布人名称。
	PublisherName string `json:"publisherName"`
	// PublishTime 发布时间。
	PublishTime string `json:"publishTime"`
}

// NoticeQuery 是公告分页查询参数。
type NoticeQuery struct {
	// PageQuery 分页参数。
	PageQuery
	// Title 公告标题关键字（管理端搜索使用）。
	Title string `json:"title" form:"title"`
	// Keywords 关键字（兼容前端其它页面的命名）。
	Keywords string `json:"keywords" form:"keywords"`
	// PublishStatus 发布状态过滤。
	PublishStatus string `json:"publishStatus" form:"publishStatus"`
	// Type 公告类型过滤。
	Type string `json:"type" form:"type"`
}

// NewNoticeItem 把公告实体转换为前端列表项。
//
// 参数 Parameters:
//   - row (*model.SysNotice): 公告实体。
//   - isRead (int): 1 已读 / 0 未读。
//
// 返回 Returns:
//   - item (NoticeItem): 前端公告列表项。
func NewNoticeItem(row *model.SysNotice, isRead int) NoticeItem {
	return NoticeItem{
		ID:            textID(row.ID),
		Title:         row.Title,
		Content:       textValue(row.Content),
		Type:          row.Type,
		Level:         textValue(row.Level),
		PublishStatus: row.PublishStatus,
		IsRead:        isRead,
		PublishTime:   timeText(row.PublishTime),
		RevokeTime:    timeText(row.RevokeTime),
		CreateTime:    timeText(row.CreateTime),
	}
}
