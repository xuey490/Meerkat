package dto

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/company/monitor-webserver/internal/model"
)

// 前端菜单类型（web/src/enums/business.ts 的 MenuTypeEnum）。
const (
	// MenuTypeCatalog 目录。
	MenuTypeCatalog = "C"
	// MenuTypeMenu 菜单。
	MenuTypeMenu = "M"
	// MenuTypeButton 按钮/接口权限。
	MenuTypeButton = "B"
	// MenuTypeExternal 外链。
	MenuTypeExternal = "E"
)

// MenuParam 是菜单路由参数（前端 MenuForm.params 的元素）。
type MenuParam struct {
	// Key 参数名。
	Key string `json:"key"`
	// Value 参数值。
	Value string `json:"value"`
}

// MenuItem 对齐前端 MenuItem（菜单树节点，管理页使用）。
type MenuItem struct {
	// ID 菜单 ID。
	ID string `json:"id"`
	// ParentID 父菜单 ID。
	ParentID string `json:"parentId"`
	// Name 菜单名称。
	Name string `json:"name"`
	// Type 菜单类型 C/M/B/E。
	Type string `json:"type"`
	// Path 路由路径。
	Path string `json:"path"`
	// RoutePath 路由路径（前端自定义字段）。
	RoutePath string `json:"routePath"`
	// RouteName 路由名称。
	RouteName string `json:"routeName"`
	// Component 组件路径。
	Component string `json:"component"`
	// Redirect 跳转路径。
	Redirect string `json:"redirect"`
	// ExternalURL 外链地址。
	ExternalURL string `json:"externalUrl"`
	// Icon 图标。
	Icon string `json:"icon"`
	// Perm 权限标识。
	Perm string `json:"perm"`
	// Sort 排序。
	Sort int `json:"sort"`
	// Visible 是否显示：1 显示 / 0 隐藏。
	Visible int `json:"visible"`
	// Scope 菜单范围：1 平台（单租户部署固定为 1）。
	Scope int `json:"scope"`
	// AlwaysShow 目录是否始终显示。
	AlwaysShow FlagValue `json:"alwaysShow"`
	// KeepAlive 是否缓存。
	KeepAlive FlagValue `json:"keepAlive"`
	// Params 路由参数。
	Params []MenuParam `json:"params"`
	// Children 子菜单。
	Children []MenuItem `json:"children,omitempty"`
	// CreateTime 创建时间。
	CreateTime string `json:"createTime"`
	// UpdateTime 更新时间。
	UpdateTime string `json:"updateTime"`
}

// MenuForm 对齐前端 MenuForm（新增/编辑菜单表单）。
type MenuForm struct {
	// ID 菜单 ID。
	ID string `json:"id"`
	// ParentID 父菜单 ID。
	ParentID string `json:"parentId"`
	// Name 菜单名称。
	Name string `json:"name"`
	// Type 菜单类型 C/M/B/E。
	Type string `json:"type"`
	// Path 路由路径。
	Path string `json:"path"`
	// RouteName 路由名称。
	RouteName string `json:"routeName"`
	// RoutePath 路由路径（前端自定义字段）。
	RoutePath string `json:"routePath"`
	// Component 组件路径。
	Component string `json:"component"`
	// Redirect 跳转路径。
	Redirect string `json:"redirect"`
	// ExternalURL 外链地址。
	ExternalURL string `json:"externalUrl"`
	// Icon 图标。
	Icon string `json:"icon"`
	// Perm 权限标识。
	Perm string `json:"perm"`
	// Sort 排序。
	Sort int `json:"sort"`
	// Visible 是否显示。
	Visible int `json:"visible"`
	// Scope 菜单范围。
	Scope int `json:"scope"`
	// AlwaysShow 目录是否始终显示。
	AlwaysShow FlagValue `json:"alwaysShow"`
	// KeepAlive 是否缓存。
	KeepAlive FlagValue `json:"keepAlive"`
	// Params 路由参数。
	Params []MenuParam `json:"params"`
	// Status 状态。
	Status int `json:"status"`
	// Method 请求方式（按钮类菜单）。
	Method string `json:"method"`
}

