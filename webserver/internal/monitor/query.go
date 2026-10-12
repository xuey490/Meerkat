package monitor

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 指标查询的默认与边界值：既给前端留出降采样空间，又避免一次拉回不可控的数据量。
const (
	// defaultMetricLimit 单次查询每条序列的默认返回点数（与迁移前 limit 5000 保持一致）。
	defaultMetricLimit = 5000
	// maxMetricLimit 单次查询允许的最大点数。
	maxMetricLimit = 20000
	// maxMetricFields 单次查询允许指定的字段数上限。
	maxMetricFields = 16
	// maxMetricWindowSeconds 聚合窗口上限（1 天）。
	maxMetricWindowSeconds = 86400
)

// aggregateRate 是「计数器转速率」的聚合方式：先取窗口末值，再求每秒增量，最后按窗口取均值。
//
// 它不是 Flux 自带的 fn，需要单独拼一段 derivative 才能得到 bytes/s、ops/s，
// 因此与 mean/max 这类直接透传给 aggregateWindow 的聚合区分开。
const aggregateRate = "rate"

// allowedAggregates 是允许的聚合方式白名单（除 aggregateRate 外均可直接作为 Flux fn）。
var allowedAggregates = map[string]bool{
	"mean": true, "median": true, "max": true, "min": true, "sum": true,
	"first": true, "last": true, "count": true,
}

// durationPattern 匹配单一 Flux duration，如 30s / 1m / 2h / 1d / 1w。
var durationPattern = regexp.MustCompile(`^\d+(ms|s|m|h|d|w)$`)

// rangePattern 匹配查询区间：相对时长（-30m、-1h）或 RFC3339 时间点。
//
// 只放行这两种形态：区间会被直接拼进 Flux 语句，任何多余的表达式都可能被当作查询注入。
var rangePattern = regexp.MustCompile(
	`^-?\d+(ms|s|m|h|d|w)(\d+(ms|s|m|h|d|w))*$|^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?)?$`)

// fieldPattern 匹配 Telegraf 字段名。
//
// 不能按「标识符」收紧：Windows 的 win_perf_counters 字段形如 `% Processor Time`、
// `Committed Bytes In Use`，含百分号、空格与点号，收紧会把 Windows 主机的字段全部拦掉。
var fieldPattern = regexp.MustCompile(`^[%A-Za-z0-9_][A-Za-z0-9_%\-./()#+ ]{0,63}$`)

// MetricQuery 描述一次时序指标查询。
//
// 它是 Flux 语句的唯一输入，构造后先经 normalize 收敛再做 validate，
// 保证拼进查询语句的每个片段都在白名单或正则约束内。
type MetricQuery struct {
	// Measurement 指标类型（measurement），必须在 allowedMeasurements 内。
	Measurement string
	// AgentID 服务器 ID，对应 global_tags.agent_id。
	AgentID string
	// Start 查询起点：相对时长（-30m）或 RFC3339。
	Start string
	// End 查询终点：为空表示到 now()。
	End string
	// Fields 字段白名单，为空表示不过滤字段。
	Fields []string
	// Aggregate 聚合方式：空表示原始点；rate 表示计数器转每秒速率；其余为 Flux aggregateWindow 的 fn。
	Aggregate string
	// Window 聚合窗口（Flux duration），仅在有聚合时生效。
	Window string
	// Limit 每条序列返回点数上限，<= 0 时取 defaultMetricLimit。
	Limit int
}

