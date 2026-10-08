package evaluator

import (
	"context"
	"log/slog"
	"time"

	"monitor-server/internal/state"
)

type AlertEvaluator struct {
	store  *state.Store
	logger *slog.Logger
}

func NewAlertEvaluator(store *state.Store, logger *slog.Logger) *AlertEvaluator {
	return &AlertEvaluator{store: store, logger: logger}
}

// The first evaluator rule is host offline. Metric threshold rules are added
// after the query layer is available; all alerts still use one persisted model.
func (e *AlertEvaluator) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			agents, err := e.store.ListAgents()
			if err != nil {
				e.logger.Error("list agents for alerts", "error", err)
				continue
			}
			for _, agent := range agents {
				if agent.OnlineState == "offline" {
					now := time.Now().UTC()
					_, err := e.store.UpsertOfflineAlert(agent.ID, now)
					if err != nil {
						e.logger.Error("upsert offline alert", "agent_id", agent.ID, "error", err)
						continue
					}
					_ = e.store.Publish("alerts", agent.ID, map[string]any{
						"rule": "host_offline", "agent_id": agent.ID,
						"state": "critical", "at": now,
					})
				}
			}
		}
	}
}
