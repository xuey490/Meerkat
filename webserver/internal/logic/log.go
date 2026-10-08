package logic

import (
	"math"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// LogLogic 负责 sa_system_oper_log 与 sa_system_login_log 的查询与清理。
type LogLogic struct {
	// db 权限库会话。
	db *gorm.DB
}

// NewLogLogic 创建日志领域操作。
//
// 参数 Parameters:
//   - db (*gorm.DB): 权限库会话。
//
// 返回 Returns:
//   - logic (*LogLogic): 日志领域操作实例。
func NewLogLogic(db *gorm.DB) *LogLogic { return &LogLogic{db: db} }

// OperPage 分页查询操作日志。
//
// 参数 Parameters:
//   - q (dto.LogQuery): 查询条件（关键字、时间范围、分页）。
//
// 返回 Returns:
//   - rows ([]model.SysOperLog): 当前页日志。
//   - total (int64): 满足条件的总数。
//   - err (error): 查询失败时返回业务错误。
func (l *LogLogic) OperPage(q dto.LogQuery) ([]model.SysOperLog, int64, error) {
	page, size := util.NormalizePage(q.PageNum, q.PageSize)
	db := l.db.Model(&model.SysOperLog{})
	if pattern := util.LikeKeyword(strings.TrimSpace(q.Keywords)); pattern != "" {
		db = db.Where(`(ip LIKE ? ESCAPE '\' OR username LIKE ? ESCAPE '\' OR router LIKE ? ESCAPE '\')`,
			pattern, pattern, pattern)
	}
	if start, end := util.ParseTimePair(q.CreateTime); start != nil || end != nil {
		if start != nil {
			db = db.Where("create_time >= ?", *start)
		}
		if end != nil {
			db = db.Where("create_time <= ?", dayEnd(*end))
		}
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, InternalErr("查询操作日志数量失败", err)
	}
	var rows []model.SysOperLog
	if err := db.Order("create_time desc").Order("id desc").
		Offset(util.Offset(page, size)).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, InternalErr("查询操作日志失败", err)
	}
	return rows, total, nil
}

// LoginPage 分页查询登录日志。
//
// 参数 Parameters:
//   - q (dto.LoginLogQuery): 查询条件。
//
// 返回 Returns:
//   - rows ([]model.SysLoginLog): 当前页日志。
//   - total (int64): 满足条件的总数。
//   - err (error): 查询失败时返回业务错误。
func (l *LogLogic) LoginPage(q dto.LoginLogQuery) ([]model.SysLoginLog, int64, error) {
	page, size := util.NormalizePage(q.PageNum, q.PageSize)
	db := l.db.Model(&model.SysLoginLog{})
	if pattern := util.LikeKeyword(strings.TrimSpace(q.Keywords)); pattern != "" {
		db = db.Where(`(username LIKE ? ESCAPE '\' OR ip LIKE ? ESCAPE '\')`, pattern, pattern)
	}
	if status := strings.TrimSpace(q.Status); status != "" {
		db = db.Where("status = ?", util.AtoiSafe(status, 1))
	}
	if start, end := util.ParseTimePair(q.CreateTime); start != nil || end != nil {
		if start != nil {
			db = db.Where("login_time >= ?", *start)
		}
		if end != nil {
			db = db.Where("login_time <= ?", dayEnd(*end))
		}
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, InternalErr("查询登录日志数量失败", err)
	}
	var rows []model.SysLoginLog
	if err := db.Order("login_time desc").Order("id desc").
		Offset(util.Offset(page, size)).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, InternalErr("查询登录日志失败", err)
	}
	return rows, total, nil
}

// trendPoint 是访问趋势的聚合中间结构。
type trendPoint struct {
	Day string
	PV  int64
	UV  int64
}

