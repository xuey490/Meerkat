package v1

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/job"
)

// JobAPI 是计划任务管理的 HTTP 控制器。
type JobAPI struct {
	service *job.Service
}

// NewJobAPI 创建计划任务控制器。
func NewJobAPI(service *job.Service) *JobAPI {
	return &JobAPI{service: service}
}

// Register 注册计划任务 REST 路由。
func (h *JobAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/monitor/jobs", guard("monitor:job:list"), h.List)
	group.GET("/monitor/jobs/:id", guard("monitor:job:list"), h.Get)
	group.POST("/monitor/jobs", guard("monitor:job:create"), h.Create)
	group.PUT("/monitor/jobs/:id", guard("monitor:job:update"), h.Update)
	group.DELETE("/monitor/jobs/:id", guard("monitor:job:delete"), h.Delete)
	group.PATCH("/monitor/jobs/:id/toggle", guard("monitor:job:update"), h.Toggle)
	group.POST("/monitor/jobs/:id/run", guard("monitor:job:run"), h.Run)
}

// List 处理 GET /api/v1/monitor/jobs：分页列表。
func (h *JobAPI) List(c *gin.Context) {
	page, size := PageParams(c)
	keyword := c.Query("keyword")
	rows, total, err := h.service.ListJobs(page, size, keyword)
	if err != nil {
		Fail(c, err, "查询失败")
		return
	}
	OK(c, gin.H{"list": rows, "total": total})
}

// Get 处理 GET /api/v1/monitor/jobs/:id：单条详情。
func (h *JobAPI) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		BadRequest(c, "ID 无效")
		return
	}
	row, err := h.service.GetJob(uint(id))
	if err != nil {
		Fail(c, err, "查询失败")
		return
	}
	OK(c, row)
}

// Create 处理 POST /api/v1/monitor/jobs：新增。
func (h *JobAPI) Create(c *gin.Context) {
	var form job.Job
	if !BindJSON(c, &form) {
		return
	}
	if err := validateJob(&form); err != "" {
		BadRequest(c, err)
		return
	}
	form.CreatedBy = operatorName(c)
	if err := h.service.CreateJob(&form); err != nil {
		Fail(c, err, "创建失败")
		return
	}
	Created(c, form)
}

// Update 处理 PUT /api/v1/monitor/jobs/:id：修改。
func (h *JobAPI) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		BadRequest(c, "ID 无效")
		return
	}
	var form job.Job
	if !BindJSON(c, &form) {
		return
	}
	if err := validateJob(&form); err != "" {
		BadRequest(c, err)
		return
	}
	form.ID = uint(id)
	form.UpdatedBy = operatorName(c)
	form.UpdatedAt = time.Now()
	if err := h.service.UpdateJob(&form); err != nil {
		Fail(c, err, "更新失败")
		return
	}
	OK(c, nil)
}

// Delete 处理 DELETE /api/v1/monitor/jobs/:id：删除。
func (h *JobAPI) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		BadRequest(c, "ID 无效")
		return
	}
	if err := h.service.DeleteJob(uint(id)); err != nil {
		Fail(c, err, "删除失败")
		return
	}
	OK(c, nil)
}

// Toggle 处理 PATCH /api/v1/monitor/jobs/:id/toggle：启用/禁用。
func (h *JobAPI) Toggle(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		BadRequest(c, "ID 无效")
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !BindJSON(c, &body) {
		return
	}
	if err := h.service.ToggleJob(uint(id), body.Enabled); err != nil {
		Fail(c, err, "状态更新失败")
		return
	}
	OK(c, nil)
}

// Run 处理 POST /api/v1/monitor/jobs/:id/run：立即执行。
func (h *JobAPI) Run(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		BadRequest(c, "ID 无效")
		return
	}
	if err := h.service.RunJob(uint(id), operatorName(c)); err != nil {
		Fail(c, err, "执行失败")
		return
	}
	OK(c, gin.H{"message": "任务已触发执行"})
}

func validateJob(j *job.Job) string {
	if j.Name == "" {
		return "任务名称不能为空"
	}
	if j.InvokeTarget == "" {
		return "调用目标不能为空"
	}
	if j.CronExpression == "" {
		return "cron 表达式不能为空"
	}
	return ""
}
