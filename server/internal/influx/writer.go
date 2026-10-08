package influx

import (
	"context"
	"fmt"
	"strings"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"

	"monitor-server/internal/config"
)

type Writer struct {
	client influxdb2.Client
	org    string
	bucket string
}

func New(cfg config.InfluxConfig) *Writer {
	return &Writer{
		client: influxdb2.NewClient(cfg.URL, cfg.Token),
		org:    cfg.Organization,
		bucket: cfg.Bucket,
	}
}

func (w *Writer) Close() {
	w.client.Close()
}

func (w *Writer) WriteLineProtocol(ctx context.Context, payload []byte) error {
	record := strings.TrimSpace(string(payload))
	if record == "" {
		return fmt.Errorf("empty line protocol payload")
	}
	if err := w.client.WriteAPIBlocking(w.org, w.bucket).WriteRecord(ctx, record); err != nil {
		return fmt.Errorf("write InfluxDB metrics: %w", err)
	}
	return nil
}
