// Package job 提供计划任务调度与管理能力。
package job

import "time"

// Job 对应 sa_job 表：定时任务配置。
type Job struct {
	// ID 主键（自增）。
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// Name 任务名称。
	Name string `gorm:"column:job_name;size:64;not null" json:"name"`
	// Group 任务组名。
	Group string `gorm:"column:job_group;size:64;not null;default:'DEFAULT'" json:"group"`
	// InvokeTarget 调用目标字符串（如 cleanInfluxDB、cleanLoginLog 等）。
	InvokeTarget string `gorm:"column:invoke_target;size:500;not null" json:"invokeTarget"`
	// CronExpression cron 执行表达式。
	CronExpression string `gorm:"column:cron_expression;size:255" json:"cronExpression"`
	// MisfirePolicy 计划执行错误策略（1立即执行 2执行一次 3放弃执行）。
	MisfirePolicy string `gorm:"column:misfire_policy;size:20;default:'3'" json:"misfirePolicy"`
	// Concurrent 是否并发执行（0允许 1禁止）。
	Concurrent string `gorm:"column:concurrent;size:1;default:'1'" json:"concurrent"`
	// Status 状态（0正常 1停用）。
	Status string `gorm:"column:status;size:1;not null;default:'0'" json:"status"`
	// Remark 备注。
	Remark string `gorm:"column:remark;size:500" json:"remark"`
	// DelFlag 删除标志（0正常 1删除）。
	DelFlag string `gorm:"column:del_flag;size:1;not null;default:'0'" json:"delFlag"`
	// CreatedBy 创建者。
	CreatedBy string `gorm:"column:create_by;size:64;not null;default:''" json:"createdBy"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `gorm:"column:create_time" json:"createdAt"`
	// UpdatedBy 更新者。
	UpdatedBy string `gorm:"column:update_by;size:64;not null;default:''" json:"updatedBy"`
	// UpdatedAt 更新时间。
	UpdatedAt time.Time `gorm:"column:update_time" json:"updatedAt"`
}

// TableName 返回 sa_job 表名。
func (Job) TableName() string { return "sa_job" }

// JobLog 对应 sa_job_log 表：任务执行日志。
type JobLog struct {
	// ID 主键（自增）。
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// Name 任务名称。
	Name string `gorm:"column:job_name;size:64;not null" json:"name"`
	// Group 任务组名。
	Group string `gorm:"column:job_group;size:64;not null" json:"group"`
	// InvokeTarget 调用目标字符串。
	InvokeTarget string `gorm:"column:invoke_target;size:500;not null" json:"invokeTarget"`
	// JobMessage 日志信息。
	JobMessage string `gorm:"column:job_message;size:500" json:"jobMessage"`
	// Status 执行状态（0正常 1失败）。
	Status string `gorm:"column:status;size:1;not null;default:'0'" json:"status"`
	// ExceptionInfo 异常信息。
	ExceptionInfo string `gorm:"column:exception_info;size:2000" json:"exceptionInfo"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `gorm:"column:create_time" json:"createdAt"`
}

// TableName 返回 sa_job_log 表名。
func (JobLog) TableName() string { return "sa_job_log" }
