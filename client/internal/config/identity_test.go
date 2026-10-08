package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadFillsPlaceholderOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	original := "# keep me\n" +
		"agent_id: \"local-windows-002\"\n" +
		"agent_version: \"\"\n" +
		"config_version: \"\"\n" +
		"labels:\n" +
		"  environment: \"\"\n" +
		"  site: \"\"\n" +
		"  role: \"\"\n" +
		"server_address: \"127.0.0.1:9500\"\n" +
		"ca_file: \"client/certs/ca.pem\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AgentID == "" || cfg.AgentID == "local-windows-002" || len(cfg.AgentID) != 26 {
		t.Fatalf("agent_id = %q", cfg.AgentID)
	}
	if strings.ContainsAny(cfg.AgentID, "ILOU") {
		t.Fatalf("ulid alphabet: %s", cfg.AgentID)
	}
	if cfg.AgentVersion != "dev" || cfg.ConfigVersion != "local-1" {
		t.Fatalf("version %s %s", cfg.AgentVersion, cfg.ConfigVersion)
	}
	if cfg.Labels["environment"] != "test" || cfg.Labels["site"] != "local" || cfg.Labels["role"] != runtime.GOOS {
		t.Fatalf("labels %#v", cfg.Labels)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "# keep me") || !strings.Contains(text, `server_address: "127.0.0.1:9500"`) || !strings.Contains(text, `ca_file: "client/certs/ca.pem"`) {
		t.Fatalf("rewrote unrelated lines:\n%s", text)
	}
	if !strings.Contains(text, `agent_id: "`+cfg.AgentID+`"`) {
		t.Fatalf("yaml missing id:\n%s", text)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.AgentID != cfg.AgentID {
		t.Fatalf("id changed %s -> %s", cfg.AgentID, again.AgentID)
	}
	body2, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body2) != text {
		t.Fatal("second load rewrote agent.yaml")
	}
}

func TestLoadKeepsExplicitIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	original := "agent_id: \"host-a\"\n" +
		"agent_version: \"1.0.0\"\n" +
		"config_version: \"prod-1\"\n" +
		"labels:\n" +
		"  environment: production\n" +
		"  site: shanghai\n" +
		"  role: web\n" +
		"server_address: \"10.0.0.8:9500\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AgentID != "host-a" || cfg.Labels["environment"] != "production" || cfg.Labels["role"] != "web" {
		t.Fatalf("%s %#v", cfg.AgentID, cfg.Labels)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Fatalf("explicit identity was rewritten:\n%s", body)
	}
}
