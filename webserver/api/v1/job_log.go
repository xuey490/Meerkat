package v1

import (
	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/internal/job"
	"github.com/company/monitor-webserver/internal/util"
)

// JobLogAPI 是计划任务执行日志的 HTTP 控制器。
type JobLogAPI struct {
	service *job.Service
}

// NewJobLogAPI 创建任务日志控制器。
func NewJobLogAPI(service *job.Service) *JobLogAPI {
	return &JobLogAPI{service: service}
}

// Register 注册任务日志 REST 路由。
func (h *JobLogAPI) Register(group *gin.RouterGroup, guard Guard) {
	group.GET("/monitor/job-logs", guard("monitor:job-log:list"), h.List)
	group.DELETE("/monitor/job-logs/clean", guard("monitor:job-log:delete"), h.Clean)
	// 按 ID 批量清理：静态路径 /clean 优先匹配，不会与 :ids 冲突（gin 的静态节点优先于参数节点）。
	group.DELETE("/monitor/job-logs/:ids", guard("monitor:job-log:delete"), h.Delete)
}

// Delete 处理 DELETE /api/v1/monitor/job-logs/:ids：批量清理选中的任务执行日志。
//
// 与「清空日志」共用 monitor:job-log:delete：同一模块的同类操作不必再拆一个权限标识。
func (h *JobLogAPI) Delete(c *gin.Context) {
	ids := util.ParseIDList(c.Param("ids"))
	if len(ids) == 0 {
		BadRequest(c, "日志 ID 无效")
		return
	}
	removed, err := h.service.DeleteJobLogs(ids)
	if err != nil {
		Fail(c, err, "清理失败")
		return
	}
	OK(c, gin.H{"removed": removed})
}

// List 处理 GET /api/v1/monitor/job-logs：分页列表。
func (h *JobLogAPI) List(c *gin.Context) {
	page, size := PageParams(c)
	jobName := c.Query("jobName")
	rows, total, err := h.service.ListJobLogs(page, size, jobName)
	if err != nil {
		Fail(c, err, "查询失败")
		return
	}
	OK(c, gin.H{"list": rows, "total": total})
}

// Clean 处理 DELETE /api/v1/monitor/job-logs/clean：清空日志。
func (h *JobLogAPI) Clean(c *gin.Context) {
	if err := h.service.CleanJobLogs(); err != nil {
		Fail(c, err, "清空失败")
		return
	}
	OK(c, nil)
}