// SplitFields 把逗号分隔的字段查询参数切分为字段列表。
//
// 参数 Parameters:
//   - raw (string): 形如 "usage_user,usage_system" 的原始参数，可为空。
//
// 返回 Returns:
//   - fields ([]string): 去空、去重后的字段名；raw 为空时返回 nil。
func SplitFields(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	seen := make(map[string]bool)
	fields := make([]string, 0, 4)
	for _, item := range strings.Split(raw, ",") {
		field := strings.TrimSpace(item)
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		fields = append(fields, field)
		if len(fields) >= maxMetricFields {
			break
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// normalize 收敛查询参数：补默认值、统一大小写、按窗口大小推导默认聚合窗口。
//
// 只做「无害的补齐」：任何取值合法性判断都留给 validate，避免非法值被静默改写。
func (q *MetricQuery) normalize() {
	q.Measurement = strings.TrimSpace(q.Measurement)
	q.AgentID = strings.TrimSpace(q.AgentID)
	q.Start = strings.TrimSpace(q.Start)
	q.End = strings.TrimSpace(q.End)
	q.Aggregate = strings.ToLower(strings.TrimSpace(q.Aggregate))
	q.Window = strings.TrimSpace(q.Window)
	if q.Start == "" {
		q.Start = "-1h"
	}
	if q.Limit <= 0 {
		q.Limit = defaultMetricLimit
	}
	if q.Limit > maxMetricLimit {
		q.Limit = maxMetricLimit
	}
	if q.Aggregate != "" && q.Window == "" {
		// 有聚合无窗口时给 1m：Telegraf 间隔 10s/30s，1 分钟一档足够平滑且不丢峰。
		q.Window = "1m"
	}
	if q.Aggregate == "" {
		q.Window = ""
	}
}

// validate 校验查询参数，非法时返回包内哨兵错误供 api 层翻译为 400。
//
// 返回 Returns:
//   - err (error): 参数合法时为 nil；否则为 ErrInvalidMeasurement / ErrInvalidAgent /
//     ErrInvalidRange / ErrInvalidField / ErrInvalidAggregate。
func (q MetricQuery) validate() error {
	if !allowedMeasurements[q.Measurement] {
		return ErrInvalidMeasurement
	}
	if q.AgentID == "" || q.AgentID == "undefined" || q.AgentID == "null" || len(q.AgentID) > 128 {
		return ErrInvalidAgent
	}
	if !rangePattern.MatchString(q.Start) || len(q.Start) > 32 {
		return ErrInvalidRange
	}
	if q.End != "" && (!rangePattern.MatchString(q.End) || len(q.End) > 32) {
		return ErrInvalidRange
	}
	if len(q.Fields) > maxMetricFields {
		return ErrInvalidField
	}
	for _, field := range q.Fields {
		if !fieldPattern.MatchString(field) {
			return ErrInvalidField
		}
	}
	if q.Aggregate == "" {
		return nil
	}
	if q.Aggregate != aggregateRate && !allowedAggregates[q.Aggregate] {
		return ErrInvalidAggregate
	}
	seconds, ok := durationSeconds(q.Window)
	if !ok || seconds <= 0 || seconds > maxMetricWindowSeconds {
		return ErrInvalidRange
	}
	return nil
}

// durationSeconds 把 Flux duration 换算为秒数。
//
// 参数 Parameters:
//   - value (string): 形如 30s / 5m / 2h / 1d / 1w 的表达式。
//
// 返回 Returns:
//   - seconds (int): 换算结果；仅支持单一单位，不支持 1h30m 这类组合。
//   - ok (bool): 表达式是否合法。
func durationSeconds(value string) (seconds int, ok bool) {
	if !durationPattern.MatchString(value) {
		return 0, false
	}
	unitIndex := strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' })
	if unitIndex <= 0 {
		return 0, false
	}
	amount, err := strconv.Atoi(value[:unitIndex])
	if err != nil {
		return 0, false
	}
	switch value[unitIndex:] {
	case "ms":
		return amount / 1000, true
	case "s":
		return amount, true
	case "m":
		return amount * 60, true
	case "h":
		return amount * 3600, true
	case "d":
		return amount * 86400, true
	case "w":
		return amount * 604800, true
	default:
		return 0, false
	}
}

// flux 生成查询语句。
//
// 参数 Parameters:
//   - bucket (string): InfluxDB bucket 名，取自配置 influx.bucket；空串将生成非法语句，调用方需保证非空。
//
// 返回 Returns:
//   - query (string): Flux 语句；参数须先通过 validate。
//
// 行为约束：不做任何 IO，纯字符串拼装；所有插值都用 %q 转义，避免引号注入。
func (q MetricQuery) flux(bucket string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "from(bucket: %q)\n", bucket)
	if q.End != "" {
		fmt.Fprintf(&builder, "  |> range(start: %s, stop: %s)\n", q.Start, q.End)
	} else {
		fmt.Fprintf(&builder, "  |> range(start: %s)\n", q.Start)
	}

	conditions := []string{
		fmt.Sprintf("r._measurement == %q", q.Measurement),
		fmt.Sprintf("r.agent_id == %q", q.AgentID),
	}
	if len(q.Fields) == 1 {
		conditions = append(conditions, fmt.Sprintf("r._field == %q", q.Fields[0]))
	} else if len(q.Fields) > 1 {
		fieldConditions := make([]string, 0, len(q.Fields))
		for _, field := range q.Fields {
			fieldConditions = append(fieldConditions, fmt.Sprintf("r._field == %q", field))
		}
		conditions = append(conditions, "("+strings.Join(fieldConditions, " or ")+")")
	}
	fmt.Fprintf(&builder, "  |> filter(fn: (r) => %s)\n", strings.Join(conditions, " and "))

	switch q.Aggregate {
	case aggregateRate:
		// 计数器（net / diskio）直接画累计值没有意义，也无法描述「每秒」；
		// 先取窗口末值，再求每秒增量（nonNegative 抹平计数器重置），最后按窗口取均值。
		fmt.Fprintf(&builder, "  |> aggregateWindow(every: %s, fn: last, createEmpty: false)\n", q.Window)
		builder.WriteString("  |> derivative(unit: 1s, nonNegative: true)\n")
		fmt.Fprintf(&builder, "  |> aggregateWindow(every: %s, fn: mean, createEmpty: false)\n", q.Window)
	case "":
		// 无聚合：保持迁移前的原始点语义，由前端自行处理。
	default:
		fmt.Fprintf(&builder, "  |> aggregateWindow(every: %s, fn: %s, createEmpty: false)\n", q.Window, q.Aggregate)
	}
	fmt.Fprintf(&builder, "  |> limit(n: %d)", q.Limit)
	return builder.String()
}

// metaTagKeys 是 InfluxDB 查询结果里由引擎注入的元数据键，不属于业务标签。
var metaTagKeys = map[string]bool{
	"result": true,
	"table":  true,
}

// isMetaTagKey 判断标签键是否为查询引擎注入的元数据。
//
// 参数 Parameters:
//   - key (string): 标签键。
//
// 返回 Returns:
//   - meta (bool): 以 _ 开头或属于 result/table 时为 true。
func isMetaTagKey(key string) bool {
	return strings.HasPrefix(key, "_") || metaTagKeys[key]
}

// seriesKey 生成数据点所属序列的唯一键（业务标签 + 字段名）。
//
// 参数 Parameters:
//   - point (MetricPoint): 数据点。
//
// 返回 Returns:
//   - key (string): 形如 "name=eth0|interface=eth0|_field=bytes_sent" 的稳定字符串。
func seriesKey(point MetricPoint) string {
	keys := make([]string, 0, len(point.Tags))
	for key := range point.Tags {
		if isMetaTagKey(key) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, point.Tags[key]))
	}
	parts = append(parts, "field="+point.Field)
	return strings.Join(parts, "|")
}

