package service

import (
	"bytes"
	"encoding/csv"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/logic"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/permission"
	"github.com/company/monitor-webserver/internal/util"
)

// 用户默认值与校验阈值（与旧实现保持一致）。
const (
	// DefaultUserPassword 是通过接口新增用户时的初始密码。
	DefaultUserPassword = "123456"
	// MinPasswordLength 是密码最小长度，与前端校验一致。
	MinPasswordLength = 6
	// maxImportMessages 是导入错误提示的最大条数。
	maxImportMessages = 20
)

// UserService 提供用户管理的应用服务。
type UserService struct {
	// users 用户领域操作。
	users *logic.UserLogic
	// depts 部门领域操作（部门筛选含下级部门）。
	depts *logic.DeptLogic
	// perms 权限缓存（数据范围与失效）。
	perms *permission.Service
}

// NewUserService 创建用户应用服务。
//
// 参数 Parameters:
//   - users (*logic.UserLogic): 用户领域操作。
//   - depts (*logic.DeptLogic): 部门领域操作。
//   - perms (*permission.Service): 权限服务。
//
// 返回 Returns:
//   - service (*UserService): 用户应用服务。
func NewUserService(users *logic.UserLogic, depts *logic.DeptLogic, perms *permission.Service) *UserService {
	return &UserService{users: users, depts: depts, perms: perms}
}

// Page 分页查询用户（按当前用户的数据权限范围收敛）。
//
// 参数 Parameters:
//   - q (dto.UserQuery): 查询条件。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - result (dto.PageResult[dto.UserItem]): 分页结果。
//   - err (error): 未登录或查询失败时返回业务错误。
func (s *UserService) Page(q dto.UserQuery, operator *model.SysUser) (dto.PageResult[dto.UserItem], error) {
	if operator == nil {
		return dto.PageResult[dto.UserItem]{}, apperr.Unauthorized("登录已过期")
	}
	var deptIDs []int64
	if deptID := strings.TrimSpace(q.DeptID); deptID != "" {
		ids, err := s.depts.WithDescendants(util.ParseIDList(deptID))
		if err != nil {
			return dto.PageResult[dto.UserItem]{}, err
		}
		if len(ids) == 0 {
			return dto.PageResult[dto.UserItem]{List: []dto.UserItem{}, Total: 0}, nil
		}
		deptIDs = ids
	}
	scope := s.perms.Of(operator).Scope
	rows, total, err := s.users.Page(q, scope, operator.ID, deptIDs)
	if err != nil {
		return dto.PageResult[dto.UserItem]{}, err
	}
	deptNames, err := s.users.DeptNames()
	if err != nil {
		return dto.PageResult[dto.UserItem]{}, err
	}
	userIDs := make([]int64, 0, len(rows))
	for index := range rows {
		userIDs = append(userIDs, rows[index].ID)
	}
	roleNames, err := s.users.RoleNames(userIDs)
	if err != nil {
		return dto.PageResult[dto.UserItem]{}, err
	}
	list := make([]dto.UserItem, 0, len(rows))
	for index := range rows {
		row := rows[index]
		deptName := ""
		if row.DeptID != nil {
			deptName = deptNames[*row.DeptID]
		}
		list = append(list, dto.NewUserItem(&row, deptName, roleNames[row.ID]))
	}
	return dto.PageResult[dto.UserItem]{List: list, Total: total}, nil
}

// Options 返回用户下拉选项（标签取昵称，昵称为空时回退账号）。
//
// 返回 Returns:
//   - options ([]dto.OptionItem): 用户选项。
//   - err (error): 查询失败时返回业务错误。
func (s *UserService) Options() ([]dto.OptionItem, error) {
	rows, err := s.users.Enabled()
	if err != nil {
		return nil, err
	}
	options := make([]dto.OptionItem, 0, len(rows))
	for index := range rows {
		row := rows[index]
		label := row.Realname
		if strings.TrimSpace(label) == "" {
			label = row.Username
		}
		options = append(options, dto.OptionItem{Value: util.TextID(row.ID), Label: label})
	}
	return options, nil
}

