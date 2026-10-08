package collector

import (
	"fmt"
	"log/slog"
	"time"

	collectorv1 "monitor-server/api/proto/collector/v1"
	"monitor-server/internal/state"

	"google.golang.org/grpc"
)

type Service struct {
	collectorv1.UnimplementedCollectorServiceServer
	store  *state.Store
	logger *slog.Logger
}

func New(store *state.Store, logger *slog.Logger) *Service {
	return &Service{store: store, logger: logger}
}

func (s *Service) Register(server *grpc.Server) {
	collectorv1.RegisterCollectorServiceServer(server, s)
}

func (s *Service) Connect(stream grpc.BidiStreamingServer[collectorv1.AgentMessage, collectorv1.ServerMessage]) error {
	var agentID string
	for {
		message, err := stream.Recv()
		if err != nil {
			s.logger.Warn("agent stream closed", "agent_id", agentID, "error", err)
			return err
		}
		if current := messageAgentID(message); current != "" {
			agentID = current
		}
		if err := s.handle(message); err != nil {
			s.logger.Warn("reject agent message", "error", err)
			return err
		}
		if err := stream.Send(&collectorv1.ServerMessage{
			ConfigVersion:            "local-1",
			BacklogMaxBytesPerSecond: 1 << 20,
			BacklogMaxBatches:        10,
			BacklogMaxParallel:       1,
		}); err != nil {
			return err
		}
		printAcceptedMessage(message)
	}
}

func printAcceptedMessage(message *collectorv1.AgentMessage) {
	const reset = "\x1b[0m"
	color := "\x1b[37m"
	detail := "unknown"
	switch payload := message.GetPayload().(type) {
	case *collectorv1.AgentMessage_Register:
		color = "\x1b[32m"
		detail = fmt.Sprintf("REGISTER host=%s ip=%s os=%s/%s agent=%s labels=%v",
			payload.Register.GetHostname(), payload.Register.GetIpAddress(),
			payload.Register.GetOs(),
			payload.Register.GetArchitecture(), payload.Register.GetAgentVersion(),
			payload.Register.GetLabels())
	case *collectorv1.AgentMessage_Heartbeat:
		color = "\x1b[36m"
		heartbeat := payload.Heartbeat
		detail = fmt.Sprintf(
			"HEARTBEAT agent=%s os=%s time=%s memory=%s/%s processes=%d ports=%d telegraf=%s/%s queue=%s %.1f%%",
			heartbeat.GetAgentId(), heartbeat.GetOsVersion(),
			time.Unix(0, heartbeat.GetSystemTimeUnixNano()).UTC().Format(time.RFC3339),
			formatBytes(heartbeat.GetMemoryUsedBytes()), formatBytes(heartbeat.GetMemoryTotalBytes()),
			heartbeat.GetProcessCount(), heartbeat.GetListeningPortCount(),
			heartbeat.GetTelegrafState(), heartbeat.GetTelegrafVersion(),
			heartbeat.GetQueueAlertLevel(), heartbeat.GetQueueUsagePercent())
	case *collectorv1.AgentMessage_Metrics:
		color = "\x1b[33m"
		metrics := payload.Metrics
		detail = fmt.Sprintf("METRICS agent=%s sequence=%d collected=%s bytes=%d",
			metrics.GetAgentId(), metrics.GetSequence(),
			time.Unix(0, metrics.GetCollectedAtUnixNano()).UTC().Format(time.RFC3339),
			len(metrics.GetGzipLineProtocol()))
	case *collectorv1.AgentMessage_Event:
		color = "\x1b[35m"
		detail = fmt.Sprintf("EVENT agent=%s type=%s", payload.Event.GetAgentId(), payload.Event.GetEventType())
	}
	fmt.Printf("%s[%s] accepted %s%s\n", color, time.Now().Format("2006-01-02 15:04:05"), detail, reset)
}

func formatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := uint64(unit), 0
	for value >= div*unit && exp < 6 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}

