package resourcealert

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"

	"github.com/company/monitor-webserver/internal/eventhub"
)

// alertSSETopic 是告警推送的 SSE 主题，与前端 src/enums/sse.ts 的 MONITOR_ALERT 一致。
const alertSSETopic = "monitor-alert"

// maxEvaluateTimeout 是单轮评估的时间上限。
//
// 时序库若无响应（连接半开、服务假死），查询会一直挂着——而 Run 的循环是同步的，
// 结果就是整个评估永久停摆；更麻烦的是它「没有返回错误」，因此连一条失败日志都没有，
// 表现为「告警中心从此不再更新，但服务看起来一切正常」。
const maxEvaluateTimeout = 2 * time.Minute

// Options 是资源告警服务的运行参数，由装配层从 webserver.yaml 的 alert 段构造。
//
// 阈值与判定期不在此处——它们以数据库中的告警规则为准（可在界面按全局/角色/单机调整）。
type Options struct {
	// Enabled 是否启用资源告警评估。
	Enabled bool
	// EvaluateInterval 评估周期。
	EvaluateInterval time.Duration
	// MergeHosts 是否把同一规则下的多台主机合并为一次 Flux 查询。
	MergeHosts bool
	// MaxPoints 单次查询返回点数上限保护。
	MaxPoints int
	// TopN 归因进程条数。
	TopN int
	// PreferProcstat 是否优先使用进程级采集（procstat）数据做归因。
	PreferProcstat bool
	// Log 告警日志输出参数。
	Log LogOptions
}

// LogOptions 是资源告警日志的输出参数。
//
// 告警按「主机 + 日期」分文件，因此这里给的是目录与命名模板，而不是固定文件路径。
type LogOptions struct {
	// Enabled 是否写独立日志文件。
	Enabled bool
	// Format 输出形态：json / text / both。
	Format string
	// Dir 告警日志目录。
	Dir string
	// NamePattern 文件命名模板（不含扩展名），支持 {agent} 与 {date}；
	// 默认 {agent}-{date}-alerts，展开后同时产出 .log（结构化）与 .txt（人读）。
	NamePattern string
	// MaxSizeMB 单文件大小上限（MB）。
	MaxSizeMB int
	// MaxBackups 保留的历史文件数。
	MaxBackups int
	// MaxAgeDays 历史文件保留天数。
	MaxAgeDays int
	// Compress 是否压缩历史文件。
	Compress bool
}

// Service 是资源告警的装配根。
type Service struct {
	// store 规则与告警的持久化入口。
	store *Store
	// opts 运行参数。
	opts Options
	// logger 结构化日志（落业务日志，便于与告警日志交叉排查）。
	logger *slog.Logger
	// cancel 后台评估协程的取消函数（Start 时创建，Stop 时释放）。
	cancel context.CancelFunc
	// querier 时序窗口查询器。
	querier *Querier
	// attributor 归因器。
	attributor *Attributor
	// machine 状态机（抑制重复输出）。
	machine *Machine
	// writer 告警日志写入器（可能为 nil，表示只走业务日志）。
	writer *Writer
	// events SSE 广播中心（可能为 nil）。
	events *eventhub.Hub
}

// New 创建资源告警服务。
//
// 参数 Parameters:
//   - store (*Store): 持久化入口。
//   - client (influxdb2.Client): 时序库客户端（可为 nil，此时归因退化为心跳快照）。
//   - org (string): 时序库组织名。
//   - bucket (string): 时序库桶名。
//   - events (*eventhub.Hub): SSE 广播中心，可为 nil。
//   - opts (Options): 运行参数。
//   - logger (*slog.Logger): 结构化日志器；为 nil 时使用 slog.Default()。
//
// 返回 Returns:
//   - service (*Service): 资源告警服务。
//   - err (error): 告警日志目录创建失败时返回非 nil。
func New(store *Store, client influxdb2.Client, org, bucket string, events *eventhub.Hub, opts Options, logger *slog.Logger) (*Service, error) {
	if logger == nil {
		logger = slog.Default()
	}
	writer, err := NewWriter(opts.Log)
	if err != nil {
		return nil, err
	}
	return &Service{
		store:      store,
		opts:       opts,
		logger:     logger,
		querier:    NewQuerier(client, org, bucket, opts.MaxPoints),
		attributor: NewAttributor(store, client, org, bucket, opts.TopN, opts.PreferProcstat),
		machine:    NewMachine(),
		writer:     writer,
		events:     events,
	}, nil
}

