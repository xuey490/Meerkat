package service

import (
	"strings"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/logic"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// DeptService 提供部门管理的应用服务。
type DeptService struct {
	// depts 部门领域操作。
	depts *logic.DeptLogic
}

// NewDeptService 创建部门应用服务。
//
// 参数 Parameters:
//   - depts (*logic.DeptLogic): 部门领域操作。
//
// 返回 Returns:
//   - service (*DeptService): 部门应用服务。
func NewDeptService(depts *logic.DeptLogic) *DeptService { return &DeptService{depts: depts} }

// List 返回部门树。
//
// 参数 Parameters:
//   - q (dto.DeptQuery): 查询条件。
//
// 返回 Returns:
//   - tree ([]dto.DeptItem): 部门树。
//   - err (error): 查询失败时返回业务错误。
func (s *DeptService) List(q dto.DeptQuery) ([]dto.DeptItem, error) {
	rows, err := s.depts.List(q)
	if err != nil {
		return nil, err
	}
	return dto.BuildDeptTree(rows), nil
}

// Options 返回部门下拉树。
//
// 返回 Returns:
//   - tree ([]dto.OptionItem): 部门选项树。
//   - err (error): 查询失败时返回业务错误。
func (s *DeptService) Options() ([]dto.OptionItem, error) {
	rows, err := s.depts.Enabled()
	if err != nil {
		return nil, err
	}
	items := make([]dto.OptionItem, 0, len(rows))
	parents := make(map[string]string, len(rows))
	for index := range rows {
		row := rows[index]
		value := util.TextID(row.ID)
		items = append(items, dto.OptionItem{Value: value, Label: row.Name})
		if row.ParentID > 0 {
			parents[value] = util.TextID(row.ParentID)
		}
	}
	return dto.BuildOptionTree(items, parents), nil
}

// Form 返回部门编辑表单。
//
// 参数 Parameters:
//   - idText (string): 部门 ID 文本。
//
// 返回 Returns:
//   - form (dto.DeptForm): 表单数据。
//   - err (error): ID 非法或部门不存在时返回业务错误。
func (s *DeptService) Form(idText string) (dto.DeptForm, error) {
	id, err := requiredID(idText, "部门 ID 无效")
	if err != nil {
		return dto.DeptForm{}, err
	}
	row, err := s.depts.Get(id)
	if err != nil {
		return dto.DeptForm{}, err
	}
	leaderID := ""
	if row.LeaderID != nil {
		leaderID = util.TextID(*row.LeaderID)
	}
	return dto.DeptForm{
		ID:       util.TextID(row.ID),
		Name:     row.Name,
		Code:     util.TextValue(row.Code),
		ParentID: util.TextID(row.ParentID),
		Sort:     row.Sort,
		Status:   row.Status,
		LeaderID: leaderID,
		Remark:   util.TextValue(row.Remark),
	}, nil
}

// Create 新增部门。
//
// 参数 Parameters:
//   - req (dto.DeptForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 新部门 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *DeptService) Create(req dto.DeptForm, operator *model.SysUser) (string, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return "", apperr.Invalid("部门名称不能为空")
	}
	parentID := optionalID(req.ParentID)
	level, err := s.depts.Level(parentID)
	if err != nil {
		return "", err
	}
	row := model.SysDept{
		ParentID:    parentID,
		Name:        name,
		Code:        util.NonEmptyPtr(req.Code),
		Level:       level,
		Sort:        req.Sort,
		Status:      util.NormalizeStatus(req.Status),
		Remark:      util.NonEmptyPtr(req.Remark),
		LeaderID:    util.Int64Ptr(optionalID(req.LeaderID)),
		AuditFields: logic.Stamp(operator),
	}
	if err := s.depts.Create(&row); err != nil {
		return "", err
	}
	return util.TextID(row.ID), nil
}

// Update 更新部门（含子孙祖级列表同步）。
//
// 参数 Parameters:
//   - idText (string): 部门 ID 文本。
//   - req (dto.DeptForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 部门 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *DeptService) Update(idText string, req dto.DeptForm, operator *model.SysUser) (string, error) {
	id, err := requiredID(idText, "部门 ID 无效")
	if err != nil {
		return "", err
	}
	row, err := s.depts.Get(id)
	if err != nil {
		return "", err
	}
	parentID := row.ParentID
	if raw := strings.TrimSpace(req.ParentID); raw != "" {
		if parsed := optionalID(raw); parsed > 0 {
			parentID = parsed
		} else if raw == "0" {
			parentID = 0
		}
	}
	if parentID == row.ID {
		return "", apperr.Invalid("上级部门不能是自身")
	}
	oldLevel := row.Level
	level, err := s.depts.Level(parentID)
	if err != nil {
		return "", err
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		row.Name = name
	}
	row.Code = util.NonEmptyPtr(req.Code)
	row.ParentID = parentID
	row.Level = level
	row.Sort = req.Sort
	row.Status = util.NormalizeStatus(req.Status)
	row.Remark = util.NonEmptyPtr(req.Remark)
	row.LeaderID = util.Int64Ptr(optionalID(req.LeaderID))
	row.UpdatedBy, row.UpdateTime = logic.Touch(operator)
	if err := s.depts.Update(row, oldLevel); err != nil {
		return "", err
	}
	return util.TextID(row.ID), nil
}

// Delete 删除部门。
//
// 参数 Parameters:
//   - idsText (string): 逗号分隔的部门 ID。
//
// 返回 Returns:
//   - err (error): ID 非法或写入失败时返回业务错误。
func (s *DeptService) Delete(idsText string) error {
	ids := util.ParseIDList(idsText)
	if len(ids) == 0 {
		return apperr.Invalid("部门 ID 无效")
	}
	return s.depts.Delete(ids)
}

// WithDescendants 返回部门及其子孙 ID，供用户列表的数据权限过滤复用。
//
// 参数 Parameters:
//   - ids ([]int64): 起始部门 ID。
//
// 返回 Returns:
//   - result ([]int64): 含子孙的部门 ID。
//   - err (error): 查询失败时返回业务错误。
func (s *DeptService) WithDescendants(ids []int64) ([]int64, error) {
	return s.depts.WithDescendants(ids)
}
