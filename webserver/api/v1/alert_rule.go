package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/resourcealert"
	"github.com/company/monitor-webserver/internal/util"
)

// AlertRuleAPI 是资源告警规则的 HTTP 控制器。
//
// 规则按「全局 → 角色 → 单机」三层存储：全局与角色两级在「监控中心 → 告警规则」页维护，
// 单机覆盖在「服务器列表 → 告警设置」弹窗维护；生效优先级为 单机 > 角色 > 全局。
type AlertRuleAPI struct {
	// alerts 资源告警服务。
	alerts *resourcealert.Service
}

// NewAlertRuleAPI 创建资源告警规则控制器。
//
// 参数 Parameters:
//   - alerts (*resourcealert.Service): 资源告警服务。
//
// 返回 Returns:
//   - api (*AlertRuleAPI): 规则控制器。
func NewAlertRuleAPI(alerts *resourcealert.Service) *AlertRuleAPI {
	return &AlertRuleAPI{alerts: alerts}
}

// Register 注册资源告警规则相关路由。
//
// 与监控查询路由（不校验权限标识）不同，这里的写接口会真正做权限校验：阈值直接决定
// 何时产生告警，不能只靠前端隐藏按钮。
//
// 参数 Parameters:
//   - group (*gin.RouterGroup): 已挂载登录鉴权中间件的路由组。
//   - guard (Guard): 按权限标识生成中间件。
func (h *AlertRuleAPI) Register(group *gin.RouterGroup, guard Guard) {
	read := guard("monitor:alert:rule:list")
	write := guard("monitor:alert:rule:update")
	group.GET("/monitor/alert-rules/metrics", read, h.Metrics)
	group.GET("/monitor/alert-rules", read, h.List)
	group.PUT("/monitor/alert-rules", write, h.Save)
	group.DELETE("/monitor/alert-rules/:id", write, h.Delete)
	group.GET("/monitor/agents/:id/alert-rules", read, h.AgentRules)
	group.PUT("/monitor/agents/:id/alert-rules", write, h.SaveAgentRules)
	// 告警详情与确认：告警中心详情弹窗用（列表本身沿用 MonitorAPI 的 /monitor/alerts）。
	group.GET("/monitor/alerts/:id", read, h.Detail)
	group.POST("/monitor/alerts/:id/ack", write, h.Acknowledge)
	// 批量清理告警记录：不可逆，用独立权限标识而非复用确认权限。
	group.DELETE("/monitor/alerts/:ids", guard("monitor:alert:delete"), h.DeleteAlerts)
}

// DeleteAlerts 处理 DELETE /api/v1/monitor/alerts/:ids：批量清理告警记录。
//
// 路径参数为逗号分隔的告警 ID（与系统模块的批量删除契约一致）。
func (h *AlertRuleAPI) DeleteAlerts(c *gin.Context) {
	ids := util.ParseIDList(c.Param("ids"))
	if len(ids) == 0 {
		BadRequest(c, "告警 ID 无效")
		return
	}
	removed, err := h.alerts.DeleteAlerts(ids)
	if err != nil {
		Fail(c, err, "清理告警失败")
		return
	}
	OK(c, gin.H{"removed": removed})
}

// Detail 处理 GET /api/v1/monitor/alerts/:id：告警详情。
//
// 返回归因快照（details_json）与状态流转时间线，供告警中心详情弹窗展示「为什么告警、
// 谁占的资源、之后如何变化」。
func (h *AlertRuleAPI) Detail(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		BadRequest(c, "告警 ID 无效")
		return
	}
	detail, err := h.alerts.Store().AlertDetail(uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(c, "告警不存在")
			return
		}
		Fail(c, err, "查询告警详情失败")
		return
	}
	OK(c, detail)
}

// Acknowledge 处理 POST /api/v1/monitor/alerts/:id/ack：人工确认告警。
//
// 走 Service 而不是直接调 Store：确认除了关掉这条记录，还要把状态机里对应的告警周期
// 一并结束，否则指标未恢复时新告警会沿用旧周期的首次触发时间。
func (h *AlertRuleAPI) Acknowledge(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		BadRequest(c, "告警 ID 无效")
		return
	}
	if _, err := h.alerts.Acknowledge(uint(id)); err != nil {
		Fail(c, err, "确认告警失败")
		return
	}
	OK(c, nil)
}

