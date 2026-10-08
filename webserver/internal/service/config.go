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

// configCacheKey 是全量配置的缓存键（monitor:config:all）。
const configCacheKey = "config:all"

// ConfigService 提供系统配置的应用服务。
type ConfigService struct {
	// configs 配置领域操作。
	configs *logic.ConfigLogic
	// cache 业务缓存（全量配置键值），可为 nil（直连数据库）。
	cache *cachestore.Store
}

// NewConfigService 创建系统配置应用服务。
//
// 参数 Parameters:
//   - configs (*logic.ConfigLogic): 配置领域操作。
//   - cache (*cachestore.Store): 业务缓存，可为 nil。
//
// 返回 Returns:
//   - service (*ConfigService): 配置应用服务。
func NewConfigService(configs *logic.ConfigLogic, cache *cachestore.Store) *ConfigService {
	return &ConfigService{configs: configs, cache: cache}
}

// Page 分页查询配置项。
//
// 参数 Parameters:
//   - q (dto.ConfigQuery): 查询条件。
//
// 返回 Returns:
//   - result (dto.PageResult[dto.ConfigItem]): 分页结果。
//   - err (error): 查询失败时返回业务错误。
func (s *ConfigService) Page(q dto.ConfigQuery) (dto.PageResult[dto.ConfigItem], error) {
	rows, total, err := s.configs.Page(q)
	if err != nil {
		return dto.PageResult[dto.ConfigItem]{}, err
	}
	list := make([]dto.ConfigItem, 0, len(rows))
	for index := range rows {
		list = append(list, dto.NewConfigItem(&rows[index]))
	}
	return dto.PageResult[dto.ConfigItem]{List: list, Total: total}, nil
}

// Form 返回配置编辑表单。
//
// 参数 Parameters:
//   - idText (string): 配置 ID 文本。
//
// 返回 Returns:
//   - form (dto.ConfigForm): 表单数据。
//   - err (error): ID 非法或配置不存在时返回业务错误。
func (s *ConfigService) Form(idText string) (dto.ConfigForm, error) {
	id, err := requiredID(idText, "配置 ID 无效")
	if err != nil {
		return dto.ConfigForm{}, err
	}
	row, err := s.configs.Get(id)
	if err != nil {
		return dto.ConfigForm{}, err
	}
	return dto.NewConfigForm(row), nil
}

// Create 新增配置项。
//
// 参数 Parameters:
//   - req (dto.ConfigForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 新配置 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *ConfigService) Create(req dto.ConfigForm, operator *model.SysUser) (string, error) {
	key := strings.TrimSpace(req.ConfigKey)
	if key == "" || strings.TrimSpace(req.ConfigName) == "" {
		return "", apperr.Invalid("配置名称与配置键不能为空")
	}
	exists, err := s.configs.KeyExists(key, 0)
	if err != nil {
		return "", err
	}
	if exists {
		return "", apperr.Conflict("配置键已存在")
	}
	row := model.SysConfig{
		Key:         key,
		Name:        util.NonEmptyPtr(req.ConfigName),
		Value:       util.NonEmptyPtr(req.ConfigValue),
		Remark:      util.NonEmptyPtr(req.Remark),
		AuditFields: logic.Stamp(operator),
	}
	if err := s.configs.Create(&row); err != nil {
		return "", err
	}
	s.invalidate()
	return util.TextID(row.ID), nil
}

// Update 更新配置项。
//
// 参数 Parameters:
//   - idText (string): 配置 ID 文本。
//   - req (dto.ConfigForm): 前端表单。
//   - operator (*model.SysUser): 当前操作人。
//
// 返回 Returns:
//   - id (string): 配置 ID。
//   - err (error): 校验或写入失败时返回业务错误。
func (s *ConfigService) Update(idText string, req dto.ConfigForm, operator *model.SysUser) (string, error) {
	id, err := requiredID(idText, "配置 ID 无效")
	if err != nil {
		return "", err
	}
	row, err := s.configs.Get(id)
	if err != nil {
		return "", err
	}
	if name := strings.TrimSpace(req.ConfigName); name != "" {
		row.Name = util.NonEmptyPtr(name)
	}
	if key := strings.TrimSpace(req.ConfigKey); key != "" && key != row.Key {
		exists, err := s.configs.KeyExists(key, row.ID)
		if err != nil {
			return "", err
		}
		if exists {
			return "", apperr.Conflict("配置键已存在")
		}
		row.Key = key
	}
	row.Value = util.NonEmptyPtr(req.ConfigValue)
	row.Remark = util.NonEmptyPtr(req.Remark)
	row.UpdatedBy, row.UpdateTime = logic.Touch(operator)
	if err := s.configs.Update(row); err != nil {
		return "", err
	}
	s.invalidate()
	return util.TextID(row.ID), nil
}

// Delete 删除配置项。
//
// 参数 Parameters:
//   - idsText (string): 逗号分隔的配置 ID。
//
// 返回 Returns:
//   - err (error): ID 非法或写入失败时返回业务错误。
func (s *ConfigService) Delete(idsText string) error {
	ids := util.ParseIDList(idsText)
	if len(ids) == 0 {
		return apperr.Invalid("配置 ID 无效")
	}
	if err := s.configs.Delete(ids); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// RefreshCache 重新从数据库加载全量配置并写入缓存，返回键值供前端确认。
//
// 返回 Returns:
//   - data (map[string]any): 形如 {"configs": {...}, "total": n} 的响应数据。
//   - err (error): 查询失败时返回业务错误。
func (s *ConfigService) RefreshCache() (map[string]any, error) {
	items, err := s.reload(context.Background())
	if err != nil {
		return nil, err
	}
	return map[string]any{"configs": items, "total": len(items)}, nil
}

// PrimeCache 启动预热：把全量配置写入缓存（Redis 关闭时写入进程内缓存）。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//
// 返回 Returns:
//   - err (error): 查询失败时返回业务错误。
func (s *ConfigService) PrimeCache(ctx context.Context) error {
	_, err := s.reload(ctx)
	return err
}

// ConfigMap 返回全量配置键值（优先读缓存，未命中回源数据库并写缓存）。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//
// 返回 Returns:
//   - items (map[string]string): 配置键值。
//   - err (error): 查询失败时返回业务错误。
func (s *ConfigService) ConfigMap(ctx context.Context) (map[string]string, error) {
	var cached map[string]string
	if s.cache.Get(ctx, configCacheKey, &cached) {
		return cached, nil
	}
	return s.reload(ctx)
}

// reload 从数据库加载全量配置并刷新缓存。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//
// 返回 Returns:
//   - items (map[string]string): 配置键值。
//   - err (error): 查询失败时返回业务错误。
func (s *ConfigService) reload(ctx context.Context) (map[string]string, error) {
	rows, err := s.configs.All()
	if err != nil {
		return nil, err
	}
	items := make(map[string]string, len(rows))
	for _, row := range rows {
		items[row.Key] = util.TextValue(row.Value)
	}
	_ = s.cache.Set(ctx, configCacheKey, items)
	return items, nil
}

// invalidate 失效全量配置缓存（配置增删改后调用）。
func (s *ConfigService) invalidate() {
	_ = s.cache.Delete(context.Background(), configCacheKey)
}
