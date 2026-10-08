package dto

import (
	"fmt"
	"strings"

	"github.com/company/monitor-webserver/internal/model"
)

// OperLog 对齐前端 LogItem（操作日志列表与详情共用）。
type OperLog struct {
	// ID 日志 ID。
	ID int64 `json:"id"`
	// Module 模块名。
	Module string `json:"module"`
	// ActionType 操作类型。
	ActionType string `json:"actionType"`
	// Title 操作标题。
	Title string `json:"title"`
	// Content 请求内容（已脱敏）。
	Content string `json:"content"`
	// OperatorID 操作人 ID。
	OperatorID int64 `json:"operatorId"`
	// OperatorName 操作人账号。
	OperatorName string `json:"operatorName"`
	// RequestURI 请求路径。
	RequestURI string `json:"requestUri"`
	// RequestMethod 请求方法。
	RequestMethod string `json:"requestMethod"`
	// IP 请求 IP。
	IP string `json:"ip"`
	// Region IP 归属地。
	Region string `json:"region"`
	// Device 设备类型。
	Device string `json:"device"`
	// Browser 浏览器。
	Browser string `json:"browser"`
	// OS 操作系统。
	OS string `json:"os"`
	// Status 状态：1 成功 / 0 失败。
	Status int `json:"status"`
	// ExecutionTime 执行时间（毫秒）。
	ExecutionTime float64 `json:"executionTime"`
	// ErrorMsg 错误信息。
	ErrorMsg string `json:"errorMsg"`
	// CreateTime 操作时间。
	CreateTime string `json:"createTime"`
}

// LoginLog 是登录日志列表项（前端登录日志页使用）。
type LoginLog struct {
	// ID 日志 ID。
	ID int64 `json:"id"`
	// Username 用户名。
	Username string `json:"username"`
	// IP 登录 IP。
	IP string `json:"ip"`
	// IPLocation IP 归属地。
	IPLocation string `json:"ipLocation"`
	// OS 操作系统。
	OS string `json:"os"`
	// Browser 浏览器。
	Browser string `json:"browser"`
	// Status 登录状态：1 成功 / 2 失败。
	Status int `json:"status"`
	// Message 提示消息。
	Message string `json:"message"`
	// LoginTime 登录时间。
	LoginTime string `json:"loginTime"`
}

// LogQuery 是操作日志分页查询参数。
type LogQuery struct {
	// PageQuery 分页参数。
	PageQuery
	// Keywords 关键字（IP/操作人）。
	Keywords string `json:"keywords" form:"keywords"`
	// CreateTime 操作时间范围（开始,结束）。
	CreateTime []string `json:"createTime" form:"createTime"`
}

// LoginLogQuery 是登录日志分页查询参数。
type LoginLogQuery struct {
	// PageQuery 分页参数。
	PageQuery
	// Keywords 关键字（用户名/IP）。
	Keywords string `json:"keywords" form:"keywords"`
	// Status 登录状态过滤。
	Status string `json:"status" form:"status"`
	// CreateTime 登录时间范围（开始,结束）。
	CreateTime []string `json:"createTime" form:"createTime"`
}

// VisitTrendQuery 是访问趋势查询参数。
type VisitTrendQuery struct {
	// StartDate 开始日期（YYYY-MM-DD）。
	StartDate string `json:"startDate" form:"startDate"`
	// EndDate 结束日期（YYYY-MM-DD）。
	EndDate string `json:"endDate" form:"endDate"`
}

// VisitTrendDetail 对齐前端 VisitTrendDetail。
type VisitTrendDetail struct {
	// Dates 日期列表。
	Dates []string `json:"dates"`
	// PVList 浏览量列表。
	PVList []int64 `json:"pvList"`
	// UVList 访客数列表。
	UVList []int64 `json:"uvList"`
}

// VisitOverviewDetail 对齐前端 VisitOverviewDetail。
type VisitOverviewDetail struct {
	// TodayUVCount 今日独立访客数。
	TodayUVCount int64 `json:"todayUvCount"`
	// TotalUVCount 累计独立访客数。
	TotalUVCount int64 `json:"totalUvCount"`
	// UVGrowthRate 独立访客增长率（百分比）。
	UVGrowthRate float64 `json:"uvGrowthRate"`
	// TodayPVCount 今日页面浏览量。
	TodayPVCount int64 `json:"todayPvCount"`
	// TotalPVCount 累计页面浏览量。
	TotalPVCount int64 `json:"totalPvCount"`
	// PVGrowthRate 页面浏览量增长率（百分比）。
	PVGrowthRate float64 `json:"pvGrowthRate"`
}

// NewOperLog 把操作日志实体转换为前端列表项。
//
// 参数 Parameters:
//   - row (*model.SysOperLog): 操作日志实体。
//
// 返回 Returns:
//   - item (OperLog): 前端操作日志项。
func NewOperLog(row *model.SysOperLog) OperLog {
	createTime := timeText(row.CreateTime)
	if createTime == "" {
		createTime = timeText(row.UpdateTime)
	}
	return OperLog{
		ID:            row.ID,
		Module:        textValue(row.App),
		ActionType:    textValue(row.ActionType),
		Title:         textValue(row.ServiceName),
		Content:       textValue(row.RequestData),
		OperatorID:    int64Of(row.OperatorID),
		OperatorName:  textValue(row.Username),
		RequestURI:    textValue(row.Router),
		RequestMethod: textValue(row.Method),
		IP:            textValue(row.IP),
		Region:        textValue(row.IPLocation),
		Device:        textValue(row.Device),
		Browser:       textValue(row.Browser),
		OS:            textValue(row.OS),
		Status:        intOf(row.Status),
		ExecutionTime: ParseDuration(textValue(row.Duration)),
		ErrorMsg:      textValue(row.ErrorMsg),
		CreateTime:    createTime,
	}
}

// NewLoginLog 把登录日志实体转换为前端列表项。
//
// 参数 Parameters:
//   - row (*model.SysLoginLog): 登录日志实体。
//
// 返回 Returns:
//   - item (LoginLog): 前端登录日志项。
func NewLoginLog(row *model.SysLoginLog) LoginLog {
	loginTime := timeText(row.LoginTime)
	if loginTime == "" {
		loginTime = timeText(row.CreateTime)
	}
	return LoginLog{
		ID:         row.ID,
		Username:   textValue(row.Username),
		IP:         textValue(row.IP),
		IPLocation: textValue(row.IPLocation),
		OS:         textValue(row.OS),
		Browser:    textValue(row.Browser),
		Status:     row.Status,
		Message:    textValue(row.Message),
		LoginTime:  loginTime,
	}
}

// ParseDuration 解析操作日志中保存的耗时（毫秒文本）。
//
// 参数 Parameters:
//   - raw (string): 形如 "12.34" 的毫秒文本。
//
// 返回 Returns:
//   - milliseconds (float64): 解析结果；无法解析时返回 0。
func ParseDuration(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	var value float64
	if _, err := fmt.Sscanf(raw, "%f", &value); err != nil {
		return 0
	}
	return value
}

// GrowthRate 计算增长率百分比，基准为 0 时按「有增长即 100%」处理。
//
// 参数 Parameters:
//   - current (int64): 当前值。
//   - base (int64): 基准值。
//
// 返回 Returns:
//   - rate (float64): 增长率百分比，保留整数。
func GrowthRate(current, base int64) float64 {
	if base <= 0 {
		if current <= 0 {
			return 0
		}
		return 100
	}
	return float64(current-base) * 100 / float64(base)
}
