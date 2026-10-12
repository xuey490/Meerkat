package resourcealert

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

// ErrTimeSeriesUnavailable 表示时序库不可用或查询失败。
//
// 评估层据此「保持上一次状态」：不能因为 Influx 抖动就把在告警的状态判成恢复。
var ErrTimeSeriesUnavailable = errors.New("时序库查询失败")

// metricQuery 描述一个指标在 InfluxDB 中的取数方式。
type metricQuery struct {
	// measurement Telegraf 的 measurement 名。
	measurement string
	// field 参与判定的字段（聚合后由评估层换算成指标值）。
	field string
	// extraFilter 附加的 Flux 过滤片段（如只看 cpu-total）。
	extraFilter string
	// keepTags 需要额外保留的 tag 列。
	keepTags []string
	// groupTag 用于区分告警目标的 tag（磁盘为挂载点 path）。
	groupTag string
}

// metricQueries 是指标到 Flux 取数方式的映射。
//
// 口径说明（与 rule.go 的指标定义一致）：
//   - cpu: 整机利用率取 cpu-total 的 usage_idle，评估层用 100-usage_idle 换算；
//   - mem: 用 available_percent 而非 used_percent，避免把可回收 cache 算成「已用」；
//   - system: load1 是绝对值，评估层除以逻辑核数后才可比；
//   - disk: used_percent 按 path 分组，fstype 一并取回供规则过滤。
var metricQueries = map[string]metricQuery{
	MetricCPUUsage: {
		measurement: "cpu",
		field:       "usage_idle",
		extraFilter: ` and r.cpu == "cpu-total"`,
	},
	MetricMemUsage: {
		measurement: "mem",
		field:       "available_percent",
	},
	MetricLoadPerCore: {
		measurement: "system",
		field:       "load1",
	},
	MetricDiskUsage: {
		measurement: "disk",
		field:       "used_percent",
		keepTags:    []string{"path", "fstype"},
		groupTag:    "path",
	},
}

// QuerySpec 描述一次时序窗口查询。
type QuerySpec struct {
	// MetricKey 指标标识。
	MetricKey string
	// AgentIDs 待查询的主机集合；同规则下的多主机合并为一次查询。
	AgentIDs []string
	// Window 查询窗口（判定期 + 余量）。
	Window time.Duration
	// Step 聚合粒度（与评估周期一致）。
	Step time.Duration
}

// WindowPoint 是窗口内的一个聚合采样点。
type WindowPoint struct {
	// At 采样时间。
	At time.Time
	// Value 该字段在聚合窗口内的均值（原始口径，未换算）。
	Value float64
}

// MetricWindow 是某主机（磁盘为某挂载点）在一段时间窗口内的聚合序列。
type MetricWindow struct {
	// AgentID 主机标识。
	AgentID string
	// Target 告警目标：磁盘为挂载点，其余为空。
	Target string
	// FSType 文件系统类型（仅磁盘），供规则过滤伪文件系统。
	FSType string
	// Points 按时间升序的聚合采样点。
	Points []WindowPoint
}

// Querier 是资源告警的时序窗口查询器。
type Querier struct {
	// client InfluxDB 客户端；为 nil 时表示时序库不可用。
	client influxdb2.Client
	// org 组织名。
	org string
	// bucket 桶名。
	bucket string
	// maxPoints 单个序列返回点数上限保护。
	maxPoints int
}

// NewQuerier 创建时序窗口查询器。
//
// 参数 Parameters:
//   - client (influxdb2.Client): InfluxDB 客户端，可为 nil。
//   - org (string): 组织名。
//   - bucket (string): 桶名。
//   - maxPoints (int): 单序列点数上限，非正数时取 2000。
//
// 返回 Returns:
//   - querier (*Querier): 查询器。
func NewQuerier(client influxdb2.Client, org, bucket string, maxPoints int) *Querier {
	if maxPoints <= 0 {
		maxPoints = 2000
	}
	return &Querier{client: client, org: org, bucket: bucket, maxPoints: maxPoints}
}