// Store 返回持久化入口（供 API 层做规则 CRUD 与告警查询）。
//
// 返回 Returns:
//   - store (*Store): 持久化入口。
func (s *Service) Store() *Store { return s.store }

// Options 返回运行参数（供 API 层与输出层读取，如归因 TopN、日志格式）。
//
// 返回 Returns:
//   - opts (Options): 运行参数。
func (s *Service) Options() Options { return s.opts }

// Migrate 幂等迁移告警相关表，并在全局层没有任何规则时写入内置默认值。
//
// 返回 Returns:
//   - err (error): 迁移或默认值写入失败时返回非 nil。
func (s *Service) Migrate() error {
	if err := s.store.Migrate(); err != nil {
		return err
	}
	if err := s.store.EnsureDefaults(ScopeGlobal); err != nil {
		return err
	}
	rules, err := s.store.ListManagedRules()
	if err != nil {
		return err
	}
	// 启动时确认规则就绪，并把「进程归因是否可用」一并说明，便于排查归因缺失。
	s.logger.Info("资源告警规则已就绪", "count", len(rules), "prefer_procstat", s.opts.PreferProcstat)
	// 恢复运行态：不恢复的话，已处于告警的主机会被重新判为「首次触发」而重复告警。
	if err := s.restoreStates(rules); err != nil {
		s.logger.Warn("恢复告警运行态失败，重启后可能重复输出一次告警", "error", err)
	}
	return nil
}

// Acknowledge 人工确认并结束一条告警，同时结束状态机里的当前告警周期。
//
// 只关数据库那一行是不够的：状态机仍停在「警告/严重」，指标没恢复时它只会在重申间隔
// 到达后再输出一条 repeated，于是新记录沿用了旧周期的首次触发时间——看起来就像
// 新告警叠加在已处理的记录上。这里把状态机复位为「正常 + 自确认时刻起冷却」，
// 冷却期后仍超标时输出的是 fired，即一条时间与计数都重新计算的全新告警。
//
// 参数 Parameters:
//   - id (uint): 告警主键。
//
// 返回 Returns:
//   - closedNow (bool): 本次是否真的结束了该告警；重复确认时为 false。
//   - err (error): 更新失败时返回非 nil。
func (s *Service) Acknowledge(id uint) (bool, error) {
	alert, closedNow, err := s.store.Acknowledge(id)
	if err != nil {
		return false, err
	}
	// 只复位本模块管理的指标告警；collector 端的离线告警没有 metric_key，与状态机无关。
	if closedNow && alert.MetricKey != "" && alert.DedupKey != "" {
		s.machine.Reset(alert.DedupKey, time.Now())
	}
	return closedNow, nil
}

// DeleteAlerts 清理告警记录（告警中心的多选清理）。
//
// 与人工确认同一思路：被清理的若是「仍在活动」的告警，就把状态机里的周期一并结束，
// 否则内存仍认为该指标在告警，后续重申会再生成一条新记录（表现为「清理后又冒出来」）。
// 已结束的历史记录只做数据删除，不触碰状态机。
//
// 参数 Parameters:
//   - ids ([]int64): 告警主键列表（来自前端逗号分隔的路径参数）。
//
// 返回 Returns:
//   - removed (int): 实际删除的条数。
//   - err (error): 删除失败时返回非 nil。
func (s *Service) DeleteAlerts(ids []int64) (int, error) {
	targets := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			targets = append(targets, uint(id))
		}
	}
	removed, err := s.store.DeleteAlerts(targets)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	for _, alert := range removed {
		if alert.ClosedAt == nil && alert.MetricKey != "" && alert.DedupKey != "" {
			s.machine.Reset(alert.DedupKey, now)
		}
	}
	return len(removed), nil
}

