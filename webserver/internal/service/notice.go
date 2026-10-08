package service

import (
	"strings"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/logic"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// NoticeService 提供通知公告的应用服务。
type NoticeService struct {
	// notices 公告领域操作。
	notices *logic.NoticeLogic
}

// NewNoticeService 创建公告应用服务。
//
// 参数 Parameters:
//   - notices (*logic.NoticeLogic): 公告领域操作。
//
// 返回 Returns:
//   - service (*NoticeService): 公告应用服务。
func NewNoticeService(notices *logic.NoticeLogic) *NoticeService {
	return &NoticeService{notices: notices}
}

// Page 分页查询公告（管理端）。
//
// 参数 Parameters:
//   - q (dto.NoticeQuery): 查询条件。
//
// 返回 Returns:
//   - result (dto.PageResult[dto.NoticeItem]): 分页结果。
//   - err (error): 查询失败时返回业务错误。
func (s *NoticeService) Page(q dto.NoticeQuery) (dto.PageResult[dto.NoticeItem], error) {
	return s.page(q, false, 0)
}

// MyPage 分页查询「我的通知」。
//
// 参数 Parameters:
//   - q (dto.NoticeQuery): 查询条件。
//   - userID (int64): 当前用户主键。
//
// 返回 Returns:
//   - result (dto.PageResult[dto.NoticeItem]): 分页结果。
//   - err (error): 查询失败时返回业务错误。
func (s *NoticeService) MyPage(q dto.NoticeQuery, userID int64) (dto.PageResult[dto.NoticeItem], error) {
	return s.page(q, true, userID)
}

// page 是管理端与「我的通知」的公共分页实现。
func (s *NoticeService) page(q dto.NoticeQuery, onlyMine bool, userID int64) (dto.PageResult[dto.NoticeItem], error) {
	rows, total, err := s.notices.Page(q, onlyMine, userID)
	if err != nil {
		return dto.PageResult[dto.NoticeItem]{}, err
	}
	ids := make([]int64, 0, len(rows))
	for index := range rows {
		ids = append(ids, rows[index].ID)
	}
	readIDs, err := s.notices.ReadIDs(userID, ids)
	if err != nil {
		return dto.PageResult[dto.NoticeItem]{}, err
	}
	list := make([]dto.NoticeItem, 0, len(rows))
	for index := range rows {
		row := rows[index]
		isRead := 0
		if readIDs[row.ID] {
			isRead = 1
		}
		list = append(list, dto.NewNoticeItem(&row, isRead))
	}
	return dto.PageResult[dto.NoticeItem]{List: list, Total: total}, nil
}

// Form 返回公告编辑表单。
//
// 参数 Parameters:
//   - idText (string): 公告 ID 文本。
//
// 返回 Returns:
//   - form (dto.NoticeForm): 表单数据。
//   - err (error): ID 非法或公告不存在时返回业务错误。
func (s *NoticeService) Form(idText string) (dto.NoticeForm, error) {
	id, err := requiredID(idText, "公告 ID 无效")
	if err != nil {
		return dto.NoticeForm{}, err
	}
	row, err := s.notices.Get(id)
	if err != nil {
		return dto.NoticeForm{}, err
	}
	return dto.NoticeForm{
		ID:          util.TextID(row.ID),
		Title:       row.Title,
		Content:     util.TextValue(row.Content),
		Type:        dto.FlexInt(row.Type),
		Level:       util.TextValue(row.Level),
		Status:      dto.FlexInt(logic.NoticeStatusFromPublish(row.PublishStatus)),
		TargetType:  dto.FlexInt(row.TargetType),
		TargetUsers: util.ParseIDList(util.TextValue(row.TargetUsers)),
	}, nil
}

// Detail 返回公告详情（含发布人展示名）。
//
// 参数 Parameters:
//   - idText (string): 公告 ID 文本。
//
// 返回 Returns:
//   - detail (dto.NoticeDetail): 公告详情。
//   - err (error): ID 非法或公告不存在时返回业务错误。
func (s *NoticeService) Detail(idText string) (dto.NoticeDetail, error) {
	id, err := requiredID(idText, "公告 ID 无效")
	if err != nil {
		return dto.NoticeDetail{}, err
	}
	row, err := s.notices.Get(id)
	if err != nil {
		return dto.NoticeDetail{}, err
	}
	return dto.NoticeDetail{
		ID:            util.TextID(row.ID),
		Title:         row.Title,
		Content:       util.TextValue(row.Content),
		Type:          row.Type,
		Level:         util.TextValue(row.Level),
		PublishStatus: row.PublishStatus,
		TargetUserIDs: util.TextValue(row.TargetUsers),
		PublisherName: s.notices.PublisherName(row.PublisherID),
		PublishTime:   util.TimeText(row.PublishTime),
	}, nil
}

// Create 新增公告。
//
// 参数 Parameters:
//   - req (dto.NoticeForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 新公告 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *NoticeService) Create(req dto.NoticeForm, operator *model.SysUser) (string, error) {
	if strings.TrimSpace(req.Title) == "" {
		return "", apperr.Invalid("公告标题不能为空")
	}
	row := model.SysNotice{}
	logic.ApplyNoticeForm(&row, &req, operatorID(operator))
	row.AuditFields = logic.Stamp(operator)
	if err := s.notices.Create(&row); err != nil {
		return "", err
	}
	return util.TextID(row.ID), nil
}