// Form 返回用户编辑表单（含角色与岗位）。
//
// 参数 Parameters:
//   - idText (string): 用户 ID 文本。
//
// 返回 Returns:
//   - form (dto.UserForm): 表单数据。
//   - err (error): ID 非法或用户不存在时返回业务错误。
func (s *UserService) Form(idText string) (dto.UserForm, error) {
	id, err := requiredID(idText, "用户 ID 无效")
	if err != nil {
		return dto.UserForm{}, err
	}
	row, err := s.users.Get(id)
	if err != nil {
		return dto.UserForm{}, err
	}
	roleIDs, err := s.users.RoleIDs(row.ID)
	if err != nil {
		return dto.UserForm{}, err
	}
	postIDs, err := s.users.PostIDs(row.ID)
	if err != nil {
		return dto.UserForm{}, err
	}
	deptID := ""
	if row.DeptID != nil {
		deptID = util.TextID(*row.DeptID)
	}
	return dto.UserForm{
		ID:       util.TextID(row.ID),
		Username: row.Username,
		Nickname: row.Realname,
		Avatar:   row.Avatar,
		DeptID:   deptID,
		Gender:   dto.FlexInt(dto.GenderFromDB(row.Gender)),
		Mobile:   util.TextValue(row.Phone),
		Email:    util.TextValue(row.Email),
		RoleIDs:  util.TextIDs(roleIDs),
		PostIDs:  util.TextIDs(postIDs),
		Status:   dto.FlexInt(row.Status),
		Remark:   util.TextValue(row.Remark),
	}, nil
}

// Create 新增用户并绑定角色、部门与岗位。
//
// 参数 Parameters:
//   - req (dto.UserForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 新用户 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *UserService) Create(req dto.UserForm, operator *model.SysUser) (string, error) {
	username := strings.TrimSpace(req.Username)
	if username == "" || strings.TrimSpace(req.Nickname) == "" {
		return "", apperr.Invalid("用户名与昵称不能为空")
	}
	exists, err := s.users.UsernameExists(username, 0)
	if err != nil {
		return "", err
	}
	if exists {
		return "", apperr.Conflict("用户名已存在")
	}
	password := strings.TrimSpace(req.Password)
	if password == "" {
		password = DefaultUserPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", apperr.Internal("生成密码失败", err)
	}
	row := model.SysUser{
		Username:    username,
		Password:    string(hash),
		Realname:    strings.TrimSpace(req.Nickname),
		Gender:      dto.GenderToDB(int(req.Gender)),
		Avatar:      req.Avatar,
		Email:       util.NonEmptyPtr(req.Email),
		Phone:       util.NonEmptyPtr(req.Mobile),
		DeptID:      util.Int64Ptr(optionalID(req.DeptID)),
		Status:      util.NormalizeStatus(int(req.Status)),
		Remark:      util.NonEmptyPtr(req.Remark),
		Dashboard:   "work",
		AuditFields: logic.Stamp(operator),
	}
	if err := s.users.Create(&row, req.RoleIDs.IDs(), req.PostIDs.IDs()); err != nil {
		return "", err
	}
	s.perms.Invalidate(row.ID)
	return util.TextID(row.ID), nil
}

