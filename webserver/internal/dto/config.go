package dto

import "github.com/company/monitor-webserver/internal/model"

// ConfigItem 对齐前端 ConfigItem（系统配置列表项）。
type ConfigItem struct {
	// ID 配置 ID。
	ID string `json:"id"`
	// ConfigName 配置名称。
	ConfigName string `json:"configName"`
	// ConfigKey 配置键。
	ConfigKey string `json:"configKey"`
	// ConfigValue 配置值。
	ConfigValue string `json:"configValue"`
}

// ConfigForm 对齐前端 ConfigForm。
type ConfigForm struct {
	// ID 配置 ID。
	ID string `json:"id"`
	// ConfigName 配置名称。
	ConfigName string `json:"configName"`
	// ConfigKey 配置键。
	ConfigKey string `json:"configKey"`
	// ConfigValue 配置值。
	ConfigValue string `json:"configValue"`
	// Remark 描述。
	Remark string `json:"remark"`
}

// ConfigQuery 是系统配置分页查询参数。
type ConfigQuery struct {
	// PageQuery 分页参数。
	PageQuery
	// Keywords 关键字（配置键/名称）。
	Keywords string `json:"keywords" form:"keywords"`
}

// NewConfigItem 把配置实体转换为前端列表项。
//
// 参数 Parameters:
//   - row (*model.SysConfig): 配置实体。
//
// 返回 Returns:
//   - item (ConfigItem): 配置列表项。
func NewConfigItem(row *model.SysConfig) ConfigItem {
	return ConfigItem{
		ID:          textID(row.ID),
		ConfigName:  textValue(row.Name),
		ConfigKey:   row.Key,
		ConfigValue: textValue(row.Value),
	}
}

// NewConfigForm 把配置实体转换为编辑表单。
//
// 参数 Parameters:
//   - row (*model.SysConfig): 配置实体。
//
// 返回 Returns:
//   - form (ConfigForm): 配置表单。
func NewConfigForm(row *model.SysConfig) ConfigForm {
	return ConfigForm{
		ID:          textID(row.ID),
		ConfigName:  textValue(row.Name),
		ConfigKey:   row.Key,
		ConfigValue: textValue(row.Value),
		Remark:      textValue(row.Remark),
	}
}
