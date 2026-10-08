package v1

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/eventhub"
	"github.com/company/monitor-webserver/internal/monitor"
)

// monitorUnavailableCode 是监控库不可用时的业务码（保持与迁移前一致）。
const monitorUnavailableCode = "B0200"

// MonitorAPI 是监控模块的 HTTP 控制器。
//
// 监控模块是平台核心能力，本控制器的响应码、提示语与查询语义与迁移前逐字保持一致。
type MonitorAPI struct {
	// monitor 监控查询服务。
	monitor *monitor.Service
	// events SSE 广播中心。
	events *eventhub.Hub
}

// NewMonitorAPI 创建监控控制器。
//
// 参数 Parameters:
//   - monitorService (*monitor.Service): 监控查询服务。
//   - events (*eventhub.Hub): SSE 广播中心。
//
// 返回 Returns:
//   - api (*MonitorAPI): 监控控制器。
func NewMonitorAPI(monitorService *monitor.Service, events *eventhub.Hub) *MonitorAPI {
	return &MonitorAPI{monitor: monitorService, events: events}
}

// Register 注册监控相关路由（全部需要登录，但不做权限标识限制，保持原行为）。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
//   - guard (Guard): 按权限标识生成中间件（监控路由不使用）。
func (h *MonitorAPI) Register(group *gin.RouterGroup, guard Guard) {
	_ = guard
	group.GET("/monitor/overview", h.Overview)
	group.GET("/monitor/agents", h.Agents)
	group.GET("/monitor/agents/:id", h.AgentDetail)
	group.GET("/monitor/agents/:id/metrics", h.Metrics)
	group.GET("/monitor/services/:id", h.Services)
	group.GET("/monitor/containers/:id", h.Containers)
	group.GET("/monitor/alerts", h.Alerts)
	group.GET("/monitor/maintenance", h.Maintenance)
	group.GET("/sse/connect", h.SSE)
}

// Overview 处理 GET /api/v1/monitor/overview：监控总览。
func (h *MonitorAPI) Overview(c *gin.Context) {
	result, err := h.monitor.Overview()
	if err != nil {
		write(c, http.StatusBadGateway, monitorUnavailableCode, "查询监控状态失败", nil)
		return
	}
	OK(c, result)
}

// Agents 处理 GET /api/v1/monitor/agents：服务器列表。
func (h *MonitorAPI) Agents(c *gin.Context) {
	page, size := PageParams(c)
	result, err := h.monitor.Agents(page, size, c.Query("keywords"), c.Query("state"))
	if err != nil {
		write(c, http.StatusBadGateway, monitorUnavailableCode, "查询服务器失败", nil)
		return
	}
	OK(c, result)
}

// AgentDetail 处理 GET /api/v1/monitor/agents/:id：服务器详情。
func (h *MonitorAPI) AgentDetail(c *gin.Context) {
	agentID := strings.TrimSpace(c.Param("id"))
	if agentID == "" || agentID == "undefined" || agentID == "null" {
		BadRequest(c, "服务器 ID 无效")
		return
	}
	agent, services, containers, processes, temperatures, err := h.monitor.AgentDetail(agentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(c, "服务器不存在")
			return
		}
		write(c, http.StatusBadGateway, monitorUnavailableCode, "查询服务器失败", nil)
		return
	}
	OK(c, gin.H{
		"agent": agent, "services": services, "containers": containers,
		"processes": processes, "temperatures": temperatures,
	})
}

// Services 处理 GET /api/v1/monitor/services/:id：服务探测状态。
func (h *MonitorAPI) Services(c *gin.Context) {
	rows, err := h.monitor.Services(c.Param("id"))
	if err != nil {
		write(c, http.StatusBadGateway, monitorUnavailableCode, "查询服务状态失败", nil)
		return
	}
	OK(c, rows)
}

// Containers 处理 GET /api/v1/monitor/containers/:id：容器状态。
func (h *MonitorAPI) Containers(c *gin.Context) {
	rows, err := h.monitor.Containers(c.Param("id"))
	if err != nil {
		write(c, http.StatusBadGateway, monitorUnavailableCode, "查询容器状态失败", nil)
		return
	}
	OK(c, rows)
}

// Alerts 处理 GET /api/v1/monitor/alerts：告警列表。
func (h *MonitorAPI) Alerts(c *gin.Context) {
	page, size := PageParams(c)
	result, err := h.monitor.Alerts(page, size, c.Query("state"), c.Query("agentId"))
	if err != nil {
		write(c, http.StatusBadGateway, monitorUnavailableCode, "查询告警失败", nil)
		return
	}
	OK(c, result)
}

// Maintenance 处理 GET /api/v1/monitor/maintenance：维护窗口。
func (h *MonitorAPI) Maintenance(c *gin.Context) {
	rows, err := h.monitor.Maintenance()
	if err != nil {
		write(c, http.StatusBadGateway, monitorUnavailableCode, "查询维护窗口失败", nil)
		return
	}
	OK(c, rows)
}

// Metrics 处理 GET /api/v1/monitor/agents/:id/metrics：指标曲线（InfluxDB）。
func (h *MonitorAPI) Metrics(c *gin.Context) {
	measurement := c.DefaultQuery("measurement", "cpu")
	points, err := h.monitor.Metrics(measurement, c.Param("id"), c.DefaultQuery("start", "-1h"))
	if err != nil {
		switch {
		case errors.Is(err, monitor.ErrInvalidMeasurement), errors.Is(err, monitor.ErrInvalidAgent),
			errors.Is(err, monitor.ErrInvalidRange):
			BadRequest(c, err.Error())
		default:
			write(c, http.StatusBadGateway, "B0201", "查询时序数据失败", nil)
		}
		return
	}
	OK(c, points)
}

// SSE 处理 GET /api/v1/sse/connect：Server-Sent Events 长连接。
func (h *MonitorAPI) SSE(c *gin.Context) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		write(c, http.StatusInternalServerError, "B0001", "当前服务器不支持 SSE", nil)
		return
	}
	ch := h.events.Subscribe()
	defer h.events.Unsubscribe(ch)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case payload := <-ch:
			_, _ = c.Writer.Write(payload)
			flusher.Flush()
		case <-time.After(20 * time.Second):
			_, _ = c.Writer.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}