// restoreStates 从 alerts 表重建状态机运行态。
//
// 状态机是内存态（进程重启即丢失），而告警状态是持久化的；两者若不同步，
// 重启后会出现「库里已是 warning，却再次输出 fired」的重复告警。
//
// 参数 Parameters:
//   - rules ([]AlertRule): 本模块管理的规则（用于取最大冷却时长）。
//
// 返回 Returns:
//   - err (error): 查询失败时返回非 nil。
func (s *Service) restoreStates(rules []AlertRule) error {
	now := time.Now()
	alerts, err := s.store.ActiveResourceAlerts()
	if err != nil {
		return err
	}
	for _, alert := range alerts {
		lastWrite := alert.UpdatedAt
		if alert.LastSeenAt != nil {
			lastWrite = *alert.LastSeenAt
		}
		s.machine.Restore(alert.DedupKey, alert.State, alert.FirstFiredAt, lastWrite)
	}
	if len(alerts) > 0 {
		s.logger.Info("已恢复资源告警运行态", "count", len(alerts))
	}
	// 冷却窗口内刚结束的告警同样要恢复：否则重启会让冷却期凭空消失，
	// 指标仍超标的主机在下一轮立刻重新告警（与不重启时的行为不一致）。
	closed, err := s.store.RecentlyClosedResourceAlerts(now.Add(-maxCooldown(rules)))
	if err != nil {
		return err
	}
	for _, alert := range closed {
		if alert.ClosedAt == nil || alert.DedupKey == "" {
			continue
		}
		s.machine.Reset(alert.DedupKey, *alert.ClosedAt)
	}
	if len(closed) > 0 {
		s.logger.Info("已恢复冷却期内的资源告警运行态", "count", len(closed))
	}
	return nil
}

// maxCooldown 返回规则中最大的恢复冷却时长；未配置时回退到内置默认值。
//
// 参数 Parameters:
//   - rules ([]AlertRule): 本模块管理的规则。
//
// 返回 Returns:
//   - cooldown (time.Duration): 冷却时长。
func maxCooldown(rules []AlertRule) time.Duration {
	cooldown := defaultCooldown
	for _, rule := range rules {
		if value := time.Duration(rule.CooldownSeconds) * time.Second; value > cooldown {
			cooldown = value
		}
	}
	return cooldown
}

// Start 启动后台评估协程（非阻塞、幂等）。
//
// 与 job.Service 的启停风格保持一致：装配层调用 Start，进程退出时调用 Stop。
func (s *Service) Start() {
	if !s.opts.Enabled || s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.Run(ctx)
}

// Stop 停止后台评估协程并关闭日志文件（幂等）。
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if err := s.writer.Close(); err != nil {
		s.logger.Warn("关闭告警日志失败", "error", err)
	}
}

// Run 按评估周期驱动资源告警评估，直到 ctx 结束。
//
// 参数 Parameters:
//   - ctx (context.Context): 生命周期上下文，取消后退出。
func (s *Service) Run(ctx context.Context) {
	if !s.opts.Enabled {
		s.logger.Info("资源告警未启用（alert.enabled=false）")
		return
	}
	interval := s.opts.EvaluateInterval
	if interval <= 0 {
		interval = time.Minute
	}
	s.logger.Info("资源告警评估已启动", "interval", interval.String(), "top_n", s.opts.TopN)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("资源告警评估已停止")
			return
		case <-ticker.C:
			// 每轮都必须有时间上限：时序库无响应时查询会一直挂着，而本循环是同步的，
			// 没有上限就会永久停摆（且不产生任何日志，问题极难发现）。
			runCtx, cancel := context.WithTimeout(ctx, evaluateTimeout(interval))
			err := s.evaluateOnce(runCtx)
			cancel()
			if err != nil {
				// 单轮失败不退出循环：Influx 抖动或慢查询不应让评估整体停摆。
				s.logger.Error("资源告警评估失败", "error", err)
			}
		}
	}
}

// evaluateTimeout 计算单轮评估的时间上限。
//
// 参数 Parameters:
//   - interval (time.Duration): 评估周期。
//
// 返回 Returns:
//   - timeout (time.Duration): 时间上限；周期很长（或未配置）时封顶到 maxEvaluateTimeout。
func evaluateTimeout(interval time.Duration) time.Duration {
	if interval <= 0 || interval > maxEvaluateTimeout {
		return maxEvaluateTimeout
	}
	return interval
}

