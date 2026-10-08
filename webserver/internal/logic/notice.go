package logic

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// 公告发布状态（sa_system_notice.publish_status）。
const (
	// NoticeDraft 草稿。
	NoticeDraft = 0
	// NoticePublished 已发布。
	NoticePublished = 1
	// NoticeRevokedStatus 已撤回。
	NoticeRevokedStatus = 2
)

// NoticeLogic 负责 sa_system_notice 与 sa_system_notice_read 的数据读写。
type NoticeLogic struct {
	// db 权限库会话。
	db *gorm.DB
}

// NewNoticeLogic 创建公告领域操作。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//
// 返回 Returns:
//   - logic (*NoticeLogic): 公告领域操作实例。
func NewNoticeLogic(db *gorm.DB) *NoticeLogic { return &NoticeLogic{db: db} }

// Page 分页查询公告；onlyMine 为 true 时只返回已发布且面向当前用户的公告。
//
// 参数 Parameters:
//   - q (dto.NoticeQuery): 查询条件。
//   - onlyMine (bool): 是否仅查询「我的通知」。
//   - userID (int64): 当前用户主键。
//
// 返回 Returns:
//   - rows ([]model.SysNotice): 当前页公告。
//   - total (int64): 满足条件的总数。
//   - err (error): 查询失败时返回业务错误。
func (l *NoticeLogic) Page(q dto.NoticeQuery, onlyMine bool, userID int64) ([]model.SysNotice, int64, error) {
	page, size := util.NormalizePage(q.PageNum, q.PageSize)
	db := l.db.Model(&model.SysNotice{})
	if onlyMine {
		db = db.Where("publish_status = ?", NoticePublished)
		if userID != 0 {
			db = db.Where(`(target_type = 1 OR target_users LIKE ? ESCAPE '\')`, "%"+util.TextID(userID)+"%")
		} else {
			db = db.Where("target_type = 1")
		}
	} else {
		title := strings.TrimSpace(q.Title)
		if title == "" {
			title = strings.TrimSpace(q.Keywords)
		}
		if pattern := util.LikeKeyword(title); pattern != "" {
			db = db.Where(`title LIKE ? ESCAPE '\'`, pattern)
		}
		if publishStatus := strings.TrimSpace(q.PublishStatus); publishStatus != "" {
			db = db.Where("publish_status = ?", util.AtoiSafe(publishStatus, 0))
		}
		if noticeType := strings.TrimSpace(q.Type); noticeType != "" {
			db = db.Where("type = ?", util.AtoiSafe(noticeType, 0))
		}
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, InternalErr("查询公告数量失败", err)
	}
	var rows []model.SysNotice
	if err := db.Order("id desc").Offset(util.Offset(page, size)).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, InternalErr("查询公告失败", err)
	}
	return rows, total, nil
}

// ReadIDs 返回当前用户在这批公告中的已读标记。
//
// 参数 Parameters:
//   - userID (int64): 当前用户主键。
//   - noticeIDs ([]int64): 公告主键列表。
//
// 返回 Returns:
//   - readIDs (map[int64]bool): 已读公告集合。
//   - err (error): 查询失败时返回业务错误。
func (l *NoticeLogic) ReadIDs(userID int64, noticeIDs []int64) (map[int64]bool, error) {
	readIDs := map[int64]bool{}
	if userID == 0 || len(noticeIDs) == 0 {
		return readIDs, nil
	}
	var loaded []int64
	if err := l.db.Model(&model.SysNoticeRead{}).
		Where("user_id = ? AND notice_id IN ?", userID, noticeIDs).
		Pluck("notice_id", &loaded).Error; err != nil {
		return nil, InternalErr("查询公告已读状态失败", err)
	}
	for _, id := range loaded {
		readIDs[id] = true
	}
	return readIDs, nil
}

// Get 按主键查询公告。
//
// 参数 Parameters:
//   - id (int64): 公告主键。
//
// 返回 Returns:
//   - row (*model.SysNotice): 公告实体。
//   - err (error): 不存在时返回 NotFound 业务错误。
func (l *NoticeLogic) Get(id int64) (*model.SysNotice, error) {
	var row model.SysNotice
	if err := l.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, TranslateDBError(err, "公告不存在", "查询公告失败")
	}
	return &row, nil
}

// PublisherName 返回公告发布人的展示名（真实姓名优先，其次账号）。
//
// 参数 Parameters:
//   - publisherID (*int64): 发布人主键，可为 nil。
//
// 返回 Returns:
//   - name (string): 展示名；查不到时返回空串。
func (l *NoticeLogic) PublisherName(publisherID *int64) string {
	if publisherID == nil {
		return ""
	}
	var publisher model.SysUser
	if err := l.db.Select("id", "username", "realname").First(&publisher, "id = ?", *publisherID).Error; err != nil {
		return ""
	}
	if strings.TrimSpace(publisher.Realname) != "" {
		return publisher.Realname
	}
	return publisher.Username
}

// Create 新增公告。
//
// 参数 Parameters:
//   - row (*model.SysNotice): 公告实体（含审计字段）。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *NoticeLogic) Create(row *model.SysNotice) error {
	if err := l.db.Create(row).Error; err != nil {
		return InternalErr("创建公告失败", err)
	}
	return nil
}

// Update 保存公告。
//
// 参数 Parameters:
//   - row (*model.SysNotice): 已修改的公告实体。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *NoticeLogic) Update(row *model.SysNotice) error {
	if err := l.db.Save(row).Error; err != nil {
		return InternalErr("更新公告失败", err)
	}
	return nil
}

