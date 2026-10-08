// Package service 是应用服务层：把一次用例串起来——校验入参、调用领域操作、
// 组装视图对象、维护审计字段与缓存失效。它不感知 HTTP，也不直接手写 SQL。
package service

import (
	"strings"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/logic"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// PostService 提供岗位管理的应用服务。
type PostService struct {
	// posts 岗位领域操作。
	posts *logic.PostLogic
}

// NewPostService 创建岗位应用服务。
//
// 参数 Parameters:
//   - posts (*logic.PostLogic): 岗位领域操作。
//
// 返回 Returns:
//   - service (*PostService): 岗位应用服务。
func NewPostService(posts *logic.PostLogic) *PostService {
	return &PostService{posts: posts}
}

// Page 分页查询岗位并转换为前端列表项。
//
// 参数 Parameters:
//   - q (dto.PostQuery): 查询条件。
//
// 返回 Returns:
//   - result (dto.PageResult[dto.PostItem]): 分页结果。
//   - err (error): 查询失败时返回业务错误。
func (s *PostService) Page(q dto.PostQuery) (dto.PageResult[dto.PostItem], error) {
	rows, total, err := s.posts.Page(q)
	if err != nil {
		return dto.PageResult[dto.PostItem]{}, err
	}
	list := make([]dto.PostItem, 0, len(rows))
	for index := range rows {
		list = append(list, dto.NewPostItem(&rows[index]))
	}
	return dto.PageResult[dto.PostItem]{List: list, Total: total}, nil
}

// Options 返回岗位下拉选项。
//
// 返回 Returns:
//   - options ([]dto.OptionItem): 岗位选项。
//   - err (error): 查询失败时返回业务错误。
func (s *PostService) Options() ([]dto.OptionItem, error) {
	rows, err := s.posts.Options()
	if err != nil {
		return nil, err
	}
	options := make([]dto.OptionItem, 0, len(rows))
	for index := range rows {
		row := rows[index]
		options = append(options, dto.OptionItem{Value: util.TextID(row.ID), Label: util.TextValue(row.Name)})
	}
	return options, nil
}

// Form 返回岗位编辑表单。
//
// 参数 Parameters:
//   - idText (string): 岗位 ID 文本。
//
// 返回 Returns:
//   - form (dto.PostForm): 表单数据。
//   - err (error): ID 非法或岗位不存在时返回业务错误。
func (s *PostService) Form(idText string) (dto.PostForm, error) {
	id, err := requiredID(idText, "岗位 ID 无效")
	if err != nil {
		return dto.PostForm{}, err
	}
	row, err := s.posts.Get(id)
	if err != nil {
		return dto.PostForm{}, err
	}
	return dto.NewPostForm(row), nil
}

// Create 新增岗位。
//
// 参数 Parameters:
//   - req (dto.PostForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 新岗位 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *PostService) Create(req dto.PostForm, operator *model.SysUser) (string, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return "", apperr.Invalid("岗位名称不能为空")
	}
	row := model.SysPost{
		Name:        util.NonEmptyPtr(name),
		Code:        util.NonEmptyPtr(req.Code),
		Sort:        req.Sort,
		Status:      util.NormalizeStatus(req.Status),
		Remark:      util.NonEmptyPtr(req.Remark),
		AuditFields: logic.Stamp(operator),
	}
	if err := s.posts.Create(&row); err != nil {
		return "", err
	}
	return util.TextID(row.ID), nil
}

// Update 更新岗位。
//
// 参数 Parameters:
//   - idText (string): 岗位 ID 文本。
//   - req (dto.PostForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 岗位 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *PostService) Update(idText string, req dto.PostForm, operator *model.SysUser) (string, error) {
	id, err := requiredID(idText, "岗位 ID 无效")
	if err != nil {
		return "", err
	}
	row, err := s.posts.Get(id)
	if err != nil {
		return "", err
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		row.Name = util.NonEmptyPtr(name)
	}
	row.Code = util.NonEmptyPtr(req.Code)
	row.Sort = req.Sort
	row.Status = util.NormalizeStatus(req.Status)
	row.Remark = util.NonEmptyPtr(req.Remark)
	row.UpdatedBy, row.UpdateTime = logic.Touch(operator)
	if err := s.posts.Update(row); err != nil {
		return "", err
	}
	return util.TextID(row.ID), nil
}

// Delete 删除岗位（支持逗号分隔的批量 ID）。
//
// 参数 Parameters:
//   - idsText (string): 逗号分隔的岗位 ID。
//
// 返回 Returns:
//   - err (error): ID 非法或写入失败时返回业务错误。
func (s *PostService) Delete(idsText string) error {
	ids := util.ParseIDList(idsText)
	if len(ids) == 0 {
		return apperr.Invalid("岗位 ID 无效")
	}
	return s.posts.Delete(ids)
}
