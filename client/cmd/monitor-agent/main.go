package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"monitor-client/internal/config"
	"monitor-client/internal/receiver"
	"monitor-client/internal/spool"
	"monitor-client/internal/systeminfo"
	"monitor-client/internal/telegraf"
	"monitor-client/internal/transport"
)

func main() {
	configPath := flag.String("config", "configs/agent.yaml", "path to agent YAML configuration")
	unattended := flag.Bool("u", false, "Windows: hide the console and continue as a background process")
	flag.Parse()
	if err := detachIfRequested(*unattended); err != nil {
		log.Fatal(err)
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	store, err := spool.OpenWithIdentity(cfg.StateDirectory, cfg.MaxSpoolBytes, cfg.AgentID, transport.BootID())
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	store.SetMaxAge(cfg.MaxSpoolAge)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	telegrafMonitor := telegraf.New(telegraf.Options{
		BinaryPath: cfg.TelegrafBinary, ServiceName: cfg.TelegrafService,
		CheckEvery: cfg.TelegrafCheck, AutoRecover: cfg.AutoRecoverTelegraf,
	})
	systemMonitor := systeminfo.New()
	systemMonitor.Refresh(ctx)
	printAgentReady(cfg)
	printSystemSummary(systemMonitor.Snapshot())
	go telegrafMonitor.Run(ctx)
	go systemMonitor.Run(ctx)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if err := store.EnforceLimits(now.UTC()); err != nil {
					log.Printf("spool maintenance failed: %v", err)
				}
			}
		}
	}()
	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           receiver.NewWithMonitor(store, telegrafMonitor, cfg.AgentID),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
	}
	healthServer := &http.Server{Addr: cfg.HealthAddress, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stats, statsErr := store.StatsDetailed()
		health := telegrafMonitor.Snapshot()
		system := systemMonitor.Snapshot()
		if r.URL.Path != "/healthz" && r.URL.Path != "/readyz" {
			http.NotFound(w, r)
			return
		}
		status := http.StatusOK
		if r.URL.Path == "/readyz" && statsErr != nil {
			status = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":         map[bool]string{true: "ok", false: "degraded"}[statsErr == nil],
			"agent_id":       cfg.AgentID,
			"uptime_seconds": time.Since(startedAt).Seconds(),
			"goroutines":     runtime.NumGoroutine(),
			"memory_bytes":   runtimeMem(),
			"telegraf":       health,
			"system":         system,
			"queue":          stats,
		})
	})}
	go func() {
		colorLog("\x1b[36m", "accepting Telegraf metrics on "+cfg.ListenAddress)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Printf("metrics receiver stopped: %v", err)
			stop()
		}
	}()
	go func() {
		if err := transport.Run(ctx, cfg, store, telegrafMonitor, systemMonitor); err != nil {
			log.Printf("gRPC transport stopped: %v", err)
			stop()
		}
	}()
	go func() {
		colorLog("\x1b[35m", "health endpoint listening on "+cfg.HealthAddress)
		if err := healthServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Printf("health endpoint stopped: %v", err)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	_ = healthServer.Shutdown(shutdownCtx)
}

var startedAt = time.Now().UTC()

func colorLog(color, message string) {
	fmt.Printf("%s[%s] %s\x1b[0m\n", color, time.Now().Format("2006-01-02 15:04:05"), message)
}

func printAgentReady(cfg config.Config) {
	hostname, _ := os.Hostname()
	reportIP := cfg.ReportIP
	if reportIP == "" {
		reportIP = systeminfo.PrimaryIPv4()
	}
	fmt.Print("\x1b]0;Monitor Agent\x07")
	colorLog("\x1b[93m", "agent端启动成功")
	colorLog("\x1b[93m", fmt.Sprintf("agent_id=%s  hostname=%s  ip=%s  os=%s/%s  metrics=%s  health=%s",
		cfg.AgentID, hostname, reportIP, runtime.GOOS, runtime.GOARCH, cfg.ListenAddress, cfg.HealthAddress))
}

func printSystemSummary(snapshot systeminfo.Snapshot) {
	colorLog("\x1b[32m", fmt.Sprintf(
		"agent %s | time=%s | memory=%s/%s | processes=%d | listening_ports=%d | load1=%.2f",
		snapshot.OSVersion,
		snapshot.SystemTime.Format(time.RFC3339),
		formatBytes(snapshot.MemoryUsedBytes),
		formatBytes(snapshot.MemoryTotalBytes),
		snapshot.ProcessCount,
		snapshot.ListeningPortCount,
		snapshot.Load1,
	))
}

func formatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := uint64(unit), 0
	for value >= div*unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}

func runtimeMem() uint64 {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.Alloc
}