// Update 更新用户基础信息与关联关系。
//
// 参数 Parameters:
//   - idText (string): 用户 ID 文本。
//   - req (dto.UserForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 用户 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *UserService) Update(idText string, req dto.UserForm, operator *model.SysUser) (string, error) {
	id, err := requiredID(idText, "用户 ID 无效")
	if err != nil {
		return "", err
	}
	row, err := s.users.Get(id)
	if err != nil {
		return "", err
	}
	if username := strings.TrimSpace(req.Username); username != "" && username != row.Username {
		exists, err := s.users.UsernameExists(username, row.ID)
		if err != nil {
			return "", err
		}
		if exists {
			return "", apperr.Conflict("用户名已存在")
		}
		row.Username = username
	}
	if nickname := strings.TrimSpace(req.Nickname); nickname != "" {
		row.Realname = nickname
	}
	row.Gender = dto.GenderToDB(int(req.Gender))
	row.Avatar = req.Avatar
	row.Email = util.NonEmptyPtr(req.Email)
	row.Phone = util.NonEmptyPtr(req.Mobile)
	row.DeptID = util.Int64Ptr(optionalID(req.DeptID))
	row.Status = util.NormalizeStatus(int(req.Status))
	row.Remark = util.NonEmptyPtr(req.Remark)
	row.UpdatedBy, row.UpdateTime = logic.Touch(operator)
	// postIds 缺省表示「不修改岗位关联」，显式空数组表示清空，因此保留 nil 语义。
	var postIDs []int64
	if req.PostIDs != nil {
		postIDs = req.PostIDs.IDs()
	}
	if err := s.users.Update(row, req.RoleIDs.IDs(), postIDs); err != nil {
		return "", err
	}
	s.perms.Invalidate(row.ID)
	return util.TextID(row.ID), nil
}

// ResetPassword 重置指定用户的密码。
//
// 参数 Parameters:
//   - idText (string): 用户 ID 文本。
//   - password (string): 新密码明文。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - err (error): 校验失败或用户不存在时返回业务错误。
func (s *UserService) ResetPassword(idText, password string, operator *model.SysUser) error {
	id, err := requiredID(idText, "用户 ID 无效")
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(password)) < MinPasswordLength {
		return apperr.Invalid("密码长度不能少于 6 位")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return apperr.Internal("生成密码失败", err)
	}
	return s.users.ResetPassword(id, string(hash), operatorID(operator))
}

// Delete 软删除用户（支持逗号分隔的批量 ID），不允许删除当前登录用户。
//
// 参数 Parameters:
//   - idsText (string): 逗号分隔的用户 ID。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - err (error): ID 非法或写入失败时返回业务错误。
func (s *UserService) Delete(idsText string, operator *model.SysUser) error {
	ids := util.ParseIDList(idsText)
	if len(ids) == 0 {
		return apperr.Invalid("用户 ID 无效")
	}
	if operator != nil {
		for _, id := range ids {
			if id == operator.ID {
				return apperr.Invalid("不能删除当前登录用户")
			}
		}
	}
	if err := s.users.Delete(ids); err != nil {
		return err
	}
	for _, id := range ids {
		s.perms.Invalidate(id)
	}
	return nil
}

// Export 返回用户 CSV 内容（含表头），带部门名称。
//
// 返回 Returns:
//   - filename (string): 建议的下载文件名。
//   - records ([][]string): CSV 记录。
//   - err (error): 查询失败时返回业务错误。
func (s *UserService) Export() (string, [][]string, error) {
	rows, err := s.users.ExportRows()
	if err != nil {
		return "", nil, err
	}
	deptNames, err := s.users.DeptNames()
	if err != nil {
		return "", nil, err
	}
	records := make([][]string, 0, len(rows)+1)
	records = append(records, []string{"用户名", "昵称", "手机号", "邮箱", "性别", "状态", "部门"})
	for index := range rows {
		row := rows[index]
		deptName := ""
		if row.DeptID != nil {
			deptName = deptNames[*row.DeptID]
		}
		records = append(records, []string{
			row.Username,
			row.Realname,
			util.TextValue(row.Phone),
			util.TextValue(row.Email),
			strconv.Itoa(dto.GenderFromDB(row.Gender)),
			strconv.Itoa(row.Status),
			deptName,
		})
	}
	return "users.csv", records, nil
}

// Template 返回用户导入模板的 CSV 内容。
//
// 返回 Returns:
//   - filename (string): 建议的下载文件名。
//   - records ([][]string): CSV 记录。
func (s *UserService) Template() (string, [][]string) {
	return "user-import-template.csv", [][]string{
		{"用户名", "昵称", "手机号", "邮箱", "性别", "状态", "部门"},
		{"zhangsan", "张三", "13800000000", "zhangsan@example.com", "1", "1", ""},
	}
}

