package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadServerConfigFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	raw := []byte("listen: \":9090\"\ntoken: from-yaml\napi_token: api-yaml\ndb: ./data/app.db\nplugins: ./hooks\nwrite_wait: 3s\npong_wait: 50s\nping_period: 15s\n")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadServerConfig([]string{"-config", path})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.ListenAddr != ":9090" || cfg.AuthToken != "from-yaml" || cfg.APIToken != "api-yaml" {
		t.Fatalf("unexpected basic config: %+v", cfg)
	}
	if cfg.DBPath != "./data/app.db" || cfg.PluginDir != "./hooks" {
		t.Fatalf("unexpected paths: %+v", cfg)
	}
	if cfg.WriteWait != 3*time.Second || cfg.PongWait != 50*time.Second || cfg.PingPeriod != 15*time.Second {
		t.Fatalf("unexpected durations: %+v", cfg)
	}
}

func TestLoadServerConfigFlagsOverrideYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	raw := []byte("listen: \":9090\"\ntoken: from-yaml\napi_token: api-yaml\n")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadServerConfig([]string{"-config", path, "-listen", ":8088", "-token", "from-flag"})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.ListenAddr != ":8088" {
		t.Fatalf("expected flag listen override, got %q", cfg.ListenAddr)
	}
	if cfg.AuthToken != "from-flag" {
		t.Fatalf("expected flag token override, got %q", cfg.AuthToken)
	}
	if cfg.APIToken != "api-yaml" {
		t.Fatalf("unexpected api token override, got %q", cfg.APIToken)
	}
}

func TestLoadServerConfigWithoutYAMLUsesDefaults(t *testing.T) {
	cfg, err := loadServerConfig(nil)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.ListenAddr != ":8080" || cfg.AuthToken != "cleanc2-dev-token" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.DBPath != "cleanc2.db" || cfg.PluginDir != "plugins" {
		t.Fatalf("unexpected default paths: %+v", cfg)
	}
}
