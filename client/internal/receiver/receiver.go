package receiver

import (
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"monitor-client/internal/spool"
	"monitor-client/internal/telegraf"
)

type Handler struct {
	store   *spool.Store
	seq     atomic.Uint64
	health  *telegraf.Monitor
	agentID string
}

func New(store *spool.Store) *Handler {
	return NewWithMonitor(store, nil, "")
}

func NewWithMonitor(store *spool.Store, health *telegraf.Monitor, agentID string) *Handler {
	handler := &Handler{store: store, health: health, agentID: agentID}
	if sequence, err := store.LastSequence(); err == nil {
		handler.seq.Store(sequence)
	}
	return handler
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/telegraf/metrics" {
		http.NotFound(w, r)
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		http.Error(w, "loopback only", http.StatusForbidden)
		return
	}
	body := io.LimitReader(r.Body, 8<<20)
	defer r.Body.Close()
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(body)
		if err != nil {
			http.Error(w, "invalid gzip", http.StatusBadRequest)
			return
		}
		defer gz.Close()
		body = io.LimitReader(gz, 8<<20)
	}
	payload, err := io.ReadAll(body)
	if err != nil || len(payload) == 0 || !validLineProtocol(payload) {
		http.Error(w, "invalid influx line protocol", http.StatusBadRequest)
		return
	}
	payload = stampAgentID(payload, h.agentID)
	priority := spool.PriorityNormal
	if strings.Contains(strings.ToLower(string(payload)), "alert") ||
		strings.Contains(strings.ToLower(string(payload)), "critical") {
		priority = spool.PriorityAlert
	}
	if err := h.store.EnqueueWithPriority(h.seq.Add(1), payload, priority); err != nil {
		http.Error(w, fmt.Sprintf("queue metrics: %v", err), http.StatusServiceUnavailable)
		return
	}
	if h.health != nil {
		h.health.MarkMetric(time.Now().UTC())
	}
	w.WriteHeader(http.StatusNoContent)
}

func validLineProtocol(payload []byte) bool {
	for _, line := range strings.Split(strings.TrimSpace(string(payload)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.Contains(fields[0], ",") && !strings.Contains(fields[1], "=") {
			return false
		}
	}
	return true
}

// stampAgentID writes the Agent's id into every line so Influx queries by
// r.agent_id match Register, even if Telegraf's MONITOR_AGENT_ID was empty.
func stampAgentID(payload []byte, agentID string) []byte {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return payload
	}
	tag := "agent_id=" + agentID
	lines := strings.Split(string(payload), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		lines[i] = stampAgentIDLine(line, tag)
	}
	return []byte(strings.Join(lines, "\n"))
}

func stampAgentIDLine(line, tag string) string {
	space := strings.IndexByte(line, ' ')
	if space <= 0 {
		return line
	}
	head, tail := line[:space], line[space:]
	if i := strings.Index(head, "agent_id="); i >= 0 {
		rest := head[i+len("agent_id="):]
		end := strings.IndexByte(rest, ',')
		if end < 0 {
			head = head[:i] + tag
		} else {
			head = head[:i] + tag + rest[end:]
		}
		return head + tail
	}
	comma := strings.IndexByte(head, ',')
	if comma < 0 {
		return head + "," + tag + tail
	}
	return head[:comma+1] + tag + "," + head[comma+1:] + tail
}
