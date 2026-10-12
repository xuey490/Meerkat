package monitor

import (
	"errors"
	"strings"
	"testing"
)

// TestSplitFields 覆盖字段参数的切分、去空与去重。
func TestSplitFields(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"empty", "", nil},
		{"single", "usage_user", []string{"usage_user"}},
		{"multi", "usage_user, usage_system ,usage_idle", []string{"usage_user", "usage_system", "usage_idle"}},
		{"dedup", "used,used", []string{"used"}},
		{"windows field with space", "% Processor Time,Committed Bytes In Use", []string{"% Processor Time", "Committed Bytes In Use"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitFields(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("SplitFields(%q) = %v, want %v", tt.raw, got, tt.want)
			}
			for index := range got {
				if got[index] != tt.want[index] {
					t.Fatalf("SplitFields(%q)[%d] = %q, want %q", tt.raw, index, got[index], tt.want[index])
				}
			}
		})
	}
}

// TestDurationSeconds 覆盖聚合窗口的换算与非法取值。
func TestDurationSeconds(t *testing.T) {
	tests := []struct {
		value string
		want  int
		ok    bool
	}{
		{"1m", 60, true},
		{"30s", 30, true},
		{"2h", 7200, true},
		{"1d", 86400, true},
		{"1w", 604800, true},
		{"1h30m", 0, false},
		{"m", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, ok := durationSeconds(tt.value)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("durationSeconds(%q) = (%d, %v), want (%d, %v)", tt.value, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// TestMetricQueryValidate 覆盖参数校验：合法组合放行，非法组合返回对应哨兵错误。
func TestMetricQueryValidate(t *testing.T) {
	tests := []struct {
		name  string
		query MetricQuery
		want  error
	}{
		{
			name:  "linux 默认查询",
			query: MetricQuery{Measurement: "cpu", AgentID: "01H", Start: "-1h", Limit: 100},
		},
		{
			// 详情页「主机概况 / 交换分区」与「监控图表 / 交换分区使用率」依赖它。
			name:  "交换分区查询",
			query: MetricQuery{Measurement: "swap", AgentID: "01H", Start: "-30m", Aggregate: "mean", Window: "1m"},
		},
		{
			// 详情页「连接数」卡片与「网络连接数」图表依赖它。
			name:  "连接数查询",
			query: MetricQuery{Measurement: "netstat", AgentID: "01H", Start: "-30m", Aggregate: "mean", Window: "1m"},
		},
		{
			name:  "计数器速率查询",
			query: MetricQuery{Measurement: "net", AgentID: "01H", Start: "-30m", Aggregate: "rate", Window: "1m", Limit: 500},
		},
		{
			name:  "均值聚合与字段过滤",
			query: MetricQuery{Measurement: "disk", AgentID: "01H", Start: "-6h", Aggregate: "mean", Window: "5m", Fields: []string{"used_percent"}},
		},
		{
			name:  "RFC3339 区间",
			query: MetricQuery{Measurement: "mem", AgentID: "01H", Start: "2026-10-10T00:00:00Z", End: "2026-10-10T01:00:00Z"},
		},
		{
			name:  "measurement 不在白名单",
			query: MetricQuery{Measurement: "secrets", AgentID: "01H", Start: "-1h"},
			want:  ErrInvalidMeasurement,
		},
		{
			name:  "agent 为空",
			query: MetricQuery{Measurement: "cpu", AgentID: "null", Start: "-1h"},
			want:  ErrInvalidAgent,
		},
		{
			name:  "区间是表达式",
			query: MetricQuery{Measurement: "cpu", AgentID: "01H", Start: "-1h) |> drop()"},
			want:  ErrInvalidRange,
		},
		{
			name:  "字段带引号",
			query: MetricQuery{Measurement: "cpu", AgentID: "01H", Start: "-1h", Fields: []string{`x" or true`}},
			want:  ErrInvalidField,
		},
		{
			name:  "聚合方式非法",
			query: MetricQuery{Measurement: "cpu", AgentID: "01H", Start: "-1h", Aggregate: "median_x", Window: "1m"},
			want:  ErrInvalidAggregate,
		},
		{
			name:  "窗口非法",
			query: MetricQuery{Measurement: "cpu", AgentID: "01H", Start: "-1h", Aggregate: "mean", Window: "1h30m"},
			want:  ErrInvalidRange,
		},
		{
			name:  "窗口超过上限",
			query: MetricQuery{Measurement: "cpu", AgentID: "01H", Start: "-1h", Aggregate: "mean", Window: "2d"},
			want:  ErrInvalidRange,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := tt.query
			query.normalize()
			err := query.validate()
			if tt.want == nil {
				if err != nil {
					t.Fatalf("validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("validate() = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestMetricQueryNormalize 覆盖默认值补齐与 limit 收敛。
func TestMetricQueryNormalize(t *testing.T) {
	query := MetricQuery{Measurement: " cpu ", AgentID: " 01H ", Aggregate: "RATE", Limit: -3}
	query.normalize()
	if query.Measurement != "cpu" || query.AgentID != "01H" {
		t.Fatalf("normalize() 未裁剪空白: %+v", query)
	}
	if query.Start != "-1h" {
		t.Fatalf("normalize() start = %q, want -1h", query.Start)
	}
	if query.Aggregate != "rate" || query.Window != "1m" {
		t.Fatalf("normalize() 未补窗口: aggregate=%q window=%q", query.Aggregate, query.Window)
	}
	if query.Limit != defaultMetricLimit {
		t.Fatalf("normalize() limit = %d, want %d", query.Limit, defaultMetricLimit)
	}

	over := MetricQuery{Measurement: "cpu", AgentID: "01H", Limit: maxMetricLimit + 1}
	over.normalize()
	if over.Limit != maxMetricLimit {
		t.Fatalf("normalize() limit = %d, want %d", over.Limit, maxMetricLimit)
	}

	raw := MetricQuery{Measurement: "cpu", AgentID: "01H", Aggregate: "mean", Window: "1m"}
	raw.Aggregate = ""
	raw.normalize()
	if raw.Window != "" {
		t.Fatalf("normalize() 无聚合时应清空窗口, got %q", raw.Window)
	}
}

// TestMetricQueryFlux 覆盖 Flux 语句拼装：原始点 / 聚合 / 计数器速率三种形态。
func TestMetricQueryFlux(t *testing.T) {
	raw := MetricQuery{Measurement: "cpu", AgentID: `a"b`, Start: "-1h", Limit: 100}
	raw.normalize()
	got := raw.flux("bucket")
	for _, want := range []string{
		`from(bucket: "bucket")`,
		`|> range(start: -1h)`,
		`r._measurement == "cpu"`,
		`r.agent_id == "a\"b"`,
		`|> limit(n: 100)`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("flux() 缺少 %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "aggregateWindow") {
		t.Fatalf("flux() 无聚合时不应出现 aggregateWindow:\n%s", got)
	}

	agg := MetricQuery{Measurement: "disk", AgentID: "01H", Start: "-6h", Aggregate: "mean", Window: "5m", Fields: []string{"used", "total"}, Limit: 200}
	agg.normalize()
	got = agg.flux("bucket")
	if !strings.Contains(got, `(r._field == "used" or r._field == "total")`) {
		t.Fatalf("flux() 字段过滤不正确:\n%s", got)
	}
	if !strings.Contains(got, "|> aggregateWindow(every: 5m, fn: mean, createEmpty: false)") {
		t.Fatalf("flux() 聚合窗口不正确:\n%s", got)
	}

	rate := MetricQuery{Measurement: "net", AgentID: "01H", Start: "-30m", Aggregate: "rate", Window: "1m", Limit: 300}
	rate.normalize()
	got = rate.flux("bucket")
	if !strings.Contains(got, "derivative(unit: 1s, nonNegative: true)") {
		t.Fatalf("flux() 计数器速率缺少 derivative:\n%s", got)
	}
	if !strings.Contains(got, "|> aggregateWindow(every: 1m, fn: last, createEmpty: false)") {
		t.Fatalf("flux() 计数器速率缺少取末值窗口:\n%s", got)
	}
}
