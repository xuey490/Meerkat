package v1

import (
	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/service"
)

// DictAPI 是字典管理的 HTTP 控制器。
type DictAPI struct {
	// dicts 字典应用服务。
	dicts *service.DictService
}

// NewDictAPI 创建字典控制器。
//
// 参数 Parameters:
//   - dicts (*service.DictService): 字典应用服务。
//
// 返回 Returns:
//   - api (*DictAPI): 字典控制器。
func NewDictAPI(dicts *service.DictService) *DictAPI { return &DictAPI{dicts: dicts} }

// Register 注册字典相关路由。
//
// 路径参数统一命名为 :dictCode（Gin 要求同一路径位置的参数名一致），
// 其中类型维度的接口承载的是字典类型主键，数据维度承载的是字典编码。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
//   - guard (Guard): 按权限标识生成中间件。
func (h *DictAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/dicts", h.TypeList)
	group.GET("/dicts/options", h.TypeOptions)
	group.GET("/dicts/:dictCode/items", h.ItemList)
	group.GET("/dicts/:dictCode/items/options", h.ItemOptions)
	group.GET("/dicts/:dictCode/items/:id/form", h.ItemForm)
	group.GET("/dicts/:dictCode/form", h.TypeForm)

	group.POST("/dicts", guard("sys:dict:create"), h.CreateType)
	group.PUT("/dicts/:dictCode", guard("sys:dict:update"), h.UpdateType)
	group.DELETE("/dicts/:dictCode", guard("sys:dict:delete"), h.DeleteTypes)
	group.POST("/dicts/:dictCode/items", guard("sys:dict:create"), h.CreateItem)
	group.PUT("/dicts/:dictCode/items/:id", guard("sys:dict:update"), h.UpdateItem)
	group.DELETE("/dicts/:dictCode/items/:ids", guard("sys:dict:delete"), h.DeleteItems)
}

// TypeList 处理 GET /api/v1/dicts：字典类型分页。
func (h *DictAPI) TypeList(c *gin.Context) {
	page, size := PageParams(c)
	result, err := h.dicts.TypePage(dto.DictQuery{
		PageQuery: dto.PageQuery{PageNum: page, PageSize: size},
		Keywords:  c.Query("keywords"),
	})
	if err != nil {
		Fail(c, err, "查询字典类型失败")
		return
	}
	OK(c, result)
}

// TypeOptions 处理 GET /api/v1/dicts/options：字典类型下拉。
func (h *DictAPI) TypeOptions(c *gin.Context) {
	options, err := h.dicts.TypeOptions()
	if err != nil {
		Fail(c, err, "查询字典类型失败")
		return
	}
	OK(c, options)
}

// TypeForm 处理 GET /api/v1/dicts/:dictCode/form：字典类型编辑表单。
func (h *DictAPI) TypeForm(c *gin.Context) {
	form, err := h.dicts.TypeForm(c.Param("dictCode"))
	if err != nil {
		Fail(c, err, "查询字典类型失败")
		return
	}
	OK(c, form)
}

// CreateType 处理 POST /api/v1/dicts：新增字典类型。
func (h *DictAPI) CreateType(c *gin.Context) {
	var req dto.DictTypeForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.dicts.CreateType(req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "创建字典类型失败")
		return
	}
	Created(c, gin.H{"id": id})
}

// UpdateType 处理 PUT /api/v1/dicts/:dictCode：更新字典类型。
func (h *DictAPI) UpdateType(c *gin.Context) {
	var req dto.DictTypeForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.dicts.UpdateType(c.Param("dictCode"), req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "更新字典类型失败")
		return
	}
	OK(c, gin.H{"id": id})
}

// DeleteTypes 处理 DELETE /api/v1/dicts/:dictCode：删除字典类型及其字典项。
func (h *DictAPI) DeleteTypes(c *gin.Context) {
	if err := h.dicts.DeleteTypes(c.Param("dictCode")); err != nil {
		Fail(c, err, "删除字典类型失败")
		return
	}
	OK(c, nil)
}

// ItemList 处理 GET /api/v1/dicts/:dictCode/items：字典项分页。
func (h *DictAPI) ItemList(c *gin.Context) {
	page, size := PageParams(c)
	result, err := h.dicts.ItemPage(c.Param("dictCode"), dto.DictItemQuery{
		PageQuery: dto.PageQuery{PageNum: page, PageSize: size},
		Keywords:  c.Query("keywords"),
	})
	if err != nil {
		Fail(c, err, "查询字典项失败")
		return
	}
	OK(c, result)
}

// ItemOptions 处理 GET /api/v1/dicts/:dictCode/items/options：字典项下拉数据源。
func (h *DictAPI) ItemOptions(c *gin.Context) {
	options, err := h.dicts.ItemOptions(c.Param("dictCode"))
	if err != nil {
		Fail(c, err, "查询字典项失败")
		return
	}
	OK(c, options)
}

// ItemForm 处理 GET /api/v1/dicts/:dictCode/items/:id/form：字典项编辑表单。
func (h *DictAPI) ItemForm(c *gin.Context) {
	form, err := h.dicts.ItemForm(c.Param("dictCode"), c.Param("id"))
	if err != nil {
		Fail(c, err, "查询字典项失败")
		return
	}
	OK(c, form)
}

// CreateItem 处理 POST /api/v1/dicts/:dictCode/items：新增字典项。
func (h *DictAPI) CreateItem(c *gin.Context) {
	var req dto.DictItemForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.dicts.CreateItem(c.Param("dictCode"), req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "创建字典项失败")
		return
	}
	Created(c, gin.H{"id": id})
}

// UpdateItem 处理 PUT /api/v1/dicts/:dictCode/items/:id：更新字典项。
func (h *DictAPI) UpdateItem(c *gin.Context) {
	var req dto.DictItemForm
	if !BindJSON(c, &req) {
		return
	}
	id, err := h.dicts.UpdateItem(c.Param("dictCode"), c.Param("id"), req, CurrentUser(c))
	if err != nil {
		Fail(c, err, "更新字典项失败")
		return
	}
	OK(c, gin.H{"id": id})
}

// DeleteItems 处理 DELETE /api/v1/dicts/:dictCode/items/:ids：删除字典项。
func (h *DictAPI) DeleteItems(c *gin.Context) {
	if err := h.dicts.DeleteItems(c.Param("dictCode"), c.Param("ids")); err != nil {
		Fail(c, err, "删除字典项失败")
		return
	}
	OK(c, nil)
}