// QueryWindow 查询指定指标在窗口内的聚合序列，按「主机 + 目标（挂载点）」归组。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - spec (QuerySpec): 查询描述。
//
// 返回 Returns:
//   - windows ([]MetricWindow): 各主机（及各挂载点）的聚合序列。
//   - err (error): 指标不支持或查询失败时返回 ErrTimeSeriesUnavailable。
func (q *Querier) QueryWindow(ctx context.Context, spec QuerySpec) ([]MetricWindow, error) {
	mq, ok := metricQueries[spec.MetricKey]
	if !ok {
		return nil, fmt.Errorf("%w: 未知指标 %s", ErrTimeSeriesUnavailable, spec.MetricKey)
	}
	if len(spec.AgentIDs) == 0 {
		return nil, nil
	}
	if q.client == nil {
		return nil, ErrTimeSeriesUnavailable
	}
	query := q.buildQuery(spec, mq)
	result, err := q.client.QueryAPI(q.org).Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTimeSeriesUnavailable, err)
	}
	defer result.Close()

	windows := make(map[string]*MetricWindow, len(spec.AgentIDs))
	for result.Next() {
		record := result.Record()
		agentID, _ := record.ValueByKey("agent_id").(string)
		if agentID == "" {
			continue
		}
		target, fstype := "", ""
		if mq.groupTag != "" {
			target, _ = record.ValueByKey(mq.groupTag).(string)
			fstype, _ = record.ValueByKey("fstype").(string)
		}
		value, ok := toFloat(record.Value())
		if !ok {
			continue
		}
		key := agentID + "\x00" + target
		window, exists := windows[key]
		if !exists {
			window = &MetricWindow{AgentID: agentID, Target: target, FSType: fstype}
			windows[key] = window
		}
		window.Points = append(window.Points, WindowPoint{At: record.Time(), Value: value})
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTimeSeriesUnavailable, err)
	}

	out := make([]MetricWindow, 0, len(windows))
	for _, window := range windows {
		// 聚合结果按时间升序，判定「持续时长」时才能从最早的超标点起算。
		sort.Slice(window.Points, func(i, j int) bool { return window.Points[i].At.Before(window.Points[j].At) })
		out = append(out, *window)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AgentID != out[j].AgentID {
			return out[i].AgentID < out[j].AgentID
		}
		return out[i].Target < out[j].Target
	})
	return out, nil
}

// buildQuery 生成 Flux 查询语句。
//
// 关键设计：把同一规则下的多台主机合并成一次查询（agent_id 正则），
// 20 台机 × 4 类规则从 80 次/分钟降到 4 次/分钟。
//
// 参数 Parameters:
//   - spec (QuerySpec): 查询描述。
//   - mq (metricQuery): 指标取数方式。
//
// 返回 Returns:
//   - query (string): Flux 语句。
func (q *Querier) buildQuery(spec QuerySpec, mq metricQuery) string {
	agentFilter := ""
	if len(spec.AgentIDs) > 0 {
		parts := make([]string, 0, len(spec.AgentIDs))
		for _, id := range spec.AgentIDs {
			// 主机 id 可能含正则元字符，必须转义后再拼进正则。
			parts = append(parts, regexp.QuoteMeta(id))
		}
		agentFilter = fmt.Sprintf("\n  |> filter(fn: (r) => r.agent_id =~ /^(%s)$/)", strings.Join(parts, "|"))
	}
	columns := []string{`"_time"`, `"_value"`, `"agent_id"`}
	for _, tag := range mq.keepTags {
		columns = append(columns, strconv.Quote(tag))
	}
	return fmt.Sprintf(`from(bucket: %q)
  |> range(start: -%ds)
  |> filter(fn: (r) => r._measurement == %q and r._field == %q%s)%s
  |> aggregateWindow(every: %ds, fn: mean, createEmpty: false)
  |> keep(columns: [%s])
  |> limit(n: %d)`,
		q.bucket,
		int(spec.Window.Seconds()),
		mq.measurement,
		mq.field,
		mq.extraFilter,
		agentFilter,
		int(spec.Step.Seconds()),
		strings.Join(columns, ", "),
		q.maxPoints)
}

// toFloat 把 Influx 返回值统一转换为 float64。
//
// 参数 Parameters:
//   - value (any): 字段值。
//
// 返回 Returns:
//   - f (float64): 转换结果。
//   - ok (bool): 是否可转换。
func toFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case uint64:
		return float64(v), true
	default:
		return 0, false
	}
}
