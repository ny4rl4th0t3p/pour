package config

import (
	"testing"

	"github.com/alecthomas/kong"
)

func parseServeConfig(t *testing.T, args []string) ServeConfig {
	t.Helper()
	var cfg ServeConfig
	p, err := kong.New(&cfg)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	if _, err := p.Parse(args); err != nil {
		t.Fatalf("Parse(%v): %v", args, err)
	}
	return cfg
}

func TestServeConfig_defaults(t *testing.T) {
	cfg := parseServeConfig(t, []string{})

	if cfg.Listen != ":8080" {
		t.Errorf("Listen: got %q, want %q", cfg.Listen, ":8080")
	}
	if cfg.ConfigFile != "chains.yml" {
		t.Errorf("ConfigFile: got %q, want %q", cfg.ConfigFile, "chains.yml")
	}
	if cfg.DBPath != "pour.db" {
		t.Errorf("DBPath: got %q, want %q", cfg.DBPath, "pour.db")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel: got %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.Metrics {
		t.Error("Metrics: want false by default")
	}
	if cfg.NoUI {
		t.Error("NoUI: want false by default")
	}
}

func TestServeConfig_envOverride(t *testing.T) {
	t.Setenv("POUR_LISTEN", ":9090")
	t.Setenv("POUR_CONFIG", "/etc/pour/chains.yml")
	t.Setenv("POUR_DB_PATH", "/var/lib/pour/pour.db")
	t.Setenv("POUR_LOG_LEVEL", "debug")
	t.Setenv("POUR_METRICS", "true")
	t.Setenv("POUR_NO_UI", "true")

	cfg := parseServeConfig(t, []string{})

	if cfg.Listen != ":9090" {
		t.Errorf("Listen: got %q, want :9090", cfg.Listen)
	}
	if cfg.ConfigFile != "/etc/pour/chains.yml" {
		t.Errorf("ConfigFile: got %q, want /etc/pour/chains.yml", cfg.ConfigFile)
	}
	if cfg.DBPath != "/var/lib/pour/pour.db" {
		t.Errorf("DBPath: got %q, want /var/lib/pour/pour.db", cfg.DBPath)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel: got %q, want debug", cfg.LogLevel)
	}
	if !cfg.Metrics {
		t.Error("Metrics: want true from env")
	}
	if !cfg.NoUI {
		t.Error("NoUI: want true from env")
	}
}

func TestServeConfig_flags(t *testing.T) {
	cfg := parseServeConfig(t, []string{
		"--listen=:7070",
		"--config-file=/tmp/chains.yml",
		"--db-path=/tmp/pour.db",
		"--log-level=warn",
		"--metrics",
		"--no-ui",
	})

	if cfg.Listen != ":7070" {
		t.Errorf("Listen: got %q, want :7070", cfg.Listen)
	}
	if cfg.ConfigFile != "/tmp/chains.yml" {
		t.Errorf("ConfigFile: got %q, want /tmp/chains.yml", cfg.ConfigFile)
	}
	if cfg.DBPath != "/tmp/pour.db" {
		t.Errorf("DBPath: got %q, want /tmp/pour.db", cfg.DBPath)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("LogLevel: got %q, want warn", cfg.LogLevel)
	}
	if !cfg.Metrics {
		t.Error("Metrics: want true from flag")
	}
	if !cfg.NoUI {
		t.Error("NoUI: want true from flag")
	}
}
