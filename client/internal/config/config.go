package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AgentID             string            `yaml:"agent_id"`
	AgentVersion        string            `yaml:"agent_version"`
	ConfigVersion       string            `yaml:"config_version"`
	Labels              map[string]string `yaml:"labels"`
	ListenAddress       string            `yaml:"listen_address"`
	ServerAddress       string            `yaml:"server_address"`
	ReportIP            string            `yaml:"report_ip"`
	InsecureTLS         bool              `yaml:"insecure_tls"`
	CAFile              string            `yaml:"ca_file"`
	CertFile            string            `yaml:"cert_file"`
	KeyFile             string            `yaml:"key_file"`
	StateDirectory      string            `yaml:"state_directory"`
	HeartbeatInterval   time.Duration     `yaml:"heartbeat_interval"`
	ReconnectMax        time.Duration     `yaml:"reconnect_max"`
	MaxSpoolBytes       int64             `yaml:"max_spool_bytes"`
	MaxSpoolAge         time.Duration     `yaml:"max_spool_age"`
	MaxBacklogBatches   int               `yaml:"max_backlog_batches"`
	MaxBacklogParallel  int               `yaml:"max_backlog_parallel"`
	BacklogRateBytes    int64             `yaml:"backlog_rate_bytes_per_second"`
	RetryBase           time.Duration     `yaml:"retry_base"`
	RetryMax            time.Duration     `yaml:"retry_max"`
	HealthAddress       string            `yaml:"health_address"`
	TelegrafBinary      string            `yaml:"telegraf_binary"`
	TelegrafService     string            `yaml:"telegraf_service"`
	TelegrafCheck       time.Duration     `yaml:"telegraf_check_interval"`
	AutoRecoverTelegraf bool              `yaml:"auto_recover_telegraf"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var raw struct {
		AgentID             string            `yaml:"agent_id"`
		AgentVersion        string            `yaml:"agent_version"`
		ConfigVersion       string            `yaml:"config_version"`
		Labels              map[string]string `yaml:"labels"`
		ListenAddress       string            `yaml:"listen_address"`
		ServerAddress       string            `yaml:"server_address"`
		ReportIP            string            `yaml:"report_ip"`
		InsecureTLS         bool              `yaml:"insecure_tls"`
		CAFile              string            `yaml:"ca_file"`
		CertFile            string            `yaml:"cert_file"`
		KeyFile             string            `yaml:"key_file"`
		StateDirectory      string            `yaml:"state_directory"`
		HeartbeatInterval   string            `yaml:"heartbeat_interval"`
		ReconnectMax        string            `yaml:"reconnect_max"`
		MaxSpoolBytes       int64             `yaml:"max_spool_bytes"`
		MaxSpoolAge         string            `yaml:"max_spool_age"`
		MaxBacklogBatches   int               `yaml:"max_backlog_batches"`
		MaxBacklogParallel  int               `yaml:"max_backlog_parallel"`
		BacklogRateBytes    int64             `yaml:"backlog_rate_bytes_per_second"`
		RetryBase           string            `yaml:"retry_base"`
		RetryMax            string            `yaml:"retry_max"`
		HealthAddress       string            `yaml:"health_address"`
		TelegrafBinary      string            `yaml:"telegraf_binary"`
		TelegrafService     string            `yaml:"telegraf_service"`
		TelegrafCheck       string            `yaml:"telegraf_check_interval"`
		AutoRecoverTelegraf bool              `yaml:"auto_recover_telegraf"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	raw.AgentID, raw.AgentVersion, raw.ConfigVersion, raw.Labels, err = ensureIdentity(
		path, raw.AgentID, raw.AgentVersion, raw.ConfigVersion, raw.Labels)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{AgentID: raw.AgentID, AgentVersion: raw.AgentVersion, ConfigVersion: raw.ConfigVersion,
		Labels: raw.Labels, ListenAddress: raw.ListenAddress,
		ServerAddress: raw.ServerAddress, ReportIP: raw.ReportIP, InsecureTLS: raw.InsecureTLS, CAFile: raw.CAFile,
		CertFile: raw.CertFile, KeyFile: raw.KeyFile, StateDirectory: raw.StateDirectory,
		MaxSpoolBytes: raw.MaxSpoolBytes}
	cfg.MaxSpoolAge = parseDuration(raw.MaxSpoolAge, 24*time.Hour)
	cfg.RetryBase = parseDuration(raw.RetryBase, time.Second)
	cfg.RetryMax = parseDuration(raw.RetryMax, 5*time.Minute)
	cfg.HealthAddress = raw.HealthAddress
	cfg.TelegrafBinary = raw.TelegrafBinary
	cfg.TelegrafService = raw.TelegrafService
	cfg.TelegrafCheck = parseDuration(raw.TelegrafCheck, 5*time.Second)
	cfg.MaxBacklogBatches = raw.MaxBacklogBatches
	cfg.MaxBacklogParallel = raw.MaxBacklogParallel
	cfg.BacklogRateBytes = raw.BacklogRateBytes
	cfg.AutoRecoverTelegraf = raw.AutoRecoverTelegraf
	if raw.HeartbeatInterval != "" {
		if cfg.HeartbeatInterval, err = time.ParseDuration(raw.HeartbeatInterval); err != nil {
			return Config{}, fmt.Errorf("parse heartbeat_interval: %w", err)
		}
	}
	if raw.ReconnectMax != "" {
		if cfg.ReconnectMax, err = time.ParseDuration(raw.ReconnectMax); err != nil {
			return Config{}, fmt.Errorf("parse reconnect_max: %w", err)
		}
	}
	if cfg.ServerAddress == "" {
		return Config{}, fmt.Errorf("server_address is required")
	}
	if cfg.ListenAddress == "" {
		cfg.ListenAddress = "127.0.0.1:9510"
	}
	if cfg.StateDirectory == "" {
		cfg.StateDirectory = "./state"
	}
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 5 * time.Second
	}
	if cfg.ReconnectMax == 0 {
		cfg.ReconnectMax = 30 * time.Second
	}
	if cfg.MaxSpoolBytes == 0 {
		cfg.MaxSpoolBytes = 1 << 30
	}
	if cfg.MaxSpoolAge == 0 {
		cfg.MaxSpoolAge = 24 * time.Hour
	}
	if cfg.MaxBacklogBatches == 0 {
		cfg.MaxBacklogBatches = 10
	}
	if cfg.MaxBacklogParallel == 0 {
		cfg.MaxBacklogParallel = 1
	}
	if cfg.BacklogRateBytes == 0 {
		cfg.BacklogRateBytes = 1 << 20
	}
	if cfg.HealthAddress == "" {
		cfg.HealthAddress = "127.0.0.1:9511"
	}
	if cfg.AgentVersion == "" {
		cfg.AgentVersion = "dev"
	}
	if cfg.ConfigVersion == "" {
		cfg.ConfigVersion = "local-1"
	}
	return cfg, nil
}

func parseDuration(value string, fallback time.Duration) time.Duration {
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
