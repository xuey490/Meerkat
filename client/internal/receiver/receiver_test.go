package receiver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"monitor-client/internal/spool"
)

func TestStampAgentIDInsertsAndReplaces(t *testing.T) {
	got := string(stampAgentID([]byte("cpu,cpu=cpu-total,host=box usage_idle=90 1\n"), "A1"))
	if !strings.Contains(got, "agent_id=A1") || !strings.Contains(got, "cpu=cpu-total") {
		t.Fatalf("insert: %s", got)
	}
	got = string(stampAgentID([]byte("mem,agent_id=old,host=box used_percent=10 1\n"), "A2"))
	if strings.Contains(got, "agent_id=old") || !strings.Contains(got, "agent_id=A2") {
		t.Fatalf("replace: %s", got)
	}
}

func TestAcceptsInfluxLineProtocolAndQueuesBatch(t *testing.T) {
	store, err := spool.Open(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9510/v1/telegraf/metrics",
		strings.NewReader("cpu,agent_id=test usage_idle=92.5 1730000000000000000\n"))
	request.RemoteAddr = "127.0.0.1:12345"
	response := httptest.NewRecorder()

	New(store).ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	batch, err := store.Next()
	if err != nil || batch == nil {
		t.Fatalf("batch = %#v, err = %v", batch, err)
	}
}
