package logic

import (
	"strings"

	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// ConfigLogic 负责 sa_system_config 的数据读写。
type ConfigLogic struct {
	// db 权限库会话。
	db *gorm.DB
}

// NewConfigLogic 创建系统配置领域操作。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//
// 返回 Returns:
//   - logic (*ConfigLogic): 配置领域操作实例。
func NewConfigLogic(db *gorm.DB) *ConfigLogic { return &ConfigLogic{db: db} }

// Page 分页查询配置项。
//
// 参数 Parameters:
//   - q (dto.ConfigQuery): 查询条件。
//
// 返回 Returns:
//   - rows ([]model.SysConfig): 当前页配置。
//   - total (int64): 满足条件的总数。
//   - err (error): 查询失败时返回业务错误。
func (l *ConfigLogic) Page(q dto.ConfigQuery) ([]model.SysConfig, int64, error) {
	page, size := util.NormalizePage(q.PageNum, q.PageSize)
	db := l.db.Model(&model.SysConfig{})
	if pattern := util.LikeKeyword(q.Keywords); pattern != "" {
		db = db.Where(`(name LIKE ? ESCAPE '\' OR "key" LIKE ? ESCAPE '\')`, pattern, pattern)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, InternalErr("查询配置数量失败", err)
	}
	var rows []model.SysConfig
	if err := db.Order("sort").Order("id").Offset(util.Offset(page, size)).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, InternalErr("查询配置失败", err)
	}
	return rows, total, nil
}

// Get 按主键查询配置项。
//
// 参数 Parameters:
//   - id (int64): 配置主键。
//
// 返回 Returns:
//   - row (*model.SysConfig): 配置实体。
//   - err (error): 不存在时返回 NotFound 业务错误。
func (l *ConfigLogic) Get(id int64) (*model.SysConfig, error) {
	var row model.SysConfig
	if err := l.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, TranslateDBError(err, "配置不存在", "查询配置失败")
	}
	return &row, nil
}

// KeyExists 判断配置键是否已被占用（excludeID 用于编辑场景排除自身）。
//
// 参数 Parameters:
//   - key (string): 配置键。
//   - excludeID (int64): 需要排除的配置主键，0 表示不排除。
//
// 返回 Returns:
//   - exists (bool): 是否已存在。
//   - err (error): 查询失败时返回业务错误。
func (l *ConfigLogic) KeyExists(key string, excludeID int64) (bool, error) {
	// 唯一索引不含 delete_time，软删除配置同样占用配置键，见 unique_key.go 说明。
	db := l.db.Unscoped().Model(&model.SysConfig{}).Where(`"key" = ?`, key)
	if excludeID > 0 {
		db = db.Where("id <> ?", excludeID)
	}
	var count int64
	if err := db.Count(&count).Error; err != nil {
		return false, InternalErr("校验配置键失败", err)
	}
	return count > 0, nil
}

// Create 新增配置项。
//
// 参数 Parameters:
//   - row (*model.SysConfig): 配置实体（含审计字段）。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *ConfigLogic) Create(row *model.SysConfig) error {
	if err := l.db.Create(row).Error; err != nil {
		return UniqueOrInternal(err, "配置键已存在", "创建配置失败")
	}
	return nil
}

// Update 保存配置项。
//
// 参数 Parameters:
//   - row (*model.SysConfig): 已修改的配置实体。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *ConfigLogic) Update(row *model.SysConfig) error {
	if err := l.db.Save(row).Error; err != nil {
		return UniqueOrInternal(err, "配置键已存在", "更新配置失败")
	}
	return nil
}

// Delete 软删除配置项。
//
// 参数 Parameters:
//   - ids ([]int64): 配置主键列表。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *ConfigLogic) Delete(ids []int64) error {
	return l.db.Transaction(func(tx *gorm.DB) error {
		// 软删除同时释放配置键，否则同配置键无法再次创建。
		if err := softDeleteFreeKey(tx, "sa_system_config", `"key"`, ids); err != nil {
			return InternalErr("删除配置失败", err)
		}
		return nil
	})
}

// All 返回全部配置项，用于刷新缓存。
//
// 返回 Returns:
//   - rows ([]model.SysConfig): 按 sort、id 排序的配置。
//   - err (error): 查询失败时返回业务错误。
func (l *ConfigLogic) All() ([]model.SysConfig, error) {
	var rows []model.SysConfig
	if err := l.db.Order("sort").Order("id").Find(&rows).Error; err != nil {
		return nil, InternalErr("刷新配置缓存失败", err)
	}
	return rows, nil
}

// Key 返回配置键的去空格结果，便于复用校验逻辑。
//
// 参数 Parameters:
//   - value (string): 原始配置键。
//
// 返回 Returns:
//   - key (string): 去空格后的配置键。
func Key(value string) string { return strings.TrimSpace(value) }
