package dto

import (
	"strings"

	"github.com/company/monitor-webserver/internal/model"
)

// RouteMeta 对齐前端 Meta（动态路由 meta，见 web/src/api/system/menu/types.ts）。
type RouteMeta struct {
	// Title 路由标题。
	Title string `json:"title,omitempty"`
	// Icon 图标。
	Icon string `json:"icon,omitempty"`
	// Hidden 是否在侧边栏隐藏。
	Hidden bool `json:"hidden,omitempty"`
	// KeepAlive 是否开启页面缓存。
	KeepAlive bool `json:"keepAlive,omitempty"`
	// AlwaysShow 只有一个子路由时是否始终显示目录。
	AlwaysShow bool `json:"alwaysShow,omitempty"`
	// ExternalURL 外链地址。
	ExternalURL string `json:"externalUrl,omitempty"`
}

// RouteItem 对齐前端 RouteItem（GET /api/v1/menus/routes 的返回元素）。
//
// 前端 web/src/stores/permission.ts 依赖 path/name/component/redirect/children/meta：
// component 为 "Layout" 时挂载布局组件，其余按 views/** 的 glob 映射到页面组件。
type RouteItem struct {
	// Path 路由路径。
	Path string `json:"path"`
	// Name 路由名称。
	Name string `json:"name,omitempty"`
	// Component 组件路径。
	Component string `json:"component,omitempty"`
	// Redirect 跳转路径。
	Redirect string `json:"redirect,omitempty"`
	// Children 子路由。
	Children []RouteItem `json:"children,omitempty"`
	// Meta 路由元信息。
	Meta RouteMeta `json:"meta"`
}

// BuildRouteTree 把菜单实体列表转换为前端动态路由树。
//
// 参数 Parameters:
//   - rows ([]model.SysMenu): 已按 sort 排序的菜单实体列表。
//
// 返回 Returns:
//   - routes ([]RouteItem): 前端路由树；无菜单时返回空切片。
func BuildRouteTree(rows []model.SysMenu) []RouteItem {
	routes := buildRouteLevel(rows, 0)
	if routes == nil {
		return []RouteItem{}
	}
	return routes
}

// buildRouteLevel 递归构建某一层级的路由。
func buildRouteLevel(rows []model.SysMenu, parentID int64) []RouteItem {
	routes := make([]RouteItem, 0, 4)
	for index := range rows {
		menu := rows[index]
		if menu.ParentID != parentID {
			continue
		}
		component := textValue(menu.Component)
		path := textValue(menu.Path)
		if menu.Type == 1 && component == "" {
			// 目录型菜单未显式配置组件时按布局组件处理，否则前端会回退到 404 页。
			component = "Layout"
		}
		item := RouteItem{
			Path:      path,
			Name:      textValue(menu.Code),
			Component: component,
			Redirect:  textValue(menu.Redirect),
			Children:  buildRouteLevel(rows, menu.ID),
			Meta: RouteMeta{
				Title:      menu.Name,
				Icon:       textValue(menu.Icon),
				Hidden:     flagBool(menu.IsHidden),
				KeepAlive:  flagBool(menu.IsKeepAlive),
				AlwaysShow: flagBool(menu.IsAlwaysShow),
			},
		}
		if menu.Type == 4 || strings.TrimSpace(textValue(menu.LinkURL)) != "" {
			item.Meta.ExternalURL = textValue(menu.LinkURL)
			if item.Meta.ExternalURL != "" {
				item.Path = item.Meta.ExternalURL
			}
		}
		routes = append(routes, item)
	}
	if len(routes) == 0 {
		return nil
	}
	return routes
}