// rulePayload 是保存单条规则的请求体。
type rulePayload struct {
	// MetricKey 指标标识。
	MetricKey string `json:"metricKey"`
	// WarningThreshold 警告阈值。
	WarningThreshold float64 `json:"warningThreshold"`
	// CriticalThreshold 严重阈值。
	CriticalThreshold float64 `json:"criticalThreshold"`
	// ForSeconds 判定期（秒）。
	ForSeconds int64 `json:"forSeconds"`
	// BreachRatio 窗口内超标点占比下限（0.5~1）。
	BreachRatio float64 `json:"breachRatio"`
	// RemindIntervalSeconds 警告级同级重申间隔（秒），0 表示不重申。
	RemindIntervalSeconds int64 `json:"remindIntervalSeconds"`
	// RemindIntervalCriticalSeconds 严重级同级重申间隔（秒），0 表示不重申。
	RemindIntervalCriticalSeconds int64 `json:"remindIntervalCriticalSeconds"`
	// CooldownSeconds 恢复后冷却时长（秒）。
	CooldownSeconds int64 `json:"cooldownSeconds"`
	// ParamsJSON 扩展参数（JSON 文本，磁盘规则用于忽略文件系统与指定挂载点）。
	ParamsJSON string `json:"paramsJson"`
	// Enabled 是否启用。
	Enabled bool `json:"enabled"`
}

// saveRulesRequest 是保存规则集的请求体。
type saveRulesRequest struct {
	// Scope 作用域：global / role。单机级由路径决定，不接受该字段。
	Scope string `json:"scope"`
	// Target 作用域目标：角色名为角色级，空为全局级。
	Target string `json:"target"`
	// Rules 规则列表；传空数组表示清空该层级配置（回落到上一级）。
	Rules []rulePayload `json:"rules"`
}

// Metrics 处理 GET /api/v1/monitor/alert-rules/metrics：返回指标口径定义。
//
// 前端据此渲染规则表单（名称、单位、是否按挂载点、口径说明），避免口径写死在前端。
func (h *AlertRuleAPI) Metrics(c *gin.Context) {
	OK(c, resourcealert.Metrics())
}

// List 处理 GET /api/v1/monitor/alert-rules：按作用域查询规则。
func (h *AlertRuleAPI) List(c *gin.Context) {
	scope := strings.TrimSpace(c.Query("scope"))
	target := strings.TrimSpace(c.Query("target"))
	rules, err := h.alerts.Store().ListRules(scope, target)
	if err != nil {
		Fail(c, err, "查询告警规则失败")
		return
	}
	OK(c, rules)
}

// Save 处理 PUT /api/v1/monitor/alert-rules：覆盖式保存某一层级的规则。
//
// 请求体为规则集；同一层级传入空数组即清空该层级（该指标回落到上一级）。
func (h *AlertRuleAPI) Save(c *gin.Context) {
	var req saveRulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "请求参数格式错误")
		return
	}
	scope := strings.TrimSpace(req.Scope)
	if scope != resourcealert.ScopeGlobal && scope != resourcealert.ScopeRole {
		BadRequest(c, "作用域只能是 global 或 role")
		return
	}
	target := strings.TrimSpace(req.Target)
	if scope == resourcealert.ScopeRole && target == "" {
		BadRequest(c, "角色级规则必须指定角色")
		return
	}
	if err := h.saveRules(scope, target, req.Rules); err != nil {
		Fail(c, err, "保存告警规则失败")
		return
	}
	OK(c, nil)
}

// Delete 处理 DELETE /api/v1/monitor/alert-rules/:id：删除单条规则。
func (h *AlertRuleAPI) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		BadRequest(c, "规则 ID 无效")
		return
	}
	if err := h.alerts.Store().DeleteRule(uint(id)); err != nil {
		Fail(c, err, "删除告警规则失败")
		return
	}
	OK(c, nil)
}

// AgentRules 处理 GET /api/v1/monitor/agents/:id/alert-rules：单机规则视图。
//
// 返回每个指标的「生效规则 + 来源层级 + 单机覆盖值」，供弹窗展示继承关系。
func (h *AlertRuleAPI) AgentRules(c *gin.Context) {
	view, err := h.alerts.Store().AgentRules(c.Param("id"))
	if err != nil {
		Fail(c, err, "查询服务器告警设置失败")
		return
	}
	OK(c, view)
}

