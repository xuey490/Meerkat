// Package v1 是 HTTP 控制器层：负责参数绑定、调用 service、写出统一响应。
//
// 约定：
//   - 控制器不直接访问数据库，也不写业务规则，全部下推到 internal/service 与 internal/logic；
//   - 响应壳固定为 {"code","msg","data"}，分页数据固定为 {"list","total"}；
//   - HTTP 状态码与业务错误码的映射只在本文件出现一次（见 Fail）。
package v1

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/company/monitor-webserver/api/middleware"
	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// 业务响应码：与前端 src/enums/api.ts 的 ApiCodeEnum 对齐。
const (
	// CodeSuccess 成功。
	CodeSuccess = "00000"
	// CodeTokenInvalid 访问令牌失效。
	CodeTokenInvalid = "A0230"
	// CodeDenied 权限不足。
	CodeDenied = "A0301"
)

// Guard 按权限标识生成鉴权中间件，由 api/router 注入具体实现。
type Guard func(perm string) gin.HandlerFunc

// write 写出统一响应壳。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//   - status (int): HTTP 状态码。
//   - code (string): 业务响应码。
//   - message (string): 提示信息。
//   - data (any): 业务数据。
func write(c *gin.Context, status int, code, message string, data any) {
	// 业务码与提示文案写入上下文，供操作日志中间件采集失败原因。
	c.Set(middleware.ContextCodeKey, code)
	c.Set(middleware.ContextResponseKey, message)
	c.JSON(status, gin.H{"code": code, "msg": message, "data": data})
}

// OK 写出成功响应（200 / 00000）。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//   - data (any): 业务数据。
func OK(c *gin.Context, data any) {
	write(c, http.StatusOK, CodeSuccess, "操作成功", data)
}

// Created 写出创建成功响应（201 / 00000）。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//   - data (any): 业务数据。
func Created(c *gin.Context, data any) {
	write(c, http.StatusCreated, CodeSuccess, "创建成功", data)
}

// BadRequest 写出参数错误响应（400 / A0001）。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//   - message (string): 提示信息。
func BadRequest(c *gin.Context, message string) {
	write(c, http.StatusBadRequest, "A0001", message, nil)
}

// NotFound 写出资源不存在响应（404 / A0404）。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//   - message (string): 提示信息。
func NotFound(c *gin.Context, message string) {
	write(c, http.StatusNotFound, "A0404", message, nil)
}

// Fail 把 service 返回的错误映射为响应。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//   - err (error): service 返回的错误，应为 apperr 类型的业务错误。
//   - fallback (string): 非业务错误时的兜底提示。
func Fail(c *gin.Context, err error, fallback string) {
	switch apperr.KindOf(err) {
	case apperr.KindInvalid:
		write(c, http.StatusBadRequest, "A0001", apperr.MessageOf(err, fallback), nil)
	case apperr.KindNotFound:
		write(c, http.StatusNotFound, "A0404", apperr.MessageOf(err, fallback), nil)
	case apperr.KindConflict:
		write(c, http.StatusConflict, "A0002", apperr.MessageOf(err, fallback), nil)
	case apperr.KindForbidden:
		write(c, http.StatusForbidden, CodeDenied, apperr.MessageOf(err, fallback), nil)
	case apperr.KindUnauthorized:
		write(c, http.StatusUnauthorized, CodeTokenInvalid, apperr.MessageOf(err, fallback), nil)
	case apperr.KindUnavailable:
		write(c, http.StatusBadGateway, "B0201", apperr.MessageOf(err, fallback), nil)
	default:
		write(c, http.StatusInternalServerError, "B0001", fallback, nil)
	}
}

// BindJSON 绑定请求体，失败时直接写出 400 并返回 false。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//   - target (any): 绑定目标。
//
// 返回 Returns:
//   - ok (bool): 绑定是否成功。
func BindJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		BadRequest(c, "请求参数格式错误")
		return false
	}
	return true
}

// PageParams 解析分页参数 pageNum/pageSize 并做上下限收敛。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//
// 返回 Returns:
//   - page (int): 页码，最小 1。
//   - size (int): 每页条数，范围 [1, 100]。
func PageParams(c *gin.Context) (page, size int) {
	return util.NormalizePage(
		util.AtoiSafe(c.DefaultQuery("pageNum", "1"), 1),
		util.AtoiSafe(c.DefaultQuery("pageSize", ""), util.DefaultPageSize),
	)
}

// TimeRange 解析时间范围查询参数（兼容 repeat 与逗号两种前端序列化方式）。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//   - field (string): 查询字段名，如 "createTime"。
//
// 返回 Returns:
//   - start (*time.Time): 起始时间，未提供时为 nil。
//   - end (*time.Time): 结束时间，未提供时为 nil。
func TimeRange(c *gin.Context, field string) (start, end *time.Time) {
	return util.ParseTimePair(c.QueryArray(field))
}

// ClientIP 提取客户端 IP，兼容反向代理场景。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//
// 返回 Returns:
//   - ip (string): 客户端 IP。
func ClientIP(c *gin.Context) string {
	if forwarded := c.GetHeader("X-Forwarded-For"); forwarded != "" {
		if index := strings.IndexByte(forwarded, ','); index > 0 {
			return strings.TrimSpace(forwarded[:index])
		}
		return strings.TrimSpace(forwarded)
	}
	if real := c.GetHeader("X-Real-IP"); real != "" {
		return strings.TrimSpace(real)
	}
	return c.ClientIP()
}

// CurrentUser 读取鉴权中间件写入的当前用户。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//
// 返回 Returns:
//   - user (*model.SysUser): 当前用户；缺失时返回 nil。
func CurrentUser(c *gin.Context) *model.SysUser {
	value, exists := c.Get(util.ContextUserKey)
	if !exists {
		return nil
	}
	user, ok := value.(*model.SysUser)
	if !ok {
		return nil
	}
	return user
}

// CurrentSessionID 读取鉴权中间件写入的当前会话编号（JWT 载荷 sid）。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//
// 返回 Returns:
//   - sid (string): 会话编号；缺失时返回空串。
func CurrentSessionID(c *gin.Context) string {
	value, exists := c.Get(util.ContextSessionKey)
	if !exists {
		return ""
	}
	sid, _ := value.(string)
	return sid
}

// PathID 解析路径上的单值主键。
//
// 参数 Parameters:
//   - c (*gin.Context): 当前请求上下文。
//   - name (string): 路径参数名，如 "id"。
//
// 返回 Returns:
//   - id (int64): 解析结果；非法时返回 0。
func PathID(c *gin.Context, name string) int64 {
	ids := util.ParseIDList(c.Param(name))
	if len(ids) == 0 {
		return 0
	}
	return ids[0]
}