// evaluateOnce 执行一轮资源告警评估。
//
// 流程：装载规则与主机快照 → 按指标归集待评估主机 → 逐指标合并查询并判定 →
// 统一过状态机并输出（日志 / 告警表 / SSE）。
//
// 参数 Parameters:
//   - ctx (context.Context): 生命周期上下文。
//
// 返回 Returns:
//   - err (error): 本轮失败时返回非 nil。
func (s *Service) evaluateOnce(ctx context.Context) error {
	now := time.Now()
	rules, err := s.store.ListManagedRules()
	if err != nil {
		return err
	}
	if len(rules) == 0 {
		s.logger.Info("没有可用的资源告警规则，本轮评估跳过")
		return nil
	}
	snapshots, err := s.store.AgentSnapshots()
	if err != nil {
		return err
	}
	if len(snapshots) == 0 {
		// 这条日志很关键：没有在线主机时评估会「静默空转」——每轮都正常返回、
		// 不产生任何输出，看起来就像告警功能坏了。排查时第一步就该看有没有它。
		s.logger.Info("没有在线主机，本轮资源告警评估跳过")
		return nil
	}

	// 按指标归集：每台主机的生效规则可能来自不同层级，且不同主机可用不同规则。
	type target struct {
		agentID  string
		snapshot AgentSnapshot
		rule     EffectiveRule
	}
	byMetric := make(map[string][]target, len(metricSpecs))
	for agentID, snapshot := range snapshots {
		for metricKey, rule := range ResolveEffective(rules, agentID, snapshot.Role) {
			if !rule.Enabled {
				continue
			}
			byMetric[metricKey] = append(byMetric[metricKey], target{agentID: agentID, snapshot: snapshot, rule: rule})
		}
	}

	verdicts := make([]Verdict, 0, len(snapshots)*len(metricSpecs))
	for metricKey, targets := range byMetric {
		agentIDs := make([]string, 0, len(targets))
		maxFor := 0
		for _, item := range targets {
			agentIDs = append(agentIDs, item.agentID)
			if int(item.rule.ForSeconds) > maxFor {
				maxFor = int(item.rule.ForSeconds)
			}
		}
		// 窗口 = 判定期 + 1 倍余量：余量用于算出「已经持续多久」，也让占比判据更稳。
		window := time.Duration(maxFor) * 2 * time.Second
		if window <= 0 {
			window = 10 * time.Minute
		}
		spec := QuerySpec{MetricKey: metricKey, AgentIDs: agentIDs, Window: window, Step: s.opts.EvaluateInterval}
		windows, queryErr := s.querier.QueryWindow(ctx, spec)
		if queryErr != nil {
			// 时序库不可用：本轮整体跳过该指标，保持上一次状态。
			// 关键点：绝不能因为查询失败就把在告警的主机判成恢复。
			s.logger.Warn("资源告警时序查询失败，本轮跳过", "metric", metricKey, "error", queryErr)
			continue
		}
		ruleOf := make(map[string]EffectiveRule, len(targets))
		coresOf := make(map[string]float64, len(targets))
		for _, item := range targets {
			ruleOf[item.agentID] = item.rule
			coresOf[item.agentID] = float64(item.snapshot.LogicalCPUCores)
		}
		for _, window := range windows {
			rule, ok := ruleOf[window.AgentID]
			if !ok {
				continue
			}
			if metricKey == MetricDiskUsage && !diskTargetAllowed(rule, window) {
				continue
			}
			verdicts = append(verdicts, Judge(window, rule, coresOf[window.AgentID], spec))
		}
	}

	// 不需要在这里判断「已确认则跳过」：人工确认（Service.Acknowledge）已经把状态机复位为
	// 「正常 + 冷却中」，该去重键此后要么在冷却期内静默，要么在冷却期后输出 fired
	// ——一条首次触发时间与重申计数都重新计算的全新告警。
	for _, verdict := range verdicts {
		s.applyVerdict(ctx, verdict, snapshots[verdict.AgentID], verdicts, now)
	}
	return nil
}