func (s *Service) handle(message *collectorv1.AgentMessage) error {
	switch payload := message.GetPayload().(type) {
	case *collectorv1.AgentMessage_Register:
		register := payload.Register
		return s.store.UpsertAgent(state.AgentSnapshot{
			AgentID: register.GetAgentId(), BootID: register.GetBootId(),
			Hostname: register.GetHostname(), CurrentIP: register.GetIpAddress(),
			OS: register.GetOs(), Architecture: register.GetArchitecture(),
			AgentVersion: register.GetAgentVersion(), TelegrafVersion: register.GetTelegrafVersion(),
			ConfigVersion: register.GetConfigVersion(), Labels: register.GetLabels(),
			Environment: register.GetLabels()["environment"],
			Site:        register.GetLabels()["site"], Role: register.GetLabels()["role"],
			OnlineState: "online", TelegrafState: "unknown",
		}, time.Now().UTC())
	case *collectorv1.AgentMessage_Heartbeat:
		heartbeat := payload.Heartbeat
		if err := s.store.UpdateHeartbeatDetails(heartbeat.GetAgentId(), heartbeat.GetBootId(),
			heartbeat.GetSequence(), heartbeat.GetQueueBytes(),
			heartbeat.GetDroppedMetricBatches(), heartbeat.GetTelegrafRunning(),
			heartbeat.GetTelegrafPid(), time.Unix(0, heartbeat.GetAgentStartedAtUnixNano()).UTC(),
			heartbeat.GetAgentMemoryBytes(), heartbeat.GetAgentGoroutines(),
			heartbeat.GetQueueCapacityBytes(), heartbeat.GetQueueUsagePercent(),
			heartbeat.GetQueueAlertLevel(), heartbeat.GetOsVersion(),
			time.Unix(0, heartbeat.GetSystemTimeUnixNano()).UTC(),
			heartbeat.GetMemoryTotalBytes(), heartbeat.GetMemoryUsedBytes(),
			heartbeat.GetProcessCount(), heartbeat.GetListeningPortCount(),
			heartbeat.GetLoad1(), heartbeat.GetSystemUptimeSeconds(),
			heartbeat.GetCpuModel(), heartbeat.GetPhysicalCpuCores(), heartbeat.GetLogicalCpuCores(),
			heartbeat.GetHardwareTemperatureC(), processSnapshots(heartbeat.GetProcesses()),
			temperatureSnapshots(heartbeat.GetTemperatures()),
			time.Now().UTC()); err != nil {
			return err
		}
		return s.store.UpdateTelegrafState(heartbeat.GetAgentId(),
			heartbeat.GetTelegrafState(), heartbeat.GetTelegrafVersion(),
			time.Unix(0, heartbeat.GetTelegrafLastMetricUnixNano()).UTC())
	case *collectorv1.AgentMessage_Metrics:
		return s.recordMetrics(payload.Metrics)
	case *collectorv1.AgentMessage_Event:
		event := payload.Event
		return s.store.Publish("agent_events", event.GetAgentId(), event)
	default:
		return fmt.Errorf("empty AgentMessage payload")
	}
}

func processSnapshots(processes []*collectorv1.ProcessInfo) []state.ProcessSnapshot {
	result := make([]state.ProcessSnapshot, 0, len(processes))
	for _, process := range processes {
		if process == nil {
			continue
		}
		result = append(result, state.ProcessSnapshot{
			PID: int(process.GetPid()), Name: process.GetName(), MemoryBytes: process.GetMemoryBytes(),
		})
	}
	return result
}

func temperatureSnapshots(temperatures []*collectorv1.TemperatureReading) []state.TemperatureSnapshot {
	result := make([]state.TemperatureSnapshot, 0, len(temperatures))
	for _, temperature := range temperatures {
		if temperature == nil {
			continue
		}
		result = append(result, state.TemperatureSnapshot{
			Name: temperature.GetName(), Celsius: temperature.GetCelsius(),
		})
	}
	return result
}

func (s *Service) recordMetrics(batch *collectorv1.MetricBatch) error {
	if batch.GetAgentId() == "" || batch.GetBootId() == "" || batch.GetSequence() == 0 {
		return fmt.Errorf("agent_id, boot_id, and sequence are required")
	}
	payload := batch.GetGzipLineProtocol()
	if len(payload) == 0 || len(payload) > 8<<20 {
		return fmt.Errorf("metric payload size is invalid")
	}
	claimed, err := s.store.ClaimMetric(batch.GetAgentId(), batch.GetSequence(), time.Now().UTC())
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	if err := s.store.Publish("metrics", batch.GetAgentId(), map[string]any{
		"agent_id": batch.GetAgentId(), "boot_id": batch.GetBootId(),
		"sequence": batch.GetSequence(), "payload": payload,
	}); err != nil {
		_ = s.store.ReleaseMetric(batch.GetAgentId(), batch.GetSequence())
		return err
	}
	return nil
}

func messageAgentID(message *collectorv1.AgentMessage) string {
	switch payload := message.GetPayload().(type) {
	case *collectorv1.AgentMessage_Register:
		return payload.Register.GetAgentId()
	case *collectorv1.AgentMessage_Heartbeat:
		return payload.Heartbeat.GetAgentId()
	case *collectorv1.AgentMessage_Metrics:
		return payload.Metrics.GetAgentId()
	case *collectorv1.AgentMessage_Event:
		return payload.Event.GetAgentId()
	default:
		return ""
	}
}