// tagValues 返回点集中指定标签去重后的取值（升序）。
//
// 参数 Parameters:
//   - points ([]MetricPoint): 数据点。
//   - tagKey (string): 标签名，如 path / name / interface。
//
// 返回 Returns:
//   - values ([]string): 取值列表；无该标签时返回空切片（非 nil）。
func tagValues(points []MetricPoint, tagKey string) []string {
	seen := make(map[string]bool)
	values := make([]string, 0, 4)
	for _, point := range points {
		value := tagValue(point, tagKey)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

// tagValue 读取数据点的标签取值，缺失时返回空串。
//
// 参数 Parameters:
//   - point (MetricPoint): 数据点。
//   - tagKey (string): 标签名。
//
// 返回 Returns:
//   - value (string): 标签值。
func tagValue(point MetricPoint, tagKey string) string {
	raw, ok := point.Tags[tagKey]
	if !ok || raw == nil {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

// toFloat 把 Influx 返回值统一转换为 float64。
//
// 参数 Parameters:
//   - value (any): 字段值，可能是 float64 / int64 / uint64 / string 等。
//
// 返回 Returns:
//   - f (float64): 转换结果。
//   - ok (bool): 是否可转换为有限数值。
func toFloat(value any) (f float64, ok bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

// filterField 筛选出指定字段的数据点。
//
// 参数 Parameters:
//   - points ([]MetricPoint): 数据点。
//   - field (string): 字段名。
//
// 返回 Returns:
//   - filtered ([]MetricPoint): 命中字段的点；无命中时返回空切片（非 nil）。
func filterField(points []MetricPoint, field string) []MetricPoint {
	filtered := make([]MetricPoint, 0, len(points))
	for _, point := range points {
		if point.Field == field {
			filtered = append(filtered, point)
		}
	}
	return filtered
}

// hasField 判断点集中是否存在指定字段。
//
// 参数 Parameters:
//   - points ([]MetricPoint): 数据点。
//   - field (string): 字段名。
//
// 返回 Returns:
//   - exists (bool): 存在返回 true。
func hasField(points []MetricPoint, field string) bool {
	for _, point := range points {
		if point.Field == field {
			return true
		}
	}
	return false
}

// filterTag 按标签取值筛选数据点。
//
// 参数 Parameters:
//   - points ([]MetricPoint): 数据点。
//   - tagKey (string): 标签名。
//   - expected (string): 期望取值。
//
// 返回 Returns:
//   - filtered ([]MetricPoint): 命中的点；无命中时返回空切片（非 nil）。
func filterTag(points []MetricPoint, tagKey, expected string) []MetricPoint {
	filtered := make([]MetricPoint, 0, len(points))
	for _, point := range points {
		if tagValue(point, tagKey) == expected {
			filtered = append(filtered, point)
		}
	}
	return filtered
}

// latestValue 取指定字段最新的数值。
//
// 参数 Parameters:
//   - points ([]MetricPoint): 数据点（不限序列，按时间取最新）。
//   - field (string): 字段名。
//
// 返回 Returns:
//   - value (float64): 最新值。
//   - ok (bool): 无该字段或值不可转换时返回 false。
func latestValue(points []MetricPoint, field string) (value float64, ok bool) {
	var latestAt time.Time
	found := false
	for _, point := range points {
		if point.Field != field {
			continue
		}
		numeric, valid := toFloat(point.Value)
		if !valid {
			continue
		}
		if !found || point.Time.After(latestAt) {
			latestAt = point.Time
			value = numeric
			found = true
		}
	}
	return value, found
}

// latestByTag 取指定字段在每个标签分组下的最新值。
//
// 参数 Parameters:
//   - points ([]MetricPoint): 数据点。
//   - field (string): 字段名。
//   - tagKey (string): 分组标签名，如 path / name / interface。
//
// 返回 Returns:
//   - values (map[string]float64): 标签取值 → 最新值；无数据时为空 map（非 nil）。
func latestByTag(points []MetricPoint, field string, tagKey string) map[string]float64 {
	type entry struct {
		at    time.Time
		value float64
	}
	latest := make(map[string]entry)
	for _, point := range points {
		if point.Field != field {
			continue
		}
		numeric, valid := toFloat(point.Value)
		if !valid {
			continue
		}
		key := tagValue(point, tagKey)
		current, exists := latest[key]
		if !exists || point.Time.After(current.at) {
			latest[key] = entry{at: point.Time, value: numeric}
		}
	}
	values := make(map[string]float64, len(latest))
	for key, item := range latest {
		values[key] = item.value
	}
	return values
}

// rateByTag 计算计数器在每个标签分组下的每秒增量。
//
// 参数 Parameters:
//   - points ([]MetricPoint): 计数器原始点。
//   - field (string): 计数字段名，如 bytes_sent / read_bytes。
//   - tagKey (string): 分组标签名，如 name / interface。
//
// 返回 Returns:
//   - rates (map[string]float64): 标签取值 → 每秒增量；计数器重置（新值 < 旧值）按 0 处理，
//     单个分组内不足两个点时不产出该键。
//
// 行为约束：纯计算，不发起 IO；对每个分组各取「时间最新的两个点」，与采样间隔无关。
func rateByTag(points []MetricPoint, field string, tagKey string) map[string]float64 {
	type sample struct {
		at    time.Time
		value float64
	}
	grouped := make(map[string][]sample)
	for _, point := range points {
		if point.Field != field {
			continue
		}
		numeric, valid := toFloat(point.Value)
		if !valid {
			continue
		}
		key := tagValue(point, tagKey)
		grouped[key] = append(grouped[key], sample{at: point.Time, value: numeric})
	}

	rates := make(map[string]float64, len(grouped))
	for key, samples := range grouped {
		if len(samples) < 2 {
			continue
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i].at.Before(samples[j].at) })
		previous := samples[len(samples)-2]
		current := samples[len(samples)-1]
		seconds := current.at.Sub(previous.at).Seconds()
		if seconds <= 0 {
			continue
		}
		delta := current.value - previous.value
		if delta < 0 {
			// 计数器重置（进程重启 / 网卡重置）或回绕：按 0 处理，避免出现负速率。
			delta = 0
		}
		rates[key] = delta / seconds
	}
	return rates
}

// sumLatestPrefixed 求「指定前缀的全部字段」最新值之和。
//
// 用途：netstat 的 TCP 连接数按状态分散在 tcp_established / tcp_listen / tcp_time_wait 等字段里，
// 后端只提供分状态计数，展示层要的是总量。
//
// 参数 Parameters:
//   - points ([]MetricPoint): 数据点。
//   - prefix (string): 字段前缀，如 "tcp_"。
//
// 返回 Returns:
//   - total (float64): 各字段最新值之和；无匹配字段时为 0。
func sumLatestPrefixed(points []MetricPoint, prefix string) float64 {
	total := 0.0
	for _, field := range fieldsWithPrefix(points, prefix) {
		if value, ok := latestValue(points, field); ok {
			total += value
		}
	}
	return total
}

// fieldsWithPrefix 返回点集中具有指定前缀的字段名（去重、升序）。
//
// 参数 Parameters:
//   - points ([]MetricPoint): 数据点。
//   - prefix (string): 字段前缀。
//
// 返回 Returns:
//   - fields ([]string): 字段名列表；无匹配时返回空切片（非 nil）。
func fieldsWithPrefix(points []MetricPoint, prefix string) []string {
	seen := make(map[string]bool)
	fields := make([]string, 0, 4)
	for _, point := range points {
		if !strings.HasPrefix(point.Field, prefix) || seen[point.Field] {
			continue
		}
		seen[point.Field] = true
		fields = append(fields, point.Field)
	}
	sort.Strings(fields)
	return fields
}
