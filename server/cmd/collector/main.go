package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"monitor-server/internal/api"
	"monitor-server/internal/collector"
	"monitor-server/internal/config"
	"monitor-server/internal/evaluator"
	"monitor-server/internal/influx"
	"monitor-server/internal/metricwriter"
	"monitor-server/internal/state"
	"monitor-server/internal/systeminfo"
	"monitor-server/internal/transport"
)

func main() {
	configPath := flag.String("config", "configs/server.local.yaml", "path to server YAML configuration")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load configuration", "error", err)
		os.Exit(1)
	}
	printServerSummary(systeminfo.Collect(context.Background()))
	store, err := state.Open(cfg.Postgres)
	if err != nil {
		slog.Error("open PostgreSQL", "error", err)
		os.Exit(1)
	}
	writer := influx.New(cfg.Influx)
	defer writer.Close()
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	go metricwriter.New(store, writer, slog.Default()).Run(workerCtx)
	go evaluator.NewStateEvaluator(store, slog.Default()).Run(workerCtx)
	go evaluator.NewAlertEvaluator(store, slog.Default()).Run(workerCtx)

	tlsConfig, err := transport.ServerTLS(cfg.TLS)
	if err != nil {
		slog.Error("configure mTLS", "error", err)
		os.Exit(1)
	}
	listener, err := net.Listen("tcp", cfg.GRPCAddress)
	if err != nil {
		slog.Error("listen for gRPC", "address", cfg.GRPCAddress, "error", err)
		os.Exit(1)
	}

	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)))
	collector.New(store, slog.Default()).Register(server)

	go func() {
		colorLog("\x1b[36m", "gRPC Collector listening address="+cfg.GRPCAddress)
		if err := server.Serve(listener); err != nil {
			slog.Error("gRPC Collector stopped", "error", err)
		}
	}()
	httpServer := &http.Server{Addr: ":8080", Handler: api.New(store).Handler()}
	go func() {
		colorLog("\x1b[35m", "HTTPS management API listening address="+httpServer.Addr)
		if err := httpServer.ListenAndServeTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTPS management API stopped", "error", err)
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
	slog.Info("shutting down Collector")
	_ = httpServer.Shutdown(context.Background())
	server.GracefulStop()
	if err := listener.Close(); err != nil {
		slog.Warn("close listener", "error", err)
	}
	fmt.Println("collector stopped")
}

func colorLog(color, message string) {
	fmt.Printf("%s[%s] %s\x1b[0m\n", color, time.Now().Format("2006-01-02 15:04:05"), message)
}

func printServerSummary(snapshot systeminfo.Snapshot) {
	colorLog("\x1b[32m", fmt.Sprintf(
		"server system %s | time=%s | memory=%s/%s | processes=%d | listening_ports=%d",
		snapshot.OSVersion,
		snapshot.SystemTime.Format(time.RFC3339),
		formatBytes(snapshot.MemoryUsedBytes),
		formatBytes(snapshot.MemoryTotalBytes),
		snapshot.ProcessCount,
		snapshot.ListeningPortCount,
	))
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
