package v1

import (
	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/service"
)

// LogAPI 是日志查询的 HTTP 控制器。
type LogAPI struct {
	// logs 日志应用服务。
	logs *service.LogService
}

// NewLogAPI 创建日志控制器。
//
// 参数 Parameters:
//   - logs (*service.LogService): 日志应用服务。
//
// 返回 Returns:
//   - api (*LogAPI): 日志控制器。
func NewLogAPI(logs *service.LogService) *LogAPI { return &LogAPI{logs: logs} }

// Register 注册日志相关路由。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
//   - guard (Guard): 按权限标识生成中间件。
func (h *LogAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/logs", h.OperList)
	group.GET("/logs/login", h.LoginList)
	group.GET("/logs/analytics/trend", h.Trend)
	group.GET("/logs/analytics/overview", h.Overview)
	group.DELETE("/logs/login/:ids", guard("sys:login-log:delete"), h.DeleteLoginLogs)
	group.DELETE("/logs/:ids", guard("sys:log:delete"), h.DeleteOperLogs)
}

// OperList 处理 GET /api/v1/logs：操作日志分页。
func (h *LogAPI) OperList(c *gin.Context) {
	page, size := PageParams(c)
	result, err := h.logs.OperPage(dto.LogQuery{
		PageQuery:  dto.PageQuery{PageNum: page, PageSize: size},
		Keywords:   c.Query("keywords"),
		CreateTime: c.QueryArray("createTime"),
	})
	if err != nil {
		Fail(c, err, "查询操作日志失败")
		return
	}
	OK(c, result)
}

// LoginList 处理 GET /api/v1/logs/login：登录日志分页。
func (h *LogAPI) LoginList(c *gin.Context) {
	page, size := PageParams(c)
	result, err := h.logs.LoginPage(dto.LoginLogQuery{
		PageQuery:  dto.PageQuery{PageNum: page, PageSize: size},
		Keywords:   c.Query("keywords"),
		Status:     c.Query("status"),
		CreateTime: c.QueryArray("createTime"),
	})
	if err != nil {
		Fail(c, err, "查询登录日志失败")
		return
	}
	OK(c, result)
}

// Trend 处理 GET /api/v1/logs/analytics/trend：访问趋势。
func (h *LogAPI) Trend(c *gin.Context) {
	trend, err := h.logs.Trend(dto.VisitTrendQuery{
		StartDate: c.Query("startDate"),
		EndDate:   c.Query("endDate"),
	})
	if err != nil {
		Fail(c, err, "统计访问趋势失败")
		return
	}
	OK(c, trend)
}

// Overview 处理 GET /api/v1/logs/analytics/overview：访问概览。
func (h *LogAPI) Overview(c *gin.Context) {
	overview, err := h.logs.Overview()
	if err != nil {
		Fail(c, err, "统计访问概览失败")
		return
	}
	OK(c, overview)
}

// DeleteOperLogs 处理 DELETE /api/v1/logs/:ids：删除操作日志。
func (h *LogAPI) DeleteOperLogs(c *gin.Context) {
	if err := h.logs.DeleteOperLogs(c.Param("ids")); err != nil {
		Fail(c, err, "删除操作日志失败")
		return
	}
	OK(c, nil)
}

// DeleteLoginLogs 处理 DELETE /api/v1/logs/login/:ids：删除登录日志。
func (h *LogAPI) DeleteLoginLogs(c *gin.Context) {
	if err := h.logs.DeleteLoginLogs(c.Param("ids")); err != nil {
		Fail(c, err, "删除登录日志失败")
		return
	}
	OK(c, nil)
}
