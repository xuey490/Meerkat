package logic

import (
	"strings"

	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// DictLogic 负责 sa_system_dict_type 与 sa_system_dict_data 的数据读写。
type DictLogic struct {
	// db 权限库会话。
	db *gorm.DB
}

// NewDictLogic 创建字典领域操作。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//
// 返回 Returns:
//   - logic (*DictLogic): 字典领域操作实例。
func NewDictLogic(db *gorm.DB) *DictLogic { return &DictLogic{db: db} }

// TypePage 分页查询字典类型。
//
// 参数 Parameters:
//   - q (dto.DictQuery): 查询条件。
//
// 返回 Returns:
//   - rows ([]model.SysDictType): 当前页字典类型。
//   - total (int64): 满足条件的总数。
//   - err (error): 查询失败时返回业务错误。
func (l *DictLogic) TypePage(q dto.DictQuery) ([]model.SysDictType, int64, error) {
	page, size := util.NormalizePage(q.PageNum, q.PageSize)
	db := l.db.Model(&model.SysDictType{})
	if pattern := util.LikeKeyword(q.Keywords); pattern != "" {
		db = db.Where(`(name LIKE ? ESCAPE '\' OR code LIKE ? ESCAPE '\')`, pattern, pattern)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, InternalErr("查询字典类型数量失败", err)
	}
	var rows []model.SysDictType
	if err := db.Order("id").Offset(util.Offset(page, size)).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, InternalErr("查询字典类型失败", err)
	}
	return rows, total, nil
}

// Types 返回全部字典类型，用于下拉。
//
// 返回 Returns:
//   - rows ([]model.SysDictType): 字典类型列表。
//   - err (error): 查询失败时返回业务错误。
func (l *DictLogic) Types() ([]model.SysDictType, error) {
	var rows []model.SysDictType
	if err := l.db.Order("id").Find(&rows).Error; err != nil {
		return nil, InternalErr("查询字典类型失败", err)
	}
	return rows, nil
}

// GetType 按主键查询字典类型。
//
// 参数 Parameters:
//   - id (int64): 字典类型主键。
//
// 返回 Returns:
//   - row (*model.SysDictType): 字典类型实体。
//   - err (error): 不存在时返回 NotFound 业务错误。
func (l *DictLogic) GetType(id int64) (*model.SysDictType, error) {
	var row model.SysDictType
	if err := l.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, TranslateDBError(err, "字典类型不存在", "查询字典类型失败")
	}
	return &row, nil
}

// TypeCodeExists 判断字典编码是否已存在。
//
// 参数 Parameters:
//   - code (string): 字典编码。
//
// 返回 Returns:
//   - exists (bool): 是否已存在。
//   - err (error): 查询失败时返回业务错误。
func (l *DictLogic) TypeCodeExists(code string) (bool, error) {
	var count int64
	// 唯一索引不含 delete_time，软删除字典同样占用编码，见 unique_key.go 说明。
	if err := l.db.Unscoped().Model(&model.SysDictType{}).Where("code = ?", code).Count(&count).Error; err != nil {
		return false, InternalErr("校验字典编码失败", err)
	}
	return count > 0, nil
}

// CreateType 新增字典类型。
//
// 参数 Parameters:
//   - row (*model.SysDictType): 字典类型实体（含审计字段）。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *DictLogic) CreateType(row *model.SysDictType) error {
	if err := l.db.Create(row).Error; err != nil {
		return UniqueOrInternal(err, "字典编码已存在", "创建字典类型失败")
	}
	return nil
}

// UpdateType 保存字典类型。
//
// 参数 Parameters:
//   - row (*model.SysDictType): 已修改的字典类型实体。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *DictLogic) UpdateType(row *model.SysDictType) error {
	if err := l.db.Save(row).Error; err != nil {
		return UniqueOrInternal(err, "字典编码已存在", "更新字典类型失败")
	}
	return nil
}