// MenuQuery 是菜单查询参数（菜单为树形展示，仅支持关键字）。
type MenuQuery struct {
	// Keywords 关键字（菜单名称/权限标识）。
	Keywords string `json:"keywords" form:"keywords"`
	// Scope 菜单范围过滤。
	Scope string `json:"scope" form:"scope"`
}

// MenuOptionQuery 是菜单下拉选项查询参数。
type MenuOptionQuery struct {
	// OnlyParent 只返回目录节点。
	OnlyParent bool `json:"onlyParent" form:"onlyParent"`
	// Scope 菜单范围过滤。
	Scope string `json:"scope" form:"scope"`
}

// NewMenuItem 把菜单实体转换为前端菜单节点（不含子节点）。
//
// 参数 Parameters:
//   - menu (*model.SysMenu): 菜单实体。
//
// 返回 Returns:
//   - item (MenuItem): 前端菜单节点。
func NewMenuItem(menu *model.SysMenu) MenuItem {
	path := textValue(menu.Path)
	return MenuItem{
		ID:          textID(menu.ID),
		ParentID:    textID(menu.ParentID),
		Name:        menu.Name,
		Type:        MenuTypeFromDB(menu.Type),
		Path:        path,
		RoutePath:   path,
		RouteName:   textValue(menu.Code),
		Component:   textValue(menu.Component),
		Redirect:    textValue(menu.Redirect),
		ExternalURL: textValue(menu.LinkURL),
		Icon:        textValue(menu.Icon),
		Perm:        textValue(menu.Slug),
		Sort:        menu.Sort,
		Visible:     VisibleFromHidden(menu.IsHidden),
		Scope:       1, // 单租户部署，菜单范围固定为平台级
		AlwaysShow:  FlagValue(AlwaysShowFromFlag(menu.IsAlwaysShow)),
		KeepAlive:   FlagValue(KeepAliveFromFlag(menu.IsKeepAlive)),
		Params:      DecodeMenuParams(textValue(menu.Params)),
		CreateTime:  timeText(menu.CreateTime),
		UpdateTime:  timeText(menu.UpdateTime),
	}
}

// NewMenuForm 把菜单实体转换为编辑表单对象。
//
// 参数 Parameters:
//   - menu (*model.SysMenu): 菜单实体。
//
// 返回 Returns:
//   - form (MenuForm): 前端菜单表单。
func NewMenuForm(menu *model.SysMenu) MenuForm {
	path := textValue(menu.Path)
	return MenuForm{
		ID:          textID(menu.ID),
		ParentID:    textID(menu.ParentID),
		Name:        menu.Name,
		Type:        MenuTypeFromDB(menu.Type),
		Path:        path,
		RouteName:   textValue(menu.Code),
		RoutePath:   path,
		Component:   textValue(menu.Component),
		Redirect:    textValue(menu.Redirect),
		ExternalURL: textValue(menu.LinkURL),
		Icon:        textValue(menu.Icon),
		Perm:        textValue(menu.Slug),
		Sort:        menu.Sort,
		Visible:     VisibleFromHidden(menu.IsHidden),
		Scope:       1,
		AlwaysShow:  FlagValue(AlwaysShowFromFlag(menu.IsAlwaysShow)),
		KeepAlive:   FlagValue(KeepAliveFromFlag(menu.IsKeepAlive)),
		Params:      DecodeMenuParams(textValue(menu.Params)),
		Status:      menu.Status,
		Method:      textValue(menu.Method),
	}
}

// MenuTypeFromDB 把 sa_system_menu.type 转换为前端菜单类型。
//
// 参数 Parameters:
//   - value (int): 数据库类型，1 目录 / 2 菜单 / 3 按钮 / 4 外链。
//
// 返回 Returns:
//   - menuType (string): C / M / B / E；未知取值按菜单（M）处理。
func MenuTypeFromDB(value int) string {
	switch value {
	case 1:
		return MenuTypeCatalog
	case 3:
		return MenuTypeButton
	case 4:
		return MenuTypeExternal
	default:
		return MenuTypeMenu
	}
}

