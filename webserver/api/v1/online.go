package v1

import (
	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/service"
)

// OnlineAPI 是在线用户的 HTTP 控制器。
type OnlineAPI struct {
	// online 在线用户应用服务。
	online *service.OnlineService
}

// NewOnlineAPI 创建在线用户控制器。
//
// 参数 Parameters:
//   - online (*service.OnlineService): 在线用户应用服务。
//
// 返回 Returns:
//   - api (*OnlineAPI): 在线用户控制器。
func NewOnlineAPI(online *service.OnlineService) *OnlineAPI { return &OnlineAPI{online: online} }

// Register 注册在线用户路由。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
//   - guard (Guard): 按权限标识生成中间件。
func (h *OnlineAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/online-users", guard("sys:online:list"), h.List)
	group.GET("/online-users/overview", guard("sys:online:list"), h.Overview)
	group.DELETE("/online-users/:id", guard("sys:online:kick"), h.Kick)
}

// List 处理 GET /api/v1/online-users：在线用户分页。
func (h *OnlineAPI) List(c *gin.Context) {
	page, size := PageParams(c)
	result, err := h.online.Page(dto.OnlineQuery{
		PageQuery: dto.PageQuery{PageNum: page, PageSize: size},
		Keywords:  c.Query("keywords"),
		Username:  c.Query("username"),
		IP:        c.Query("ip"),
	})
	if err != nil {
		Fail(c, err, "查询在线用户失败")
		return
	}
	OK(c, result)
}

// Overview 处理 GET /api/v1/online-users/overview：在线概览。
func (h *OnlineAPI) Overview(c *gin.Context) {
	overview, err := h.online.Overview()
	if err != nil {
		Fail(c, err, "查询在线概览失败")
		return
	}
	OK(c, overview)
}

// Kick 处理 DELETE /api/v1/online-users/:id：强制指定会话下线。
func (h *OnlineAPI) Kick(c *gin.Context) {
	if err := h.online.Kick(c.Request.Context(), c.Param("id"), CurrentUser(c)); err != nil {
		Fail(c, err, "强制下线失败")
		return
	}
	OK(c, nil)
}