// Trend 按天统计登录量与独立访客数。
//
// 参数 Parameters:
//   - startText (string): 开始日期（YYYY-MM-DD），为空时取近 30 天。
//   - endText (string): 结束日期（YYYY-MM-DD），为空时取当天。
//
// 返回 Returns:
//   - trend (dto.VisitTrendDetail): 日期、PV、UV 序列。
//   - err (error): 统计失败时返回业务错误。
func (l *LogLogic) Trend(startText, endText string) (dto.VisitTrendDetail, error) {
	start := util.ParseQueryTime(startText)
	end := util.ParseQueryTime(endText)
	if start == nil {
		value := time.Now().AddDate(0, 0, -29).Truncate(24 * time.Hour)
		start = &value
	}
	if end == nil {
		value := time.Now()
		end = &value
	}
	startAt := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.Local)
	endAt := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, 1)
	var points []trendPoint
	err := l.db.Model(&model.SysLoginLog{}).
		Select("date(login_time) AS day, COUNT(*) AS pv, COUNT(DISTINCT username) AS uv").
		Where("login_time >= ? AND login_time < ?", startAt, endAt).
		Group("day").Order("day").Scan(&points).Error
	if err != nil {
		return dto.VisitTrendDetail{}, InternalErr("统计访问趋势失败", err)
	}
	indexed := make(map[string]trendPoint, len(points))
	for _, point := range points {
		indexed[point.Day] = point
	}
	trend := dto.VisitTrendDetail{
		Dates:  make([]string, 0, 32),
		PVList: make([]int64, 0, 32),
		UVList: make([]int64, 0, 32),
	}
	for cursor := startAt; cursor.Before(endAt); cursor = cursor.AddDate(0, 0, 1) {
		key := cursor.Format("2006-01-02")
		trend.Dates = append(trend.Dates, key)
		point := indexed[key]
		trend.PVList = append(trend.PVList, point.PV)
		trend.UVList = append(trend.UVList, point.UV)
	}
	return trend, nil
}

// Overview 统计今日与累计的登录访问概览（PV/UV 及环比）。
//
// 返回 Returns:
//   - overview (dto.VisitOverviewDetail): 访问概览。
//   - err (error): 统计失败时返回业务错误。
func (l *LogLogic) Overview() (dto.VisitOverviewDetail, error) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	yesterday := today.AddDate(0, 0, -1)

	var todayPoint, yesterdayPoint, totalPoint trendPoint
	counts := []struct {
		Target *trendPoint
		From   *time.Time
		To     *time.Time
	}{
		{&todayPoint, &today, nil},
		{&yesterdayPoint, &yesterday, &today},
		{&totalPoint, nil, nil},
	}
	for _, item := range counts {
		db := l.db.Model(&model.SysLoginLog{}).Select("COUNT(*) AS pv, COUNT(DISTINCT username) AS uv")
		if item.From != nil {
			db = db.Where("login_time >= ?", *item.From)
		}
		if item.To != nil {
			db = db.Where("login_time < ?", *item.To)
		}
		if err := db.Scan(item.Target).Error; err != nil {
			return dto.VisitOverviewDetail{}, InternalErr("统计访问概览失败", err)
		}
	}
	return dto.VisitOverviewDetail{
		TodayUVCount: todayPoint.UV,
		TotalUVCount: totalPoint.UV,
		UVGrowthRate: GrowthRate(todayPoint.UV, yesterdayPoint.UV),
		TodayPVCount: todayPoint.PV,
		TotalPVCount: totalPoint.PV,
		PVGrowthRate: GrowthRate(todayPoint.PV, yesterdayPoint.PV),
	}, nil
}

// DeleteOperLogs 删除指定的操作日志。
//
// 参数 Parameters:
//   - ids ([]int64): 日志主键列表。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *LogLogic) DeleteOperLogs(ids []int64) error {
	if err := l.db.Where("id IN ?", ids).Delete(&model.SysOperLog{}).Error; err != nil {
		return InternalErr("删除操作日志失败", err)
	}
	return nil
}

// DeleteLoginLogs 删除指定的登录日志。
//
// 参数 Parameters:
//   - ids ([]int64): 日志主键列表。
//
// 返回 Returns:
//   - err (error): 写入失败时返回业务错误。
func (l *LogLogic) DeleteLoginLogs(ids []int64) error {
	if err := l.db.Where("id IN ?", ids).Delete(&model.SysLoginLog{}).Error; err != nil {
		return InternalErr("删除登录日志失败", err)
	}
	return nil
}

// GrowthRate 计算环比增长率（百分比，保留两位小数）。
//
// 参数 Parameters:
//   - current (int64): 当前周期值。
//   - previous (int64): 上一周期值。
//
// 返回 Returns:
//   - rate (float64): 增长率；上期为 0 时：本期为 0 返回 0，否则返回 100。
func GrowthRate(current, previous int64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100
	}
	rate := float64(current-previous) / float64(previous) * 100
	// 保留两位小数；使用 math.Round 避免二进制浮点误差导致的 -49.99 这类结果。
	return math.Round(rate*100) / 100
}

// dayEnd 把日期归一到当天 23:59:59，用于「截止日期」包含当天的语义。
//
// 参数 Parameters:
//   - value (time.Time): 用户选择的截止日期。
//
// 返回 Returns:
//   - at (time.Time): 当日最后一秒。
func dayEnd(value time.Time) time.Time {
	return value.Add(24 * time.Hour).Add(-time.Second)
}