// MenuTypeToDB 把前端菜单类型转换为 sa_system_menu.type。
//
// 参数 Parameters:
//   - value (string): C / M / B / E，大小写不敏感。
//
// 返回 Returns:
//   - menuType (int): 1 目录 / 2 菜单 / 3 按钮 / 4 外链；未知取值按菜单处理。
func MenuTypeToDB(value string) int {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case MenuTypeCatalog:
		return 1
	case MenuTypeButton:
		return 3
	case MenuTypeExternal:
		return 4
	default:
		return 2
	}
}

// MenuTypeLabel 返回菜单类型的中文名称，用于操作日志与错误提示。
//
// 参数 Parameters:
//   - menuType (string): 前端菜单类型 C/M/B/E。
//
// 返回 Returns:
//   - label (string): 中文名称。
func MenuTypeLabel(menuType string) string {
	switch menuType {
	case MenuTypeCatalog:
		return "目录"
	case MenuTypeButton:
		return "按钮"
	case MenuTypeExternal:
		return "外链"
	default:
		return "菜单"
	}
}

// VisibleFromHidden 把 sa_system_menu.is_hidden 转换为前端 visible。
//
// 参数 Parameters:
//   - isHidden (int): 1 隐藏 / 2 显示。
//
// 返回 Returns:
//   - visible (int): 1 显示 / 0 隐藏。
func VisibleFromHidden(isHidden int) int {
	if flagBool(isHidden) {
		return 0
	}
	return 1
}

// HiddenFromVisible 把前端 visible 转换为 sa_system_menu.is_hidden。
//
// 参数 Parameters:
//   - visible (int): 1 显示 / 0 隐藏。
//
// 返回 Returns:
//   - isHidden (int): 1 隐藏 / 2 显示。
func HiddenFromVisible(visible int) int {
	if visible == 0 {
		return flagYes
	}
	return flagNo
}

// AlwaysShowFromFlag 把 is_always_show 转换为前端 alwaysShow（0/1 数字）。
//
// 参数 Parameters:
//   - flag (int): 1 是 / 2 否。
//
// 返回 Returns:
//   - value (int): 1 是 / 0 否。
func AlwaysShowFromFlag(flag int) int {
	if flagBool(flag) {
		return 1
	}
	return 0
}

// KeepAliveFromFlag 把 is_keep_alive 转换为前端 keepAlive（0/1 数字）。
//
// 参数 Parameters:
//   - flag (int): 1 是 / 2 否。
//
// 返回 Returns:
//   - value (int): 1 是 / 0 否。
func KeepAliveFromFlag(flag int) int {
	if flagBool(flag) {
		return 1
	}
	return 0
}

// FlagFromBool 把前端布尔值转换为 init.sql 的 1 是 / 2 否 标记。
//
// 参数 Parameters:
//   - value (bool): 前端布尔值。
//
// 返回 Returns:
//   - flag (int): 1 是 / 2 否。
func FlagFromBool(value bool) int {
	if value {
		return flagYes
	}
	return flagNo
}

// DecodeMenuParams 解析菜单路由参数 JSON。
//
// 参数 Parameters:
//   - raw (string): params 列内容，形如 [{"key":"id","value":"1"}]。
//
// 返回 Returns:
//   - params ([]MenuParam): 解析结果；空值或非法 JSON 返回空切片。
func DecodeMenuParams(raw string) []MenuParam {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return []MenuParam{}
	}
	var params []MenuParam
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return []MenuParam{}
	}
	if params == nil {
		return []MenuParam{}
	}
	return params
}

