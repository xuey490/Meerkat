package metricwriter

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"monitor-server/internal/influx"
	"monitor-server/internal/state"
)

type metricEvent struct {
	AgentID  string `json:"agent_id"`
	Sequence uint64 `json:"sequence"`
	Payload  []byte `json:"payload"`
}

type Worker struct {
	store  *state.Store
	writer *influx.Writer
	logger *slog.Logger
}

func New(store *state.Store, writer *influx.Writer, logger *slog.Logger) *Worker {
	return &Worker{store: store, writer: writer, logger: logger}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.consume(ctx)
		}
	}
}

func (w *Worker) consume(ctx context.Context) {
	events, err := w.store.ClaimEvents("metrics", 100)
	if err != nil {
		w.logger.Error("claim metric events", "error", err)
		return
	}
	if len(events) == 0 {
		return
	}
	acked := make([]uint, 0, len(events))
	for _, event := range events {
		var payload metricEvent
		if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
			w.logger.Error("decode metric event", "event_id", event.ID, "error", err)
			acked = append(acked, event.ID)
			continue
		}
		if err := w.writer.WriteLineProtocol(ctx, payload.Payload); err != nil {
			w.logger.Warn("write metric event", "event_id", event.ID, "error", err)
			if isPermanentWriteError(err) {
				// Schema conflicts cannot succeed on retry; acknowledge the
				// poison event so it does not block newer metrics forever.
				acked = append(acked, event.ID)
			} else {
				_ = w.store.ReleaseEvents([]uint{event.ID})
			}
			continue
		}
		acked = append(acked, event.ID)
	}
	if err := w.store.AckEvents(acked); err != nil {
		w.logger.Error("ack metric events", "error", err)
	}
}

func isPermanentWriteError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "field type conflict") ||
		strings.Contains(message, "unprocessable entity")
}
