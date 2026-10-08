package service

import (
	"context"
	"strings"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/cachestore"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/logic"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// 字典缓存的键前缀（业务键，实际键名由 cachestore 再加全局前缀）。
const (
	// dictTypeCachePrefix 字典类型缓存前缀（monitor:dict:types）。
	dictTypeCachePrefix = "dict:types"
	// dictItemCachePrefix 字典项缓存前缀（monitor:dict:items:{code}）。
	dictItemCachePrefix = "dict:items:"
	// dictCachePrefix 字典缓存总前缀，类型变更时整体失效。
	dictCachePrefix = "dict:"
)

// Publisher 是字典变更通知的抽象（由 SSE 事件中心实现）。
//
// 字典变更后需要通知前端失效缓存（web/src/stores/dict.ts 订阅 dict 主题）。
type Publisher interface {
	// Publish 向指定主题广播消息。
	Publish(topic string, data any)
}

// DictTopic 是字典变更的 SSE 主题名。
const DictTopic = "dict"

// DictService 提供字典管理的应用服务。
type DictService struct {
	// dicts 字典领域操作。
	dicts *logic.DictLogic
	// events 事件发布器，可为 nil（未接入 SSE 时静默跳过）。
	events Publisher
	// cache 业务缓存（字典类型与字典项下拉数据），可为 nil（直连数据库）。
	cache *cachestore.Store
}

// NewDictService 创建字典应用服务。
//
// 参数 Parameters:
//   - dicts (*logic.DictLogic): 字典领域操作。
//   - events (Publisher): 事件发布器，可为 nil。
//   - cache (*cachestore.Store): 业务缓存，可为 nil。
//
// 返回 Returns:
//   - service (*DictService): 字典应用服务。
func NewDictService(dicts *logic.DictLogic, events Publisher, cache *cachestore.Store) *DictService {
	return &DictService{dicts: dicts, events: events, cache: cache}
}

// TypePage 分页查询字典类型。
//
// 参数 Parameters:
//   - q (dto.DictQuery): 查询条件。
//
// 返回 Returns:
//   - result (dto.PageResult[dto.DictTypeItem]): 分页结果。
//   - err (error): 查询失败时返回业务错误。
func (s *DictService) TypePage(q dto.DictQuery) (dto.PageResult[dto.DictTypeItem], error) {
	rows, total, err := s.dicts.TypePage(q)
	if err != nil {
		return dto.PageResult[dto.DictTypeItem]{}, err
	}
	list := make([]dto.DictTypeItem, 0, len(rows))
	for index := range rows {
		list = append(list, dto.NewDictTypeItem(&rows[index]))
	}
	return dto.PageResult[dto.DictTypeItem]{List: list, Total: total}, nil
}

// TypeOptions 返回字典类型下拉选项。
//
// 返回 Returns:
//   - options ([]dto.OptionItem): 字典类型选项。
//   - err (error): 查询失败时返回业务错误。
func (s *DictService) TypeOptions() ([]dto.OptionItem, error) {
	ctx := context.Background()
	var cached []dto.OptionItem
	if s.cache.Get(ctx, dictTypeCachePrefix, &cached) {
		return cached, nil
	}
	rows, err := s.dicts.Types()
	if err != nil {
		return nil, err
	}
	options := make([]dto.OptionItem, 0, len(rows))
	for index := range rows {
		row := rows[index]
		options = append(options, dto.OptionItem{Value: util.TextID(row.ID), Label: util.TextValue(row.Name)})
	}
	_ = s.cache.Set(ctx, dictTypeCachePrefix, options)
	return options, nil
}

// TypeForm 返回字典类型编辑表单（路径参数承载的是字典类型主键）。
//
// 参数 Parameters:
//   - idText (string): 字典类型 ID 文本。
//
// 返回 Returns:
//   - form (dto.DictTypeForm): 表单数据。
//   - err (error): ID 非法或类型不存在时返回业务错误。
func (s *DictService) TypeForm(idText string) (dto.DictTypeForm, error) {
	id, err := requiredID(idText, "字典类型 ID 无效")
	if err != nil {
		return dto.DictTypeForm{}, err
	}
	row, err := s.dicts.GetType(id)
	if err != nil {
		return dto.DictTypeForm{}, err
	}
	return dto.DictTypeForm{
		ID:       util.TextID(row.ID),
		Name:     util.TextValue(row.Name),
		DictCode: util.TextValue(row.Code),
		Status:   row.Status,
		Remark:   util.TextValue(row.Remark),
	}, nil
}

// CreateType 新增字典类型。
//
// 参数 Parameters:
//   - req (dto.DictTypeForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 新字典类型 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *DictService) CreateType(req dto.DictTypeForm, operator *model.SysUser) (string, error) {
	code := strings.TrimSpace(req.DictCode)
	if strings.TrimSpace(req.Name) == "" || code == "" {
		return "", apperr.Invalid("字典名称与字典编码不能为空")
	}
	exists, err := s.dicts.TypeCodeExists(code)
	if err != nil {
		return "", err
	}
	if exists {
		return "", apperr.Conflict("字典编码已存在")
	}
	row := model.SysDictType{
		Name:        util.NonEmptyPtr(req.Name),
		Code:        util.NonEmptyPtr(code),
		Status:      util.NormalizeStatus(req.Status),
		Remark:      util.NonEmptyPtr(req.Remark),
		AuditFields: logic.Stamp(operator),
	}
	if err := s.dicts.CreateType(&row); err != nil {
		return "", err
	}
	s.invalidateTypeCache()
	s.publish(code)
	return util.TextID(row.ID), nil
}