// EncodeMenuParams 序列化菜单路由参数。
//
// 参数 Parameters:
//   - params ([]MenuParam): 前端提交的参数列表。
//
// 返回 Returns:
//   - raw (string): JSON 字符串；参数为空时返回 "[]"。
func EncodeMenuParams(params []MenuParam) string {
	filtered := make([]MenuParam, 0, len(params))
	for _, param := range params {
		if strings.TrimSpace(param.Key) == "" && strings.TrimSpace(param.Value) == "" {
			continue
		}
		filtered = append(filtered, param)
	}
	if len(filtered) == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(filtered)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

// BuildMenuTree 把菜单列表组装为树，并按 sort、id 排序。
//
// 参数 Parameters:
//   - rows ([]model.SysMenu): 菜单实体列表。
//
// 返回 Returns:
//   - tree ([]MenuItem): 菜单树；入参为空时返回空切片。
func BuildMenuTree(rows []model.SysMenu) []MenuItem {
	nodes := make(map[int64]MenuItem, len(rows))
	order := make([]int64, 0, len(rows))
	for index := range rows {
		row := rows[index]
		nodes[row.ID] = NewMenuItem(&row)
		order = append(order, row.ID)
	}
	children := make(map[int64][]int64, len(rows))
	roots := make([]int64, 0, len(rows))
	for _, id := range order {
		row, ok := MenuByID(rows, id)
		if !ok {
			continue
		}
		if row.ParentID == 0 || nodes[row.ParentID].ID == "" {
			roots = append(roots, id)
			continue
		}
		children[row.ParentID] = append(children[row.ParentID], id)
	}
	var attach func(id int64) MenuItem
	attach = func(id int64) MenuItem {
		node := nodes[id]
		kids := children[id]
		if len(kids) > 0 {
			items := make([]MenuItem, 0, len(kids))
			for _, kid := range kids {
				items = append(items, attach(kid))
			}
			sort.SliceStable(items, func(i, j int) bool { return items[i].Sort < items[j].Sort })
			node.Children = items
		}
		return node
	}
	tree := make([]MenuItem, 0, len(roots))
	for _, root := range roots {
		tree = append(tree, attach(root))
	}
	sort.SliceStable(tree, func(i, j int) bool { return tree[i].Sort < tree[j].Sort })
	return tree
}

// MenuByID 在菜单列表中按主键查找元素。
//
// 参数 Parameters:
//   - rows ([]model.SysMenu): 菜单实体列表。
//   - id (int64): 目标主键。
//
// 返回 Returns:
//   - menu (model.SysMenu): 命中的菜单实体副本。
//   - found (bool): 是否命中。
func MenuByID(rows []model.SysMenu, id int64) (model.SysMenu, bool) {
	for index := range rows {
		if rows[index].ID == id {
			return rows[index], true
		}
	}
	return model.SysMenu{}, false
}

// BuildOptionTree 把（id,parentId,label）三元组组装为前端下拉树。
//
// 参数 Parameters:
//   - items ([]OptionItem): 扁平选项列表，其 Value 为节点 ID。
//   - parents (map[string]string): 节点 ID 到父节点 ID 的索引。
//
// 返回 Returns:
//   - tree ([]OptionItem): 选项树；入参为空时返回空切片。
func BuildOptionTree(items []OptionItem, parents map[string]string) []OptionItem {
	byID := make(map[string]OptionItem, len(items))
	order := make([]string, 0, len(items))
	for _, item := range items {
		node := item
		node.Children = nil
		byID[item.Value] = node
		order = append(order, item.Value)
	}
	children := make(map[string][]string, len(items))
	roots := make([]string, 0, len(items))
	for _, id := range order {
		parent := parents[id]
		if parent == "" || parent == id || byID[parent].Value == "" {
			roots = append(roots, id)
			continue
		}
		children[parent] = append(children[parent], id)
	}
	var attach func(id string) OptionItem
	attach = func(id string) OptionItem {
		node := byID[id]
		kids := children[id]
		if len(kids) > 0 {
			list := make([]OptionItem, 0, len(kids))
			for _, kid := range kids {
				list = append(list, attach(kid))
			}
			node.Children = list
		}
		return node
	}
	tree := make([]OptionItem, 0, len(roots))
	for _, root := range roots {
		tree = append(tree, attach(root))
	}
	return tree
}