// Delete 软删除公告并清理已读记录。
//
// 参数 Parameters:
//   - ids ([]int64): 公告主键列表。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *NoticeLogic) Delete(ids []int64) error {
	return l.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id IN ?", ids).Delete(&model.SysNotice{}).Error; err != nil {
			return InternalErr("删除公告失败", err)
		}
		if err := tx.Where("notice_id IN ?", ids).Delete(&model.SysNoticeRead{}).Error; err != nil {
			return InternalErr("清理公告已读记录失败", err)
		}
		return nil
	})
}

// Publish 发布公告。
//
// 参数 Parameters:
//   - id (int64): 公告主键。
//   - publisherID (int64): 发布人主键。
//
// 返回 Returns:
//   - err (error): 公告不存在时返回 NotFound，写入失败时返回 Internal。
func (l *NoticeLogic) Publish(id, publisherID int64) error {
	now := time.Now()
	result := l.db.Model(&model.SysNotice{}).Where("id = ?", id).Updates(map[string]any{
		"publish_status": NoticePublished,
		"status":         NoticePublished,
		"publish_time":   now,
		"revoke_time":    nil,
		"publisher_id":   publisherID,
		"updated_by":     publisherID,
		"update_time":    now,
	})
	if result.Error != nil {
		return InternalErr("发布公告失败", result.Error)
	}
	if result.RowsAffected == 0 {
		return NotFoundErr("公告不存在")
	}
	return nil
}

// Revoke 撤回公告。
//
// 参数 Parameters:
//   - id (int64): 公告主键。
//   - operatorID (int64): 操作人主键。
//
// 返回 Returns:
//   - err (error): 公告不存在时返回 NotFound，写入失败时返回 Internal。
func (l *NoticeLogic) Revoke(id, operatorID int64) error {
	now := time.Now()
	result := l.db.Model(&model.SysNotice{}).Where("id = ?", id).Updates(map[string]any{
		"publish_status": NoticeRevokedStatus,
		"status":         -1, // 前端 FormStatus.REVOKED
		"revoke_time":    now,
		"updated_by":     operatorID,
		"update_time":    now,
	})
	if result.Error != nil {
		return InternalErr("撤回公告失败", result.Error)
	}
	if result.RowsAffected == 0 {
		return NotFoundErr("公告不存在")
	}
	return nil
}

// MarkAllRead 把当前用户所有已发布公告标记为已读。
//
// 参数 Parameters:
//   - userID (int64): 当前用户主键。
//
// 返回 Returns:
//   - err (error): 查询或写入失败时返回业务错误。
func (l *NoticeLogic) MarkAllRead(userID int64) error {
	var rows []model.SysNotice
	if err := l.db.Select("id").Where("publish_status = ?", NoticePublished).Find(&rows).Error; err != nil {
		return InternalErr("查询公告失败", err)
	}
	now := time.Now()
	for _, row := range rows {
		// 已读记录使用 (notice_id,user_id) 唯一索引，重复插入会被忽略。
		if err := l.db.Exec(
			`INSERT OR IGNORE INTO sa_system_notice_read (notice_id, user_id, read_time, create_time) VALUES (?, ?, ?, ?)`,
			row.ID, userID, now, now).Error; err != nil {
			return InternalErr("写入已读记录失败", err)
		}
	}
	return nil
}

// ApplyForm 把前端公告表单写入实体。
//
// 参数 Parameters:
//   - notice (*model.SysNotice): 目标实体。
//   - req (*dto.NoticeForm): 前端表单。
//   - publisherID (int64): 发布人主键，0 表示未发布。
func ApplyNoticeForm(notice *model.SysNotice, req *dto.NoticeForm, publisherID int64) {
	notice.Title = strings.TrimSpace(req.Title)
	notice.Type = int(req.Type)
	notice.Content = util.NonEmptyPtr(req.Content)
	notice.Level = util.NonEmptyPtr(req.Level)
	notice.TargetType = int(req.TargetType)
	if notice.TargetType == 0 {
		notice.TargetType = 1
	}
	if len(req.TargetUsers) > 0 {
		parts := make([]string, 0, len(req.TargetUsers))
		for _, id := range req.TargetUsers {
			parts = append(parts, util.TextID(id))
		}
		notice.TargetUsers = util.NonEmptyPtr(strings.Join(parts, ","))
	} else {
		notice.TargetUsers = nil
	}
	switch int(req.Status) {
	case NoticePublished:
		notice.PublishStatus = NoticePublished
		notice.Status = 1
		notice.PublishTime = util.NowPtr()
		notice.RevokeTime = nil
		if publisherID > 0 {
			notice.PublisherID = &publisherID
		}
	case -1: // 前端 FormStatus.REVOKED
		notice.PublishStatus = NoticeRevokedStatus
		notice.Status = -1
		notice.RevokeTime = util.NowPtr()
	default:
		notice.PublishStatus = NoticeDraft
		notice.Status = 1
	}
}

// NoticeStatusFromPublish 把发布状态转换为前端表单的 status 字段。
//
// 参数 Parameters:
//   - publishStatus (int): 0 草稿 / 1 已发布 / 2 已撤回。
//
// 返回 Returns:
//   - status (int): 0 草稿 / 1 已发布 / -1 已撤回。
func NoticeStatusFromPublish(publishStatus int) int {
	switch publishStatus {
	case NoticePublished:
		return 1
	case NoticeRevokedStatus:
		return -1
	default:
		return 0
	}
}