// UpdateType 更新字典类型。
//
// 参数 Parameters:
//   - idText (string): 字典类型 ID 文本。
//   - req (dto.DictTypeForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 字典类型 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *DictService) UpdateType(idText string, req dto.DictTypeForm, operator *model.SysUser) (string, error) {
	id, err := requiredID(idText, "字典类型 ID 无效")
	if err != nil {
		return "", err
	}
	row, err := s.dicts.GetType(id)
	if err != nil {
		return "", err
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		row.Name = util.NonEmptyPtr(name)
	}
	row.Code = util.NonEmptyPtr(req.DictCode)
	row.Status = util.NormalizeStatus(req.Status)
	row.Remark = util.NonEmptyPtr(req.Remark)
	row.UpdatedBy, row.UpdateTime = logic.Touch(operator)
	if err := s.dicts.UpdateType(row); err != nil {
		return "", err
	}
	s.invalidateTypeCache()
	s.publish(util.TextValue(row.Code))
	return util.TextID(row.ID), nil
}

// DeleteTypes 删除字典类型及其字典项。
//
// 参数 Parameters:
//   - idsText (string): 逗号分隔的字典类型 ID。
//
// 返回 Returns:
//   - err (error): ID 非法或写入失败时返回业务错误。
func (s *DictService) DeleteTypes(idsText string) error {
	ids := util.ParseIDList(idsText)
	if len(ids) == 0 {
		return apperr.Invalid("字典类型 ID 无效")
	}
	codes, err := s.dicts.DeleteTypes(ids)
	if err != nil {
		return err
	}
	s.invalidateTypeCache()
	for _, code := range codes {
		s.publish(code)
	}
	return nil
}

// ItemPage 分页查询字典项。
//
// 参数 Parameters:
//   - dictCode (string): 字典编码。
//   - q (dto.DictItemQuery): 查询条件。
//
// 返回 Returns:
//   - result (dto.PageResult[dto.DictItem]): 分页结果。
//   - err (error): 编码为空或查询失败时返回业务错误。
func (s *DictService) ItemPage(dictCode string, q dto.DictItemQuery) (dto.PageResult[dto.DictItem], error) {
	code := strings.TrimSpace(dictCode)
	if code == "" {
		return dto.PageResult[dto.DictItem]{}, apperr.Invalid("字典编码无效")
	}
	rows, total, err := s.dicts.ItemPage(code, q)
	if err != nil {
		return dto.PageResult[dto.DictItem]{}, err
	}
	list := make([]dto.DictItem, 0, len(rows))
	for index := range rows {
		item := dto.NewDictItem(&rows[index])
		item.DictCode = code
		list = append(list, item)
	}
	return dto.PageResult[dto.DictItem]{List: list, Total: total}, nil
}

// ItemOptions 返回字典项下拉数据（前端 DictSelect / DictTag 依赖）。
//
// 参数 Parameters:
//   - dictCode (string): 字典编码。
//
// 返回 Returns:
//   - options ([]dto.DictItemOption): 字典项选项。
//   - err (error): 编码为空或查询失败时返回业务错误。
func (s *DictService) ItemOptions(dictCode string) ([]dto.DictItemOption, error) {
	code := strings.TrimSpace(dictCode)
	if code == "" {
		return nil, apperr.Invalid("字典编码无效")
	}
	// 前端 DictSelect / DictTag 会高频拉取字典项，这里走缓存（变更时失效）。
	ctx := context.Background()
	cacheKey := dictItemCachePrefix + code
	var cached []dto.DictItemOption
	if s.cache.Get(ctx, cacheKey, &cached) {
		return cached, nil
	}
	rows, err := s.dicts.EnabledItems(code)
	if err != nil {
		return nil, err
	}
	options := make([]dto.DictItemOption, 0, len(rows))
	for index := range rows {
		options = append(options, dto.NewDictItemOption(&rows[index]))
	}
	_ = s.cache.Set(ctx, cacheKey, options)
	return options, nil
}