// DeleteTypes 删除字典类型及其字典项，并返回受影响的字典编码。
//
// 参数 Parameters:
//   - ids ([]int64): 字典类型主键列表。
//
// 返回 Returns:
//   - codes ([]string): 被删除的字典编码（用于 SSE 通知前端失效缓存）。
//   - err (error): 写入失败时返回业务错误。
func (l *DictLogic) DeleteTypes(ids []int64) ([]string, error) {
	codes := make([]string, 0, len(ids))
	if err := l.db.Model(&model.SysDictType{}).Where("id IN ?", ids).Pluck("code", &codes).Error; err != nil {
		return nil, InternalErr("查询字典类型失败", err)
	}
	err := l.db.Transaction(func(tx *gorm.DB) error {
		// 软删除同时释放字典编码，否则同编码字典无法再次创建。
		if err := softDeleteFreeKey(tx, "sa_system_dict_type", "code", ids); err != nil {
			return InternalErr("删除字典类型失败", err)
		}
		if len(codes) > 0 {
			if err := tx.Where("code IN ?", codes).Delete(&model.SysDictData{}).Error; err != nil {
				return InternalErr("删除字典项失败", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// ItemPage 按字典编码分页查询字典项。
//
// 参数 Parameters:
//   - dictCode (string): 字典编码。
//   - q (dto.DictItemQuery): 查询条件。
//
// 返回 Returns:
//   - rows ([]model.SysDictData): 当前页字典项。
//   - total (int64): 满足条件的总数。
//   - err (error): 查询失败时返回业务错误。
func (l *DictLogic) ItemPage(dictCode string, q dto.DictItemQuery) ([]model.SysDictData, int64, error) {
	page, size := util.NormalizePage(q.PageNum, q.PageSize)
	db := l.db.Model(&model.SysDictData{}).Where("code = ?", dictCode)
	if pattern := util.LikeKeyword(q.Keywords); pattern != "" {
		db = db.Where(`(label LIKE ? ESCAPE '\' OR value LIKE ? ESCAPE '\')`, pattern, pattern)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, InternalErr("查询字典项数量失败", err)
	}
	var rows []model.SysDictData
	if err := db.Order("sort").Order("id").Offset(util.Offset(page, size)).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, InternalErr("查询字典项失败", err)
	}
	return rows, total, nil
}

// EnabledItems 返回启用状态的字典项，用于前端下拉。
//
// 参数 Parameters:
//   - dictCode (string): 字典编码。
//
// 返回 Returns:
//   - rows ([]model.SysDictData): 字典项列表。
//   - err (error): 查询失败时返回业务错误。
func (l *DictLogic) EnabledItems(dictCode string) ([]model.SysDictData, error) {
	var rows []model.SysDictData
	if err := l.db.Where("code = ? AND status = 1", dictCode).
		Order("sort").Order("id").Find(&rows).Error; err != nil {
		return nil, InternalErr("查询字典项失败", err)
	}
	return rows, nil
}

// GetItem 按主键查询字典项。
//
// 参数 Parameters:
//   - id (int64): 字典项主键。
//
// 返回 Returns:
//   - row (*model.SysDictData): 字典项实体。
//   - err (error): 不存在时返回 NotFound 业务错误。
func (l *DictLogic) GetItem(id int64) (*model.SysDictData, error) {
	var row model.SysDictData
	if err := l.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, TranslateDBError(err, "字典项不存在", "查询字典项失败")
	}
	return &row, nil
}

// TypeIDByCode 按字典编码查询字典类型主键；类型不存在时返回 nil（允许只写字典数据）。
//
// 参数 Parameters:
//   - dictCode (string): 字典编码。
//
// 返回 Returns:
//   - id (*int64): 字典类型主键指针，可能为 nil。
//   - err (error): 查询失败时返回业务错误。
func (l *DictLogic) TypeIDByCode(dictCode string) (*int64, error) {
	var row model.SysDictType
	if err := l.db.Where("code = ?", dictCode).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, InternalErr("查询字典类型失败", err)
	}
	id := row.ID
	return &id, nil
}

// CreateItem 新增字典项。
//
// 参数 Parameters:
//   - row (*model.SysDictData): 字典项实体（含审计字段）。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *DictLogic) CreateItem(row *model.SysDictData) error {
	if err := l.db.Create(row).Error; err != nil {
		return InternalErr("创建字典项失败", err)
	}
	return nil
}

// UpdateItem 保存字典项。
//
// 参数 Parameters:
//   - row (*model.SysDictData): 已修改的字典项实体。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *DictLogic) UpdateItem(row *model.SysDictData) error {
	if err := l.db.Save(row).Error; err != nil {
		return InternalErr("更新字典项失败", err)
	}
	return nil
}

// DeleteItems 删除字典项。
//
// 参数 Parameters:
//   - ids ([]int64): 字典项主键列表。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *DictLogic) DeleteItems(ids []int64) error {
	if err := l.db.Where("id IN ?", ids).Delete(&model.SysDictData{}).Error; err != nil {
		return InternalErr("删除字典项失败", err)
	}
	return nil
}

// TrimCode 规范化字典编码输入。
//
// 参数 Parameters:
//   - dictCode (string): 原始字典编码。
//
// 返回 Returns:
//   - code (string): 去空格后的字典编码。
func TrimCode(dictCode string) string { return strings.TrimSpace(dictCode) }
