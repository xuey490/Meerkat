package api

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"monitor-server/internal/state"
)

type Server struct {
	store *state.Store
}

func New(store *state.Store) *Server {
	return &Server{store: store}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/api/v1/agents", s.agents)
	mux.HandleFunc("/api/v1/agents/heartbeat", s.heartbeat)
	mux.HandleFunc("/api/v1/agents/", s.agentResource)
	mux.HandleFunc("/api/v1/alerts", s.alerts)
	mux.HandleFunc("/api/v1/rules", s.rules)
	mux.HandleFunc("/api/v1/maintenance", s.maintenance)
	mux.HandleFunc("/api/v1/audit", s.audit)
	return mux
}

func (s *Server) TLSConfig(base *tls.Config) *tls.Config {
	return base.Clone()
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now().UTC()})
}

func (s *Server) agents(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		agents, err := s.store.ListAgents()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, agents)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request state.AgentSnapshot
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	if request.AgentID == "" {
		writeErrorStatus(w, http.StatusBadRequest, errors.New("agent_id is required"))
		return
	}
	if err := s.store.UpsertAgent(request, time.Now().UTC()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"agent_id": request.AgentID})
}

func (s *Server) agentResource(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/agents/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	agentID := parts[0]
	if agentID == "enroll" && r.Method == http.MethodPost {
		s.agents(w, r)
		return
	}
	if len(parts) == 2 && parts[1] == "config" && r.Method == http.MethodGet {
		document, err := s.store.ActiveConfig(agentID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, document)
		return
	}
	if len(parts) == 2 && parts[1] == "services" && r.Method == http.MethodGet {
		probes, err := s.store.ListServiceProbes(agentID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, probes)
		return
	}
	if len(parts) == 2 && parts[1] == "containers" && r.Method == http.MethodGet {
		containers, err := s.store.ListContainers(agentID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, containers)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		if err := s.store.DeleteAgent(agentID); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodGet {
		agent, err := s.store.GetAgent(agentID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, agent)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

type heartbeatRequest struct {
	AgentID         string `json:"agent_id"`
	BootID          string `json:"boot_id"`
	Sequence        uint64 `json:"sequence"`
	QueueBytes      uint64 `json:"queue_bytes"`
	DroppedBatches  uint64 `json:"dropped_metric_batches"`
	TelegrafState   string `json:"telegraf_state"`
	TelegrafVersion string `json:"telegraf_version"`
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request heartbeatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	if request.AgentID == "" {
		writeErrorStatus(w, http.StatusBadRequest, errors.New("agent_id is required"))
		return
	}
	if err := s.store.UpdateHeartbeat(request.AgentID, request.BootID, request.Sequence,
		request.QueueBytes, request.DroppedBatches, time.Now().UTC()); err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.UpdateTelegrafState(request.AgentID, request.TelegrafState,
		request.TelegrafVersion, time.Time{}); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"config_version": "local-1"})
}

func (s *Server) alerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	alerts, err := s.store.ListAlerts()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, alerts)
}

func (s *Server) rules(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		rules, err := s.store.ListAlertRules()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, rules)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var rule state.AlertRule
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&rule); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.CreateAlertRule(&rule); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) maintenance(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		windows, err := s.store.ListMaintenance(time.Now().UTC())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, windows)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var window state.MaintenanceWindow
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&window); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.CreateMaintenance(&window); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, window)
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var record state.AuditRecord
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&record); err != nil {
		writeErrorStatus(w, http.StatusBadRequest, err)
		return
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if err := s.store.AddAudit(&record); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	writeErrorStatus(w, http.StatusInternalServerError, err)
}

func writeErrorStatus(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