// ItemForm 返回字典项编辑表单。
//
// 参数 Parameters:
//   - dictCode (string): 字典编码（可为空，缺省时使用字典项自身保存的编码）。
//   - idText (string): 字典项 ID 文本。
//
// 返回 Returns:
//   - form (dto.DictItemForm): 表单数据。
//   - err (error): ID 非法或字典项不存在时返回业务错误。
func (s *DictService) ItemForm(dictCode, idText string) (dto.DictItemForm, error) {
	id, err := requiredID(idText, "字典项 ID 无效")
	if err != nil {
		return dto.DictItemForm{}, err
	}
	row, err := s.dicts.GetItem(id)
	if err != nil {
		return dto.DictItemForm{}, err
	}
	item := dto.NewDictItem(row)
	if code := strings.TrimSpace(dictCode); code != "" {
		item.DictCode = code
	}
	return dto.DictItemForm{
		ID:       item.ID,
		DictCode: item.DictCode,
		Label:    item.Label,
		Value:    item.Value,
		Status:   item.Status,
		Sort:     item.Sort,
		TagType:  item.TagType,
	}, nil
}

// CreateItem 新增字典项。
//
// 参数 Parameters:
//   - dictCode (string): 字典编码。
//   - req (dto.DictItemForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 新字典项 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *DictService) CreateItem(dictCode string, req dto.DictItemForm, operator *model.SysUser) (string, error) {
	code := strings.TrimSpace(dictCode)
	if code == "" {
		return "", apperr.Invalid("字典编码无效")
	}
	if strings.TrimSpace(req.Label) == "" || strings.TrimSpace(req.Value) == "" {
		return "", apperr.Invalid("字典标签与字典值不能为空")
	}
	typeID, err := s.dicts.TypeIDByCode(code)
	if err != nil {
		return "", err
	}
	row := model.SysDictData{
		TypeID:      typeID,
		Label:       util.NonEmptyPtr(req.Label),
		Value:       util.NonEmptyPtr(req.Value),
		Code:        util.NonEmptyPtr(code),
		TagType:     util.NonEmptyPtr(dto.DictTagTypeToCode(req.TagType)),
		Sort:        req.Sort,
		Status:      util.NormalizeStatus(req.Status),
		AuditFields: logic.Stamp(operator),
	}
	if err := s.dicts.CreateItem(&row); err != nil {
		return "", err
	}
	s.publish(code)
	return util.TextID(row.ID), nil
}

// UpdateItem 更新字典项。
//
// 参数 Parameters:
//   - dictCode (string): 字典编码（可为空）。
//   - idText (string): 字典项 ID 文本。
//   - req (dto.DictItemForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 字典项 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *DictService) UpdateItem(dictCode, idText string, req dto.DictItemForm, operator *model.SysUser) (string, error) {
	id, err := requiredID(idText, "字典项 ID 无效")
	if err != nil {
		return "", err
	}
	row, err := s.dicts.GetItem(id)
	if err != nil {
		return "", err
	}
	if label := strings.TrimSpace(req.Label); label != "" {
		row.Label = util.NonEmptyPtr(label)
	}
	row.Value = util.NonEmptyPtr(req.Value)
	row.TagType = util.NonEmptyPtr(dto.DictTagTypeToCode(req.TagType))
	row.Sort = req.Sort
	row.Status = util.NormalizeStatus(req.Status)
	row.UpdatedBy, row.UpdateTime = logic.Touch(operator)
	if err := s.dicts.UpdateItem(row); err != nil {
		return "", err
	}
	code := strings.TrimSpace(dictCode)
	if code == "" {
		code = util.TextValue(row.Code)
	}
	s.publish(code)
	return util.TextID(row.ID), nil
}

// DeleteItems 删除字典项。
//
// 参数 Parameters:
//   - dictCode (string): 字典编码。
//   - idsText (string): 逗号分隔的字典项 ID。
//
// 返回 Returns:
//   - err (error): ID 非法或写入失败时返回业务错误。
func (s *DictService) DeleteItems(dictCode, idsText string) error {
	ids := util.ParseIDList(idsText)
	if len(ids) == 0 {
		return apperr.Invalid("字典项 ID 无效")
	}
	if err := s.dicts.DeleteItems(ids); err != nil {
		return err
	}
	s.publish(strings.TrimSpace(dictCode))
	return nil
}

// publish 通知前端某个字典编码的数据已变更，并失效后端缓存。
//
// 参数 Parameters:
//   - dictCode (string): 字典编码。
func (s *DictService) publish(dictCode string) {
	code := strings.TrimSpace(dictCode)
	if code == "" {
		return
	}
	_ = s.cache.Delete(context.Background(), dictItemCachePrefix+code)
	if s.events == nil {
		return
	}
	s.events.Publish(DictTopic, map[string]string{"dictCode": code})
}

// invalidateTypeCache 失效字典类型缓存（类型新增/改名/删除时调用）。
func (s *DictService) invalidateTypeCache() {
	_, _ = s.cache.DeletePrefix(context.Background(), dictTypeCachePrefix)
}
