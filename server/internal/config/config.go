package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	GRPCAddress string         `yaml:"grpc_address"`
	Postgres    PostgresConfig `yaml:"postgres"`
	Influx      InfluxConfig   `yaml:"influx"`
	TLS         TLSConfig      `yaml:"tls"`
}

type PostgresConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
	SSLMode  string `yaml:"ssl_mode"`
}

type InfluxConfig struct {
	URL          string `yaml:"url"`
	Token        string `yaml:"token"`
	Organization string `yaml:"organization"`
	Bucket       string `yaml:"bucket"`
}

type TLSConfig struct {
	CAFile   string `yaml:"ca_file"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read server configuration: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse server configuration: %w", err)
	}
	cfg.applyEnvironment()
	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyEnvironment() {
	set := func(key string, destination *string) {
		if value := os.Getenv(key); value != "" {
			*destination = value
		}
	}
	set("MONITOR_POSTGRES_PASSWORD", &c.Postgres.Password)
	set("MONITOR_INFLUX_TOKEN", &c.Influx.Token)
}

func (c *Config) setDefaults() {
	if c.GRPCAddress == "" {
		c.GRPCAddress = ":9500"
	}
	if c.Postgres.Port == 0 {
		c.Postgres.Port = 5432
	}
	if c.Postgres.SSLMode == "" {
		c.Postgres.SSLMode = "disable"
	}
	if c.Postgres.Database == "" {
		c.Postgres.Database = "postgres"
	}
}

func (c Config) validate() error {
	switch {
	case c.Postgres.Host == "" || c.Postgres.User == "":
		return fmt.Errorf("postgres host and user are required")
	case c.Influx.URL == "" || c.Influx.Organization == "" || c.Influx.Bucket == "":
		return fmt.Errorf("influx url, organization, and bucket are required")
	case c.Influx.Token == "" || strings.HasPrefix(c.Influx.Token, "replace-"):
		return fmt.Errorf("a valid InfluxDB API token is required (MONITOR_INFLUX_TOKEN is preferred)")
	case c.TLS.CAFile == "" || c.TLS.CertFile == "" || c.TLS.KeyFile == "":
		return fmt.Errorf("TLS CA, certificate, and private key paths are required")
	}
	return nil
}