// applyVerdict 对单个判定推进状态机并输出告警。
//
// 参数 Parameters:
//   - ctx (context.Context): 上下文。
//   - verdict (Verdict): 判定结果。
//   - snapshot (AgentSnapshot): 主机快照。
//   - all ([]Verdict): 本轮全部判定（用于汇总关联指标）。
//   - now (time.Time): 当前时间。
func (s *Service) applyVerdict(ctx context.Context, verdict Verdict, snapshot AgentSnapshot, all []Verdict, now time.Time) {
	event, since := s.machine.Apply(verdict.DedupKey(), verdict.Level, verdict.Rule, now)
	if event == "" {
		return
	}
	// 维护窗口内静默：只推进状态机，不打扰。状态仍会更新，避免窗口结束后误报恢复。
	silenced, err := s.store.IsSilenced(verdict.AgentID, now)
	if err != nil {
		s.logger.Warn("查询维护窗口失败", "agent_id", verdict.AgentID, "error", err)
	}
	if silenced {
		s.logger.Info("维护窗口内静默资源告警",
			"agent_id", verdict.AgentID, "metric", verdict.MetricKey, "event", event)
		return
	}

	attribution := s.attributor.ProcessTop(ctx, verdict.AgentID, verdict.MetricKey, snapshot.MemoryTotalBytes)
	systemContext := s.attributor.SystemContext(ctx, snapshot)
	var cpuDetail *CPUDetail
	if verdict.MetricKey == MetricCPUUsage {
		detail := s.attributor.CPUDetail(ctx, verdict.AgentID)
		cpuDetail = &detail
	}
	if event == EventRecovered {
		// 恢复通知不需要归因：它只说明「已经好了」。
		attribution = nil
		cpuDetail = nil
	}
	report := buildReport(reportInput{
		Verdict: verdict, Event: event, Since: since, Snapshot: snapshot,
		Attribution: attribution, Context: systemContext, CPUDetail: cpuDetail,
		Correlated: correlatedFor(verdict, all), Now: now,
	})

	if err := s.writer.Write(report); err != nil {
		s.logger.Error("写入资源告警日志失败", "agent_id", verdict.AgentID, "metric", verdict.MetricKey, "error", err)
	}
	if err := s.persist(report, verdict, since, now); err != nil {
		s.logger.Error("保存资源告警失败", "agent_id", verdict.AgentID, "metric", verdict.MetricKey, "error", err)
	}
	if s.events != nil {
		s.events.Publish(alertSSETopic, report)
	}
	s.logger.Info("资源告警已输出",
		"agent_id", verdict.AgentID, "metric", verdict.MetricKey, "event", event,
		"level", verdict.Level, "value", verdict.Value, "source", verdict.Rule.Source)
}

// persist 把告警写入 alerts 表并追加流转事件。
//
// 参数 Parameters:
//   - report (Report): 告警报告。
//   - verdict (Verdict): 判定结果。
//   - since (time.Time): 进入当前级别的时间。
//   - now (time.Time): 当前时间。
//
// 返回 Returns:
//   - err (error): 写入失败时返回非 nil。
func (s *Service) persist(report Report, verdict Verdict, since, now time.Time) error {
	details, err := json.Marshal(report)
	if err != nil {
		details = []byte("{}")
	}
	state := verdict.Level
	var (
		recoveredAt *time.Time
		closedAt    *time.Time
		closeReason string
	)
	if report.Event == EventRecovered {
		// 指标自动回落 = 这条告警自动结束：写结束时间与原因。
		// 之后若再次超标，会在判定期过后产生一条全新告警（首次触发，而非重申）。
		state = LevelOK
		recovered := now
		recoveredAt = &recovered
		closedAt = &recovered
		closeReason = CloseReasonRecovered
	}
	seen := now
	_, err = s.store.UpsertAlert(AlertStateUpdate{
		Alert: Alert{
			RuleID:       verdict.Rule.ID,
			AgentID:      verdict.AgentID,
			TargetID:     verdict.Target,
			DedupKey:     verdict.DedupKey(),
			State:        state,
			Level:        verdict.Level,
			MetricKey:    verdict.MetricKey,
			CurrentValue: verdict.Value,
			FirstFiredAt: since,
			RecoveredAt:  recoveredAt,
			ClosedAt:     closedAt,
			CloseReason:  closeReason,
			DetailsJSON:  string(details),
			LastSeenAt:   &seen,
		},
		Event:   report.Event,
		Message: report.Text(),
	})
	return err
}

// reportInput 是告警报告的组装输入。
type reportInput struct {
	// Verdict 判定结果。
	Verdict Verdict
	// Event 事件类型。
	Event string
	// Since 进入当前级别的时间。
	Since time.Time
	// Snapshot 主机快照。
	Snapshot AgentSnapshot
	// Attribution 进程归因。
	Attribution []Attribution
	// Context 系统上下文。
	Context SystemContext
	// CPUDetail CPU 明细。
	CPUDetail *CPUDetail
	// Correlated 同机其它指标状态。
	Correlated []string
	// Now 事件时间。
	Now time.Time
}

