package monitor

import "errors"

// 监控查询的哨兵错误：由 api 层翻译为与迁移前完全一致的响应码与提示。
var (
	// ErrInvalidMeasurement 指标类型不在白名单内（400 / A0001）。
	ErrInvalidMeasurement = errors.New("不支持的指标类型")
	// ErrInvalidAgent 服务器 ID 无效（400 / A0001）。
	ErrInvalidAgent = errors.New("服务器 ID 无效")
	// ErrInvalidRange 时间范围无效（400 / A0001）。
	ErrInvalidRange = errors.New("时间范围无效")
	// ErrInfluxUnavailable InfluxDB 查询失败（502 / B0201）。
	ErrInfluxUnavailable = errors.New("查询时序数据失败")
	// ErrMonitorDBUnavailable 监控库查询失败（502 / B0200）。
	ErrMonitorDBUnavailable = errors.New("查询监控数据失败")
)
