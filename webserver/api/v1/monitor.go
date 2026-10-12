package v1

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/eventhub"
	"github.com/company/monitor-webserver/internal/monitor"
	"github.com/company/monitor-webserver/internal/util"
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

// Register 注册监控相关路由。
//
// 查询类接口只要求登录、不做权限标识限制（保持原行为）；破坏性接口（清理采集数据）
// 单独挂权限标识——能看监控曲线不等于可以删掉历史数据。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
//   - guard (Guard): 按权限标识生成中间件。
func (h *MonitorAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/monitor/overview", h.Overview)
	group.GET("/monitor/agents", h.Agents)
	group.GET("/monitor/agents/:id", h.AgentDetail)
	group.GET("/monitor/agents/:id/snapshot", h.Snapshot)
	group.GET("/monitor/agents/:id/metrics", h.Metrics)
	group.GET("/monitor/agents/:id/docker", h.DockerStatus)
	group.GET("/monitor/services/:id", h.Services)
	group.GET("/monitor/containers/:id", h.Containers)
	group.GET("/monitor/alerts", h.Alerts)
	group.GET("/monitor/maintenance", h.Maintenance)
	group.GET("/sse/connect", h.SSE)
	// 清理某台主机的采集数据（时序库指标 + 采集回执）：不可逆，需要独立权限标识。
	group.DELETE("/monitor/agents/:id/metrics", guard("monitor:metrics:clean"), h.CleanAgentMetrics)
}

// CleanAgentMetrics 处理 DELETE /api/v1/monitor/agents/:id/metrics：清理该主机的采集数据。
//
// 会删除两处：时序库中该 agent_id 的全部指标，以及 PostgreSQL 里该主机的采集回执
// （Collector 的 (agent_id, sequence) 幂等表）。服务器记录（agents）与告警记录不受影响。
// 非 GET 请求由操作日志中间件自动审计，另记一条结构化日志便于与采集链路日志对照。
func (h *MonitorAPI) CleanAgentMetrics(c *gin.Context) {
	agentID := strings.TrimSpace(c.Param("id"))
	if agentID == "" {
		BadRequest(c, "主机标识不能为空")
		return
	}
	result, err := h.monitor.CleanAgentMetrics(c.Request.Context(), agentID)
	if err != nil {
		switch {
		case errors.Is(err, monitor.ErrInfluxDeleteFailed):
			write(c, http.StatusBadGateway, "B0201", monitor.ErrInfluxDeleteFailed.Error(), nil)
		case errors.Is(err, monitor.ErrMonitorDBDeleteFailed):
			write(c, http.StatusBadGateway, monitorUnavailableCode, monitor.ErrMonitorDBDeleteFailed.Error(), nil)
		default:
			Fail(c, err, "清理采集数据失败")
		}
		return
	}
	slog.Info("已清理主机采集数据",
		"agent_id", agentID, "operator", operatorName(c), "client_ip", ClientIP(c),
		"receipts_deleted", result.ReceiptsDeleted, "influx_cleared", result.InfluxCleared)
	OK(c, result)
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
//
// 支持的查询参数：pageNum/pageSize、agentId、state、level、acknowledged、
// scope（active / history）、keyword（模糊匹配主机、指标与目标）。
func (h *MonitorAPI) Alerts(c *gin.Context) {
	page, size := PageParams(c)
	acknowledged, err := parseOptionalBool(c.Query("acknowledged"))
	if err != nil {
		BadRequest(c, err.Error())
		return
	}
	result, err := h.monitor.Alerts(monitor.AlertQuery{
		Page:         page,
		Size:         size,
		AgentID:      strings.TrimSpace(c.Query("agentId")),
		State:        strings.TrimSpace(c.Query("state")),
		Level:        strings.TrimSpace(c.Query("level")),
		Acknowledged: acknowledged,
		Scope:        strings.TrimSpace(c.Query("scope")),
		Keyword:      strings.TrimSpace(c.Query("keyword")),
	})
	if err != nil {
		if isMetricQueryError(err) {
			BadRequest(c, err.Error())
			return
		}
		write(c, http.StatusBadGateway, monitorUnavailableCode, "查询告警失败", nil)
		return
	}
	OK(c, result)
}

// parseOptionalBool 解析「可空布尔」形式的查询参数。
//
// 参数 Parameters:
//   - raw (string): 原始取值；空串表示不过滤。
//
// 返回 Returns:
//   - value (*bool): 解析结果；空串时返回 nil（表示全部）。
//   - err (error): 取值不是布尔字面量时返回 monitor.ErrInvalidFilter。
func parseOptionalBool(raw string) (*bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, monitor.ErrInvalidFilter
	}
	return &parsed, nil
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
//
// 支持的查询参数：
//   - measurement：指标类型，默认 cpu，必须在白名单内；
//   - start / end：查询区间，默认 start=-1h，end 缺省表示到 now()；
//   - field：字段白名单，逗号分隔（例如 usage_user,usage_system）；
//   - aggregate：聚合方式 mean/max/min/sum/last/first/count，或 rate（计数器转每秒速率）；
//   - window：聚合窗口（1m / 5m / 1h…），有 aggregate 时默认 1m；
//   - limit：单序列返回点数上限，默认 5000，最大 20000。
func (h *MonitorAPI) Metrics(c *gin.Context) {
	query := monitor.MetricQuery{
		Measurement: c.DefaultQuery("measurement", "cpu"),
		AgentID:     c.Param("id"),
		Start:       c.DefaultQuery("start", "-1h"),
		End:         c.Query("end"),
		Fields:      monitor.SplitFields(c.Query("field")),
		Aggregate:   c.Query("aggregate"),
		Window:      c.Query("window"),
		Limit:       util.AtoiSafe(c.Query("limit"), 0),
	}
	points, err := h.monitor.Metrics(query)
	if err != nil {
		if isMetricQueryError(err) {
			BadRequest(c, err.Error())
			return
		}
		write(c, http.StatusBadGateway, "B0201", "查询时序数据失败", nil)
		return
	}
	OK(c, points)
}

// Snapshot 处理 GET /api/v1/monitor/agents/:id/snapshot：主机概况当前值。
//
// 一次性返回 CPU / 内存 / 交换分区 / 负载 / 进程 / 连接数 / 磁盘 / 磁盘 IO / 网卡速率，
// 供详情页「主机概况」页签渲染，避免前端为十几张卡片轮流发起 InfluxDB 查询。
func (h *MonitorAPI) Snapshot(c *gin.Context) {
	agentID := strings.TrimSpace(c.Param("id"))
	if agentID == "" || agentID == "undefined" || agentID == "null" {
		BadRequest(c, monitor.ErrInvalidAgent.Error())
		return
	}
	snapshot, err := h.monitor.Snapshot(agentID)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			NotFound(c, "服务器不存在")
		case isMetricQueryError(err):
			BadRequest(c, err.Error())
		default:
			write(c, http.StatusBadGateway, "B0201", "查询时序数据失败", nil)
		}
		return
	}
	OK(c, snapshot)
}

