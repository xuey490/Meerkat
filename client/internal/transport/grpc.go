package transport

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"monitor-client/internal/config"
	"monitor-client/internal/spool"
	"monitor-client/internal/systeminfo"
	"monitor-client/internal/telegraf"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	collectorv1 "monitor-client/collector/v1"
)

var bootID = newBootID()
var backlogMu sync.Mutex
var backlogRateBytesPerSecond int64 = 1 << 20
var backlogTokens float64
var backlogLastRefill time.Time

func BootID() string { return bootID }

func Run(ctx context.Context, cfg config.Config, store *spool.Store, health *telegraf.Monitor, system *systeminfo.Monitor) error {
	backoff := time.Second
	for ctx.Err() == nil {
		err := runSession(ctx, cfg, store, health, system)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			log.Printf("collector %s: %v; retry in %s", cfg.ServerAddress, err, backoff)
			jitter := time.Duration(time.Now().UnixNano()%int64(backoff/2+1)) - backoff/4
			timer := time.NewTimer(backoff + jitter)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
			backoff *= 2
			if backoff > cfg.ReconnectMax {
				backoff = cfg.ReconnectMax
			}
			continue
		}
		backoff = time.Second
	}
	return nil
}

func runSession(ctx context.Context, cfg config.Config, store *spool.Store, health *telegraf.Monitor, system *systeminfo.Monitor) error {
	creds, err := clientCredentials(cfg)
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(cfg.ServerAddress, grpc.WithTransportCredentials(creds))
	if err != nil {
		return err
	}
	defer conn.Close()
	stream, err := collectorv1.NewCollectorServiceClient(conn).Connect(ctx)
	if err != nil {
		return err
	}
	hostname, _ := os.Hostname()
	reportIP := cfg.ReportIP
	if reportIP == "" {
		reportIP = systeminfo.PrimaryIPv4()
	}
	if err := sendAndReceive(stream, &collectorv1.AgentMessage{
		Payload: &collectorv1.AgentMessage_Register{Register: &collectorv1.Register{
			AgentId: cfg.AgentID, BootId: bootID, Hostname: hostname,
			IpAddress: reportIP,
			Os:        runtime.GOOS, Architecture: runtime.GOARCH,
			AgentVersion: cfg.AgentVersion, ConfigVersion: cfg.ConfigVersion,
			Labels: cfg.Labels,
		}},
	}); err != nil {
		return err
	}
	ticker := time.NewTicker(cfg.HeartbeatInterval)
	defer ticker.Stop()
	sentBatches := 0
	lastSystemDetailsAt := time.Time{}
	for {
		if batch, err := store.NextDue(time.Now().UTC()); err != nil {
			return err
		} else if batch != nil && sentBatches < cfg.MaxBacklogBatches {
			window := []*spool.Batch{batch}
			for len(window) < cfg.MaxBacklogParallel && len(window) < cfg.MaxBacklogBatches {
				next, nextErr := store.NextDueAfter(time.Now().UTC(), window[len(window)-1].ID)
				if nextErr != nil || next == nil {
					break
				}
				window = append(window, next)
			}
			for _, item := range window {
				if err := waitBacklog(ctx, len(item.Payload), cfg.BacklogRateBytes); err != nil {
					return err
				}
				if err := stream.Send(metricMessage(cfg.AgentID, item)); err != nil {
					for _, retry := range window {
						_ = store.Retry(retry.ID, cfg.RetryBase, cfg.RetryMax)
					}
					return err
				}
			}
			for _, item := range window {
				if err := receiveResponse(stream); err != nil {
					for _, retry := range window {
						_ = store.Retry(retry.ID, cfg.RetryBase, cfg.RetryMax)
					}
					return err
				}
				if err := store.Ack(item.ID); err != nil {
					return err
				}
			}
			sentBatches += len(window)
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			sentBatches = 0
			stats, statsErr := store.StatsDetailed()
			if statsErr != nil {
				return statsErr
			}
			var healthState telegraf.Snapshot
			if health != nil {
				healthState = health.Snapshot()
			}
			var systemState systeminfo.Snapshot
			if system != nil {
				systemState = system.Snapshot()
			}
			processes := processMessages(systemState.Processes)
			temperatures := temperatureMessages(systemState.Temperatures)
			if !lastSystemDetailsAt.IsZero() && time.Since(lastSystemDetailsAt) < 30*time.Second {
				processes = nil
				temperatures = nil
			} else {
				lastSystemDetailsAt = time.Now()
			}
			if err := sendAndReceive(stream, &collectorv1.AgentMessage{
				Payload: &collectorv1.AgentMessage_Heartbeat{Heartbeat: &collectorv1.Heartbeat{
					AgentId: cfg.AgentID, BootId: bootID,
					SentAtUnixNano: time.Now().UTC().UnixNano(), AgentStartedAtUnixNano: startedAt.UnixNano(),
					TelegrafState:   map[bool]string{true: "healthy", false: "down"}[healthState.Running],
					TelegrafVersion: healthState.Version, TelegrafRunning: healthState.Running,
					TelegrafPid: healthState.PID, TelegrafLastMetricUnixNano: healthState.LastMetricAt.UnixNano(),
					QueueBytes: uint64(stats.Bytes), DroppedMetricBatches: stats.Dropped,
					QueueCapacityBytes: uint64(stats.Capacity), QueueUsagePercent: stats.Usage,
					QueueAlertLevel: stats.AlertLevel, AgentMemoryBytes: runtimeMemory(),
					AgentGoroutines: uint64(runtime.NumGoroutine()),
					OsVersion:       systemState.OSVersion, SystemTimeUnixNano: systemState.SystemTime.UnixNano(),
					MemoryTotalBytes: systemState.MemoryTotalBytes, MemoryUsedBytes: systemState.MemoryUsedBytes,
					ProcessCount: systemState.ProcessCount, ListeningPortCount: systemState.ListeningPortCount,
					Load1: systemState.Load1, SystemUptimeSeconds: systemState.UptimeSeconds,
					CpuModel: systemState.CPUModel, PhysicalCpuCores: systemState.PhysicalCPUCores,
					LogicalCpuCores:      systemState.LogicalCPUCores,
					HardwareTemperatureC: systemState.HardwareTempC,
					Processes:            processes,
					Temperatures:         temperatures,
				}},
			}); err != nil {
				return err
			}
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func processMessages(processes []systeminfo.Process) []*collectorv1.ProcessInfo {
	result := make([]*collectorv1.ProcessInfo, 0, len(processes))
	for _, process := range processes {
		result = append(result, &collectorv1.ProcessInfo{
			Pid: int32(process.PID), Name: process.Name, MemoryBytes: process.Memory,
		})
	}
	return result
}

func temperatureMessages(temperatures []systeminfo.Temperature) []*collectorv1.TemperatureReading {
	result := make([]*collectorv1.TemperatureReading, 0, len(temperatures))
	for _, temperature := range temperatures {
		result = append(result, &collectorv1.TemperatureReading{
			Name: temperature.Name, Celsius: temperature.Celsius,
		})
	}
	return result
}

func sendAndReceive(stream grpc.BidiStreamingClient[collectorv1.AgentMessage, collectorv1.ServerMessage], request *collectorv1.AgentMessage) error {
	if err := stream.Send(request); err != nil {
		return err
	}
	return receiveResponse(stream)
}

func receiveResponse(stream grpc.BidiStreamingClient[collectorv1.AgentMessage, collectorv1.ServerMessage]) error {
	response, err := stream.Recv()
	if err != nil {
		return err
	}
	if rate := response.GetBacklogMaxBytesPerSecond(); rate > 0 {
		backlogMu.Lock()
		backlogRateBytesPerSecond = int64(rate)
		backlogMu.Unlock()
	}
	return nil
}

func metricMessage(agentID string, batch *spool.Batch) *collectorv1.AgentMessage {
	boot := batch.BootID
	if boot == "" {
		boot = bootID
	}
	return &collectorv1.AgentMessage{
		Payload: &collectorv1.AgentMessage_Metrics{Metrics: &collectorv1.MetricBatch{
			AgentId: agentID, BootId: boot, Sequence: batch.Sequence,
			CollectedAtUnixNano: batch.CreatedAt.UnixNano(),
			GzipLineProtocol:    batch.Payload,
		}},
	}
}

func waitBacklog(ctx context.Context, size int, rate int64) error {
	if rate <= 0 {
		rate = 1 << 20
	}
	backlogMu.Lock()
	effectiveRate := rate
	if backlogRateBytesPerSecond > 0 {
		effectiveRate = backlogRateBytesPerSecond
	}
	if effectiveRate <= 0 {
		effectiveRate = 1 << 20
	}
	now := time.Now()
	if backlogLastRefill.IsZero() {
		backlogLastRefill = now
		backlogTokens = float64(effectiveRate)
	}
	elapsed := now.Sub(backlogLastRefill).Seconds()
	backlogTokens += elapsed * float64(effectiveRate)
	burst := float64(effectiveRate)
	if backlogTokens > burst {
		backlogTokens = burst
	}
	backlogLastRefill = now
	waitSeconds := 0.0
	if backlogTokens < float64(size) {
		waitSeconds = (float64(size) - backlogTokens) / float64(effectiveRate)
		backlogTokens = 0
		backlogLastRefill = now.Add(time.Duration(waitSeconds * float64(time.Second)))
	} else {
		backlogTokens -= float64(size)
	}
	target := now.Add(time.Duration(waitSeconds * float64(time.Second)))
	backlogMu.Unlock()
	timer := time.NewTimer(time.Until(target))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

var startedAt = time.Now().UTC()

func runtimeMemory() uint64 {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.Alloc
}

func newBootID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}

func clientCredentials(cfg config.Config) (credentials.TransportCredentials, error) {
	if cfg.InsecureTLS {
		return insecure.NewCredentials(), nil
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse CA certificate")
		}
		tlsConfig.RootCAs = pool
	}
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return credentials.NewTLS(tlsConfig), nil
}