// SaveAgentRules 处理 PUT /api/v1/monitor/agents/:id/alert-rules：保存单机覆盖规则。
//
// 请求体为规则集；传空数组表示清除该主机的覆盖，恢复继承角色/全局配置。
func (h *AlertRuleAPI) SaveAgentRules(c *gin.Context) {
	var req saveRulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "请求参数格式错误")
		return
	}
	if err := h.saveRules(resourcealert.ScopeAgent, c.Param("id"), req.Rules); err != nil {
		Fail(c, err, "保存服务器告警设置失败")
		return
	}
	OK(c, nil)
}

// saveRules 校验并覆盖式保存某个层级的规则。
//
// 参数 Parameters:
//   - scope (string): 作用域。
//   - target (string): 作用域目标。
//   - payloads ([]rulePayload): 规则集。
//
// 返回 Returns:
//   - err (error): 校验或写入失败时返回非 nil。
func (h *AlertRuleAPI) saveRules(scope, target string, payloads []rulePayload) error {
	rules := make([]resourcealert.AlertRule, 0, len(payloads))
	seen := make(map[string]bool, len(payloads))
	for _, payload := range payloads {
		if err := validateRulePayload(payload); err != nil {
			return err
		}
		// 同一层级同一指标只允许一条，避免解析时出现不确定的覆盖顺序。
		if seen[payload.MetricKey] {
			return apperr.Invalid(fmt.Sprintf("指标 %s 重复配置", payload.MetricKey))
		}
		seen[payload.MetricKey] = true
		rules = append(rules, resourcealert.AlertRule{
			MetricKey:                     payload.MetricKey,
			WarningThreshold:              payload.WarningThreshold,
			CriticalThreshold:             payload.CriticalThreshold,
			ForSeconds:                    payload.ForSeconds,
			BreachRatio:                   payload.BreachRatio,
			RemindIntervalSeconds:         payload.RemindIntervalSeconds,
			RemindIntervalCriticalSeconds: payload.RemindIntervalCriticalSeconds,
			CooldownSeconds:               payload.CooldownSeconds,
			ParamsJSON:                    payload.ParamsJSON,
			Enabled:                       payload.Enabled,
		})
	}
	_, err := h.alerts.Store().SaveScopeRules(scope, target, rules)
	return err
}

// validateRulePayload 校验单条规则的合法性。
//
// 这些约束是「防误配」的最后一道闸门：阈值区间错误会让告警永远不触发或一直触发，
// 判定期/占比越界会让窗口判定失去意义。
//
// 错误统一用 apperr.Invalid 包装：它会被映射为 HTTP 400 并把原因原样返回前端，
// 而不是落进 500 的兜底文案——用户需要知道到底哪一项配错了。
//
// 参数 Parameters:
//   - payload (rulePayload): 待校验规则。
//
// 返回 Returns:
//   - err (error): 非法时返回带具体原因的业务错误。
func validateRulePayload(payload rulePayload) error {
	spec, ok := resourcealert.MetricSpecOf(payload.MetricKey)
	if !ok {
		return apperr.Invalid(fmt.Sprintf("未知指标：%s", payload.MetricKey))
	}
	if payload.WarningThreshold <= 0 || payload.CriticalThreshold <= 0 {
		return apperr.Invalid(fmt.Sprintf("%s的阈值必须大于 0", spec.Name))
	}
	if payload.CriticalThreshold < payload.WarningThreshold {
		return apperr.Invalid(fmt.Sprintf("%s的严重阈值不能低于警告阈值", spec.Name))
	}
	if payload.CriticalThreshold > 100 {
		return apperr.Invalid(fmt.Sprintf("%s的阈值不应大于 100", spec.Name))
	}
	if payload.ForSeconds <= 0 || payload.ForSeconds > 86400 {
		return apperr.Invalid(fmt.Sprintf("%s的判定期需在 1 秒 ~ 24 小时之间", spec.Name))
	}
	if payload.BreachRatio < 0.5 || payload.BreachRatio > 1 {
		return apperr.Invalid(fmt.Sprintf("%s的超标占比需在 0.5 ~ 1 之间", spec.Name))
	}
	if payload.RemindIntervalSeconds < 0 || payload.RemindIntervalCriticalSeconds < 0 || payload.CooldownSeconds < 0 {
		return apperr.Invalid(fmt.Sprintf("%s的重申间隔与冷却时长不能为负数", spec.Name))
	}
	if raw := strings.TrimSpace(payload.ParamsJSON); raw != "" && !json.Valid([]byte(raw)) {
		return apperr.Invalid(fmt.Sprintf("%s的扩展参数不是合法 JSON", spec.Name))
	}
	return nil
}