// DockerStatus 处理 GET /api/v1/monitor/agents/:id/docker：Docker 状态快照。
//
// 返回 Docker 守护进程级汇总（容器数、镜像数、网络数、卷数）以及各容器的最新
// CPU、内存、网络、块设备 IO 与状态，供详情页「Docker 状态」抽屉渲染。
func (h *MonitorAPI) DockerStatus(c *gin.Context) {
	agentID := strings.TrimSpace(c.Param("id"))
	if agentID == "" || agentID == "undefined" || agentID == "null" {
		BadRequest(c, monitor.ErrInvalidAgent.Error())
		return
	}
	snapshot, err := h.monitor.DockerSnapshot(agentID)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			NotFound(c, "服务器不存在")
		case isMetricQueryError(err):
			BadRequest(c, err.Error())
		default:
			write(c, http.StatusBadGateway, "B0201", "查询 Docker 状态失败", nil)
		}
		return
	}
	OK(c, snapshot)
}

// isMetricQueryError 判断错误是否为可直接回 400 的参数类错误。
//
// 参数 Parameters:
//   - err (error): service 返回的错误。
//
// 返回 Returns:
//   - invalid (bool): 属于参数类哨兵错误时返回 true。
func isMetricQueryError(err error) bool {
	for _, sentinel := range []error{
		monitor.ErrInvalidMeasurement, monitor.ErrInvalidAgent, monitor.ErrInvalidRange,
		monitor.ErrInvalidField, monitor.ErrInvalidAggregate, monitor.ErrInvalidFilter,
	} {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
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
