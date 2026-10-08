package dto

import (
	"sort"

	"github.com/company/monitor-webserver/internal/model"
)

// DeptItem 对齐前端 DeptItem（部门树节点）。
type DeptItem struct {
	// ID 部门 ID。
	ID string `json:"id"`
	// Name 部门名称。
	Name string `json:"name"`
	// ParentID 父部门 ID。
	ParentID string `json:"parentId"`
	// Code 部门编码。
	Code string `json:"code"`
	// Sort 排序。
	Sort int `json:"sort"`
	// Status 状态。
	Status int `json:"status"`
	// TreePath 祖级列表。
	TreePath string `json:"treePath"`
	// CreateTime 创建时间。
	CreateTime string `json:"createTime"`
	// UpdateTime 更新时间。
	UpdateTime string `json:"updateTime"`
	// Children 子部门。
	Children []DeptItem `json:"children,omitempty"`
}

// DeptForm 对齐前端 DeptForm（新增/编辑部门表单）。
type DeptForm struct {
	// ID 部门 ID。
	ID string `json:"id"`
	// Name 部门名称。
	Name string `json:"name"`
	// Code 部门编码。
	Code string `json:"code"`
	// ParentID 父部门 ID。
	ParentID string `json:"parentId"`
	// Sort 排序。
	Sort int `json:"sort"`
	// Status 状态。
	Status int `json:"status"`
	// LeaderID 负责人用户 ID。
	LeaderID string `json:"leaderId"`
	// Remark 备注。
	Remark string `json:"remark"`
}

// DeptQuery 是部门查询参数（部门为树形展示，只支持关键字）。
type DeptQuery struct {
	// Keywords 关键字（部门名称/编码）。
	Keywords string `json:"keywords" form:"keywords"`
	// Status 状态过滤。
	Status string `json:"status" form:"status"`
}

// NewDeptItem 把部门实体转换为前端树节点（不含子节点）。
//
// 参数 Parameters:
//   - dept (*model.SysDept): 部门实体。
//
// 返回 Returns:
//   - item (DeptItem): 前端部门节点。
func NewDeptItem(dept *model.SysDept) DeptItem {
	return DeptItem{
		ID:         textID(dept.ID),
		Name:       dept.Name,
		ParentID:   textID(dept.ParentID),
		Code:       textValue(dept.Code),
		Sort:       dept.Sort,
		Status:     dept.Status,
		TreePath:   dept.Level,
		CreateTime: timeText(dept.CreateTime),
		UpdateTime: timeText(dept.UpdateTime),
	}
}

// BuildDeptTree 把部门列表组装为树，并按 sort、name 排序。
//
// 参数 Parameters:
//   - rows ([]model.SysDept): 部门实体列表。
//
// 返回 Returns:
//   - tree ([]DeptItem): 部门树；入参为空时返回空切片。
func BuildDeptTree(rows []model.SysDept) []DeptItem {
	nodes := make(map[int64]DeptItem, len(rows))
	order := make([]int64, 0, len(rows))
	for index := range rows {
		row := rows[index]
		nodes[row.ID] = NewDeptItem(&row)
		order = append(order, row.ID)
	}
	children := make(map[int64][]int64, len(rows))
	roots := make([]int64, 0, len(rows))
	for _, id := range order {
		node := nodes[id]
		parentIDs := parseIDText(node.ParentID)
		if len(parentIDs) == 0 || nodes[parentIDs[0]].ID == "" || parentIDs[0] == id {
			roots = append(roots, id)
			continue
		}
		children[parentIDs[0]] = append(children[parentIDs[0]], id)
	}
	var attach func(id int64) DeptItem
	attach = func(id int64) DeptItem {
		node := nodes[id]
		kids := children[id]
		if len(kids) > 0 {
			items := make([]DeptItem, 0, len(kids))
			for _, kid := range kids {
				items = append(items, attach(kid))
			}
			sortDeptItems(items)
			node.Children = items
		}
		return node
	}
	tree := make([]DeptItem, 0, len(roots))
	for _, root := range roots {
		tree = append(tree, attach(root))
	}
	sortDeptItems(tree)
	return tree
}

// sortDeptItems 按 sort、name 稳定排序部门节点。
//
// 参数 Parameters:
//   - items ([]DeptItem): 待排序节点切片（原地排序）。
func sortDeptItems(items []DeptItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Sort == items[j].Sort {
			return items[i].Name < items[j].Name
		}
		return items[i].Sort < items[j].Sort
	})
}
