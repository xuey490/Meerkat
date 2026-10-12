package resourcealert

import (
	"testing"
	"time"
)

// TestEvaluateTimeout 锁住「单轮评估必须有时间上限」这一防御行为。
//
// 背景：Run 的评估循环是同步的，时序库无响应时若这一轮没有超时，整个评估会永久停摆，
// 而且因为「没有返回错误」连一条失败日志都不会有（表现为「告警中心不再更新，
// 但服务看起来一切正常」）。回归代价很高，所以用测试固定住上限规则。
func TestEvaluateTimeout(t *testing.T) {
	cases := []struct {
		name     string
		interval time.Duration
		want     time.Duration
	}{
		{"未配置时用上限", 0, maxEvaluateTimeout},
		{"负值用上限", -time.Second, maxEvaluateTimeout},
		{"常规周期用周期本身", 60 * time.Second, 60 * time.Second},
		{"超长周期封顶", 10 * time.Minute, maxEvaluateTimeout},
		{"正好等于上限", maxEvaluateTimeout, maxEvaluateTimeout},
	}
	for _, c := range cases {
		if got := evaluateTimeout(c.interval); got != c.want {
			t.Errorf("%s: evaluateTimeout(%v) = %v，期望 %v", c.name, c.interval, got, c.want)
		}
	}
}
