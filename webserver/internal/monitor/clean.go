package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// metricCleanTimeout 是单次清理时序数据的超时。
//
// Influx 的 delete 是同步操作，按时间分片扫描并重写分片，数据量大时耗时明显；
// 但也不能让 HTTP 请求无限挂住，因此给一个较宽松的上限。
const metricCleanTimeout = 2 * time.Minute

// CleanAgentMetricsResult 是一次「清理某台主机的采集数据」的结果。
type CleanAgentMetricsResult struct {
	// AgentID 主机标识。
	AgentID string `json:"agentId"`
	// InfluxCleared 时序库中该主机的数据是否已删除。
	//
	// Influx 的 delete 接口不返回删除点数，因此这里只表达「删除请求已成功」。
	InfluxCleared bool `json:"influxCleared"`
	// ReceiptsDeleted 删除的采集回执条数（PostgreSQL metric_receipts）。
	ReceiptsDeleted int64 `json:"receiptsDeleted"`
}

// CleanAgentMetrics 清理某台主机的采集数据（服务器列表「清理数据」入口）。
//
// 两个落点，缺一不可：
//  1. InfluxDB：按 agent_id 谓词删除该主机的全部序列（整段时间范围）；
//  2. PostgreSQL：删除 metric_receipts 中该主机的采集回执——Collector 用它做
//     (agent_id, sequence) 幂等去重，残留回执会让重新上报的同一 sequence 被判为
//     「已处理」而直接丢弃，表现为「清理后数据再也回不来」。
//
// 顺序有意如此：时序库删失败就直接返回、不动回执表，避免出现「指标还在、回执先没了」
// 这种既没清干净、又干扰后续上报的中间态。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - agentID (string): 主机标识。
//
// 返回 Returns:
//   - result (CleanAgentMetricsResult): 清理结果（含回执条数）。
//   - err (error): 任一步失败时返回哨兵错误（ErrInfluxDeleteFailed / ErrMonitorDBDeleteFailed）。
func (s *Service) CleanAgentMetrics(ctx context.Context, agentID string) (CleanAgentMetricsResult, error) {
	result := CleanAgentMetricsResult{AgentID: agentID}
	if err := s.deleteInfluxAgentData(ctx, agentID); err != nil {
		return result, err
	}
	result.InfluxCleared = true
	deleted, err := s.deleteMetricReceipts(agentID)
	if err != nil {
		return result, err
	}
	result.ReceiptsDeleted = deleted
	return result, nil
}

// deleteInfluxAgentData 删除某主机在时序库中的全部数据点。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - agentID (string): 主机标识。
//
// 返回 Returns:
//   - err (error): 时序库不可用或删除失败时返回 ErrInfluxDeleteFailed。
func (s *Service) deleteInfluxAgentData(ctx context.Context, agentID string) error {
	if s.influx == nil {
		slog.Warn("时序库客户端未初始化，跳过指标清理", "agent_id", agentID)
		return ErrInfluxDeleteFailed
	}
	cleanCtx, cancel := context.WithTimeout(ctx, metricCleanTimeout)
	defer cancel()

	// 谓词里的字符串字面量必须转义：主机标识来自路径参数，直接拼接会改变谓词语义。
	predicate := "agent_id = " + influxStringLiteral(agentID)
	// 时间范围取「纪元 → 现在 + 1 分钟」：删除按时间分片执行，上界留余量，
	// 避免刚好落在当前分片边界的点被漏掉。
	start := time.Unix(0, 0).UTC()
	stop := time.Now().Add(time.Minute)
	if err := s.influx.DeleteAPI().DeleteWithName(cleanCtx, s.influxOrg, s.influxBucket, start, stop, predicate); err != nil {
		slog.Error("删除主机时序数据失败",
			"agent_id", agentID, "org", s.influxOrg, "bucket", s.influxBucket,
			"predicate", predicate, "error", err)
		return fmt.Errorf("%w: %v", ErrInfluxDeleteFailed, err)
	}
	return nil
}

// deleteMetricReceipts 删除某主机的采集回执（Collector 的 metric_receipts 表）。
//
// 参数 Parameters:
//   - agentID (string): 主机标识。
//
// 返回 Returns:
//   - deleted (int64): 删除条数。
//   - err (error): 删除失败时返回 ErrMonitorDBDeleteFailed。
func (s *Service) deleteMetricReceipts(agentID string) (int64, error) {
	if s.db == nil {
		slog.Warn("监控库未初始化，跳过采集回执清理", "agent_id", agentID)
		return 0, ErrMonitorDBDeleteFailed
	}
	result := s.db.Exec("DELETE FROM metric_receipts WHERE agent_id = ?", agentID)
	if result.Error != nil {
		// 表由 Collector 侧建：没有该表说明这套环境没启用采集回执，属「无可清理」而非失败。
		if isUndefinedTable(result.Error) {
			return 0, nil
		}
		slog.Error("删除采集回执失败", "agent_id", agentID, "error", result.Error)
		return 0, fmt.Errorf("%w: %v", ErrMonitorDBDeleteFailed, result.Error)
	}
	return result.RowsAffected, nil
}

// influxStringLiteral 把字符串包装成 Influx 谓词中的字符串字面量（转义反斜杠与双引号）。
//
// 参数 Parameters:
//   - value (string): 原始文本。
//
// 返回 Returns:
//   - literal (string): 带引号的谓词字面量。
func influxStringLiteral(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
	return `"` + escaped + `"`
}

// isUndefinedTable 判断错误是否为「表不存在」（PostgreSQL SQLSTATE 42P01）。
//
// 参数 Parameters:
//   - err (error): 底层错误。
//
// 返回 Returns:
//   - undefined (bool): 表不存在时为 true。
func isUndefinedTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "42P01")
}
