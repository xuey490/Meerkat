package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/company/monitor-webserver/internal/app"
)

func main() {
	configPath := flag.String("config", "configs/webserver.yaml", "path to webserver config")
	flag.Parse()
	cfg, err := app.LoadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	application, err := app.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer application.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go application.RunEventPublisher(ctx)

	server := &http.Server{Addr: cfg.HTTPAddress, Handler: application.Router()}
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()
	ln, err := net.Listen("tcp", cfg.HTTPAddress)
	if err != nil {
		log.Fatal(err)
	}
	application.PrintStartup()
	if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