// Update 更新公告。
//
// 参数 Parameters:
//   - idText (string): 公告 ID 文本。
//   - req (dto.NoticeForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 公告 ID。
//   - err (error): ID 非法或写入失败时返回业务错误。
func (s *NoticeService) Update(idText string, req dto.NoticeForm, operator *model.SysUser) (string, error) {
	id, err := requiredID(idText, "公告 ID 无效")
	if err != nil {
		return "", err
	}
	row, err := s.notices.Get(id)
	if err != nil {
		return "", err
	}
	logic.ApplyNoticeForm(row, &req, util.Int64Value(row.PublisherID))
	row.UpdatedBy, row.UpdateTime = logic.Touch(operator)
	if err := s.notices.Update(row); err != nil {
		return "", err
	}
	return util.TextID(row.ID), nil
}

// Delete 删除公告。
//
// 参数 Parameters:
//   - idsText (string): 逗号分隔的公告 ID。
//
// 返回 Returns:
//   - err (error): ID 非法或写入失败时返回业务错误。
func (s *NoticeService) Delete(idsText string) error {
	ids := util.ParseIDList(idsText)
	if len(ids) == 0 {
		return apperr.Invalid("公告 ID 无效")
	}
	return s.notices.Delete(ids)
}

// Publish 发布公告。
//
// 参数 Parameters:
//   - idText (string): 公告 ID 文本。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - err (error): ID 非法或公告不存在时返回业务错误。
func (s *NoticeService) Publish(idText string, operator *model.SysUser) error {
	id, err := requiredID(idText, "公告 ID 无效")
	if err != nil {
		return err
	}
	return s.notices.Publish(id, operatorID(operator))
}

// Revoke 撤回公告。
//
// 参数 Parameters:
//   - idText (string): 公告 ID 文本。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - err (error): ID 非法或公告不存在时返回业务错误。
func (s *NoticeService) Revoke(idText string, operator *model.SysUser) error {
	id, err := requiredID(idText, "公告 ID 无效")
	if err != nil {
		return err
	}
	return s.notices.Revoke(id, operatorID(operator))
}

// ReadAll 把当前用户可见的公告全部标记为已读。
//
// 参数 Parameters:
//   - userID (int64): 当前用户主键。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (s *NoticeService) ReadAll(userID int64) error {
	if userID <= 0 {
		return apperr.Unauthorized("登录已过期")
	}
	return s.notices.MarkAllRead(userID)
}

// operatorID 返回操作人主键，缺失时为 0。
func operatorID(operator *model.SysUser) int64 {
	if operator == nil {
		return 0
	}
	return operator.ID
}