// buildReport 组装告警报告。
//
// 参数 Parameters:
//   - in (reportInput): 组装输入。
//
// 返回 Returns:
//   - report (Report): 告警报告。
func buildReport(in reportInput) Report {
	spec, _ := MetricSpecOf(in.Verdict.MetricKey)
	report := Report{
		Time:  in.Now,
		Event: in.Event,
		Level: in.Verdict.Level,
		Agent: AgentRef{
			ID: in.Verdict.AgentID, Hostname: in.Snapshot.Hostname,
			IP: in.Snapshot.CurrentIP, Role: in.Snapshot.Role,
		},
		Metric: MetricPart{
			Key: spec.Key, Name: spec.Name, Unit: spec.Unit, Explain: spec.Explain,
			Value: in.Verdict.Value, Warning: in.Verdict.Rule.WarningThreshold,
			Critical: in.Verdict.Rule.CriticalThreshold, Threshold: in.Verdict.Threshold,
			Target: in.Verdict.Target, WindowSeconds: int64(in.Verdict.Window.Seconds()),
			SustainedSeconds: in.Verdict.SustainedSeconds, BreachPoints: in.Verdict.BreachPoints,
			TotalPoints: in.Verdict.TotalPoints, BreachRatio: in.Verdict.BreachRatio,
		},
		RuleSource:  in.Verdict.Rule.Source,
		DedupKey:    in.Verdict.DedupKey(),
		CPUDetail:   in.CPUDetail,
		Attribution: in.Attribution,
		Context:     in.Context,
		Correlated:  in.Correlated,
	}
	report.Message = fmt.Sprintf("%s %s %.1f%s（%s，已持续 %s）",
		report.hostLabel(), spec.Name, in.Verdict.Value, spec.Unit,
		report.eventLabel(), formatDuration(time.Duration(in.Verdict.SustainedSeconds)*time.Second))
	return report
}

// correlatedFor 汇总同机其它指标的当前状态。
//
// 同一台机器四项指标一起超标，往往说明是整机性问题（如内存不足引发 swap 抖动），
// 这个字段能让看日志的人立刻分辨「单点问题」还是「整机问题」。
//
// 参数 Parameters:
//   - verdict (Verdict): 当前判定。
//   - all ([]Verdict): 本轮全部判定。
//
// 返回 Returns:
//   - items ([]string): 关联指标描述。
func correlatedFor(verdict Verdict, all []Verdict) []string {
	items := make([]string, 0, len(all))
	for _, other := range all {
		if other.AgentID != verdict.AgentID || other.MetricKey == verdict.MetricKey {
			continue
		}
		name := other.MetricKey
		if spec, ok := MetricSpecOf(other.MetricKey); ok {
			name = spec.Name
		}
		state := "正常"
		switch other.Level {
		case LevelWarning:
			state = fmt.Sprintf("警告 %.1f%s", other.Value, other.Unit)
		case LevelCritical:
			state = fmt.Sprintf("严重 %.1f%s", other.Value, other.Unit)
		}
		items = append(items, fmt.Sprintf("%s%s %s", name, other.Target, state))
	}
	sort.Strings(items)
	return items
}

// diskTargetAllowed 判断某个磁盘挂载点是否参与判定。
//
// 参数 Parameters:
//   - rule (EffectiveRule): 生效规则（含扩展参数）。
//   - window (MetricWindow): 窗口（含挂载点与文件系统类型）。
//
// 返回 Returns:
//   - allowed (bool): 是否参与判定。
func diskTargetAllowed(rule EffectiveRule, window MetricWindow) bool {
	params := ParseParams(rule.ParamsJSON)
	if len(params.IncludeMounts) > 0 {
		matched := false
		for _, mount := range params.IncludeMounts {
			if strings.EqualFold(strings.TrimSpace(mount), window.Target) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	fstype := strings.TrimSpace(window.FSType)
	if fstype == "" {
		return true
	}
	for _, ignored := range EffectiveIgnoreFSTypes(params) {
		if strings.EqualFold(strings.TrimSpace(ignored), fstype) {
			return false
		}
	}
	return true
}
