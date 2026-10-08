package service

import (
	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/logic"
	"github.com/company/monitor-webserver/internal/util"
)

// LogService 提供日志查询的应用服务。
type LogService struct {
	// logs 日志领域操作。
	logs *logic.LogLogic
}

// NewLogService 创建日志应用服务。
//
// 参数 Parameters:
//   - logs (*logic.LogLogic): 日志领域操作。
//
// 返回 Returns:
//   - service (*LogService): 日志应用服务。
func NewLogService(logs *logic.LogLogic) *LogService { return &LogService{logs: logs} }

// OperPage 分页查询操作日志。
//
// 参数 Parameters:
//   - q (dto.LogQuery): 查询条件。
//
// 返回 Returns:
//   - result (dto.PageResult[dto.OperLog]): 分页结果。
//   - err (error): 查询失败时返回业务错误。
func (s *LogService) OperPage(q dto.LogQuery) (dto.PageResult[dto.OperLog], error) {
	rows, total, err := s.logs.OperPage(q)
	if err != nil {
		return dto.PageResult[dto.OperLog]{}, err
	}
	list := make([]dto.OperLog, 0, len(rows))
	for index := range rows {
		list = append(list, dto.NewOperLog(&rows[index]))
	}
	return dto.PageResult[dto.OperLog]{List: list, Total: total}, nil
}

// LoginPage 分页查询登录日志。
//
// 参数 Parameters:
//   - q (dto.LoginLogQuery): 查询条件。
//
// 返回 Returns:
//   - result (dto.PageResult[dto.LoginLog]): 分页结果。
//   - err (error): 查询失败时返回业务错误。
func (s *LogService) LoginPage(q dto.LoginLogQuery) (dto.PageResult[dto.LoginLog], error) {
	rows, total, err := s.logs.LoginPage(q)
	if err != nil {
		return dto.PageResult[dto.LoginLog]{}, err
	}
	list := make([]dto.LoginLog, 0, len(rows))
	for index := range rows {
		list = append(list, dto.NewLoginLog(&rows[index]))
	}
	return dto.PageResult[dto.LoginLog]{List: list, Total: total}, nil
}

// Trend 返回访问趋势数据。
//
// 参数 Parameters:
//   - q (dto.VisitTrendQuery): 查询条件。
//
// 返回 Returns:
//   - trend (dto.VisitTrendDetail): 趋势数据。
//   - err (error): 统计失败时返回业务错误。
func (s *LogService) Trend(q dto.VisitTrendQuery) (dto.VisitTrendDetail, error) {
	return s.logs.Trend(q.StartDate, q.EndDate)
}

// Overview 返回访问概览数据。
//
// 返回 Returns:
//   - overview (dto.VisitOverviewDetail): 概览数据。
//   - err (error): 统计失败时返回业务错误。
func (s *LogService) Overview() (dto.VisitOverviewDetail, error) {
	return s.logs.Overview()
}

// DeleteOperLogs 删除操作日志。
//
// 参数 Parameters:
//   - idsText (string): 逗号分隔的日志 ID。
//
// 返回 Returns:
//   - err (error): ID 非法或写入失败时返回业务错误。
func (s *LogService) DeleteOperLogs(idsText string) error {
	ids := util.ParseIDList(idsText)
	if len(ids) == 0 {
		return apperr.Invalid("日志 ID 无效")
	}
	return s.logs.DeleteOperLogs(ids)
}

// DeleteLoginLogs 删除登录日志。
//
// 参数 Parameters:
//   - idsText (string): 逗号分隔的日志 ID。
//
// 返回 Returns:
//   - err (error): ID 非法或写入失败时返回业务错误。
func (s *LogService) DeleteLoginLogs(idsText string) error {
	ids := util.ParseIDList(idsText)
	if len(ids) == 0 {
		return apperr.Invalid("日志 ID 无效")
	}
	return s.logs.DeleteLoginLogs(ids)
}
