package evaluator

import (
	"context"
	"log/slog"
	"time"

	"monitor-server/internal/state"
)

type StateEvaluator struct {
	store  *state.Store
	logger *slog.Logger
}

func NewStateEvaluator(store *state.Store, logger *slog.Logger) *StateEvaluator {
	return &StateEvaluator{store: store, logger: logger}
}

func (e *StateEvaluator) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := e.store.EvaluateOnlineStates(now.UTC()); err != nil {
				e.logger.Error("evaluate agent states", "error", err)
			}
		}
	}
}