// ImportResult 是用户导入的结果统计（对齐前端 ExcelResult）。
type ImportResult struct {
	// ValidCount 成功导入条数。
	ValidCount int `json:"validCount"`
	// InvalidCount 失败条数。
	InvalidCount int `json:"invalidCount"`
	// MessageList 错误提示列表。
	MessageList []string `json:"messageList"`
}

// Import 按 CSV 模板批量导入用户。
//
// 参数 Parameters:
//   - content ([]byte): CSV 文件内容。
//
// 返回 Returns:
//   - result (ImportResult): 导入统计。
//   - err (error): 文件格式错误或查询失败时返回业务错误。
func (s *UserService) Import(content []byte) (ImportResult, error) {
	reader := csv.NewReader(bytes.NewReader(content))
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil && !strings.Contains(err.Error(), "EOF") {
		return ImportResult{}, apperr.Invalid("文件格式不正确，请使用模板导入")
	}
	deptIDs, err := s.users.DeptIDByName()
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{MessageList: make([]string, 0, 8)}
	for index, row := range rows {
		if index == 0 {
			continue // 表头
		}
		if len(row) == 0 || strings.TrimSpace(row[0]) == "" {
			continue
		}
		if len(result.MessageList) >= maxImportMessages {
			result.MessageList = append(result.MessageList, "错误过多，已省略后续提示")
			break
		}
		username := strings.TrimSpace(row[0])
		exists, err := s.users.UsernameExists(username, 0)
		if err != nil {
			result.InvalidCount++
			result.MessageList = append(result.MessageList, "第 "+strconv.Itoa(index+1)+" 行：数据库校验失败")
			continue
		}
		if exists {
			result.InvalidCount++
			result.MessageList = append(result.MessageList, "第 "+strconv.Itoa(index+1)+" 行：用户名已存在")
			continue
		}
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(DefaultUserPassword), bcrypt.DefaultCost)
		if hashErr != nil {
			result.InvalidCount++
			result.MessageList = append(result.MessageList, "第 "+strconv.Itoa(index+1)+" 行：密码生成失败")
			continue
		}
		var deptID *int64
		if id, matched := deptIDs[csvField(row, 6)]; matched {
			deptID = &id
		}
		now := time.Now()
		user := model.SysUser{
			Username:    username,
			Password:    string(hash),
			Realname:    csvField(row, 1),
			Phone:       util.NonEmptyPtr(csvField(row, 2)),
			Email:       util.NonEmptyPtr(csvField(row, 3)),
			Gender:      dto.GenderToDB(util.AtoiSafe(csvField(row, 4), 0)),
			Status:      util.NormalizeStatus(util.AtoiSafe(csvField(row, 5), 1)),
			DeptID:      deptID,
			AuditFields: model.AuditFields{CreateTime: &now, UpdateTime: &now},
		}
		if err := s.users.CreateImported(&user); err != nil {
			result.InvalidCount++
			result.MessageList = append(result.MessageList, "第 "+strconv.Itoa(index+1)+" 行：写入失败")
			continue
		}
		result.ValidCount++
	}
	return result, nil
}

// Profile 返回当前登录用户的个人资料。
//
// 参数 Parameters:
//   - operator (*model.SysUser): 当前用户。
//
// 返回 Returns:
//   - profile (dto.UserProfile): 个人资料。
//   - err (error): 未登录或查询失败时返回业务错误。
func (s *UserService) Profile(operator *model.SysUser) (dto.UserProfile, error) {
	if operator == nil {
		return dto.UserProfile{}, apperr.Unauthorized("登录已过期")
	}
	deptNames, err := s.users.DeptNames()
	if err != nil {
		return dto.UserProfile{}, err
	}
	roleNames, err := s.users.RoleNames([]int64{operator.ID})
	if err != nil {
		return dto.UserProfile{}, err
	}
	deptName := ""
	if operator.DeptID != nil {
		deptName = deptNames[*operator.DeptID]
	}
	return dto.NewUserProfile(operator, deptName, roleNames[operator.ID]), nil
}

