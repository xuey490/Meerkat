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
	// ErrInvalidField 指标字段不在白名单内（400 / A0001）。
	ErrInvalidField = errors.New("不支持的指标字段")
	// ErrInvalidAggregate 聚合方式或聚合窗口非法（400 / A0001）。
	ErrInvalidAggregate = errors.New("不支持的聚合方式")
	// ErrInvalidFilter 列表过滤条件取值非法（400 / A0001）。
	ErrInvalidFilter = errors.New("查询过滤条件无效")
	// ErrInfluxUnavailable InfluxDB 查询失败（502 / B0201）。
	ErrInfluxUnavailable = errors.New("查询时序数据失败")
	// ErrMonitorDBUnavailable 监控库查询失败（502 / B0200）。
	ErrMonitorDBUnavailable = errors.New("查询监控数据失败")
	// ErrInfluxDeleteFailed 时序库删除失败（502 / B0201）。
	//
	// 与 ErrInfluxUnavailable 分开：清理与查询是两类操作，提示语必须能区分
	// 「查不到数据」和「没删干净」——后者若报成前者，运维会去查采集链路而不是重试清理。
	ErrInfluxDeleteFailed = errors.New("清理时序库数据失败")
	// ErrMonitorDBDeleteFailed 监控库删除采集回执失败（502 / B0200）。
	ErrMonitorDBDeleteFailed = errors.New("清理采集回执失败")
)