// UpdateProfile 修改当前用户的昵称、头像与性别。
//
// 参数 Parameters:
//   - req (dto.UserProfileForm): 前端表单。
//   - operator (*model.SysUser): 当前用户。
//
// 返回 Returns:
//   - err (error): 未登录或写入失败时返回业务错误。
func (s *UserService) UpdateProfile(req dto.UserProfileForm, operator *model.SysUser) error {
	if operator == nil {
		return apperr.Unauthorized("登录已过期")
	}
	gender := 0
	if req.Gender != nil {
		gender = int(*req.Gender)
	}
	return s.users.UpdateProfile(operator.ID, req.Nickname, req.Avatar, dto.GenderToDB(gender))
}

// ChangePassword 修改当前用户密码（需校验原密码）。
//
// 参数 Parameters:
//   - req (dto.PasswordChangeForm): 前端表单。
//   - operator (*model.SysUser): 当前用户。
//
// 返回 Returns:
//   - err (error): 校验或写入失败时返回业务错误。
func (s *UserService) ChangePassword(req dto.PasswordChangeForm, operator *model.SysUser) error {
	if operator == nil {
		return apperr.Unauthorized("登录已过期")
	}
	if len(req.NewPassword) < MinPasswordLength {
		return apperr.Invalid("新密码长度不能少于 6 位")
	}
	if req.ConfirmPassword != "" && req.NewPassword != req.ConfirmPassword {
		return apperr.Invalid("两次输入的新密码不一致")
	}
	if bcrypt.CompareHashAndPassword([]byte(operator.Password), []byte(req.OldPassword)) != nil {
		return apperr.Invalid("原密码不正确")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return apperr.Internal("生成密码失败", err)
	}
	return s.users.ChangePassword(operator.ID, string(hash))
}

// csvField 安全读取 CSV 行的某一列。
//
// 参数 Parameters:
//   - row ([]string): CSV 行。
//   - index (int): 列下标。
//
// 返回 Returns:
//   - value (string): 去空格后的列值；越界返回空串。
func csvField(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

// MenuIDs 返回用户的个人菜单授权（供「菜单设置」弹窗回显）。
//
// 参数 Parameters:
//   - idText (string): 用户 ID 文本。
//
// 返回 Returns:
//   - ids ([]string): 菜单 ID 文本列表（非 nil）。
//   - err (error): ID 非法或查询失败时返回业务错误。
func (s *UserService) MenuIDs(idText string) ([]string, error) {
	id, err := requiredID(idText, "用户 ID 无效")
	if err != nil {
		return nil, err
	}
	ids, err := s.users.MenuIDs(id)
	if err != nil {
		return nil, err
	}
	return util.TextIDs(ids), nil
}

// ReplaceMenus 覆盖式保存用户的个人菜单授权。
//
// 参数 Parameters:
//   - idText (string): 用户 ID 文本。
//   - menuIDs ([]int64): 前端提交的菜单 ID 列表。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - err (error): ID 非法、目标为超级管理员或写入失败时返回业务错误。
func (s *UserService) ReplaceMenus(idText string, menuIDs []int64, operator *model.SysUser) error {
	id, err := requiredID(idText, "用户 ID 无效")
	if err != nil {
		return err
	}
	target, err := s.users.Get(id)
	if err != nil {
		return err
	}
	// 超级管理员登录时直接返回全部菜单，个人授权不会生效；直接拒绝，避免「保存成功却无效果」。
	if permission.IsSuperUser(target) {
		return apperr.Invalid("超级管理员默认拥有全部菜单，无需单独设置")
	}
	if err := s.users.ReplaceMenus(id, menuIDs, logic.Stamp(operator)); err != nil {
		return err
	}
	// 立即失效该用户的权限快照，使新菜单与按钮权限在下次请求即生效。
	s.perms.Invalidate(id)
	return nil
}
