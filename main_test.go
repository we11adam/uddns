package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name  string
		value string
		level slog.Level
		ok    bool
	}{
		{name: "default", value: "", level: slog.LevelInfo, ok: true},
		{name: "debug", value: "debug", level: slog.LevelDebug, ok: true},
		{name: "info uppercase", value: "INFO", level: slog.LevelInfo, ok: true},
		{name: "warning alias", value: "warning", level: slog.LevelWarn, ok: true},
		{name: "error", value: "error", level: slog.LevelError, ok: true},
		{name: "invalid", value: "trace", level: slog.LevelInfo, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, ok := parseLogLevel(tt.value)
			if level != tt.level {
				t.Fatalf("expected level %v, got %v", tt.level, level)
			}
			if ok != tt.ok {
				t.Fatalf("expected ok %v, got %v", tt.ok, ok)
			}
		})
	}
}

func TestParseLogRetentionDays(t *testing.T) {
	tests := []struct {
		name  string
		value string
		days  int
		ok    bool
	}{
		{name: "default", value: "", days: defaultLogRetentionDays, ok: true},
		{name: "custom", value: "14", days: 14, ok: true},
		{name: "trimmed", value: " 3 ", days: 3, ok: true},
		{name: "zero", value: "0", days: defaultLogRetentionDays, ok: false},
		{name: "negative", value: "-1", days: defaultLogRetentionDays, ok: false},
		{name: "invalid", value: "daily", days: defaultLogRetentionDays, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			days, ok := parseLogRetentionDays(tt.value)
			if days != tt.days {
				t.Fatalf("expected days %d, got %d", tt.days, days)
			}
			if ok != tt.ok {
				t.Fatalf("expected ok %v, got %v", tt.ok, ok)
			}
		})
	}
}

func TestResolveLogConfigUsesConfigFileValues(t *testing.T) {
	t.Setenv("UDDNS_LOG_LEVEL", "")
	t.Setenv("UDDNS_LOG_DIR", "")
	t.Setenv("UDDNS_LOG_RETENTION_DAYS", "")

	v := viper.New()
	v.Set("logging.level", "debug")
	v.Set("logging.dir", "/var/log/uddns")
	v.Set("logging.retention_days", 14)

	config := resolveLogConfig(v)

	if config.level.value != "debug" {
		t.Fatalf("expected config log level debug, got %q", config.level.value)
	}
	if config.dir.value != "/var/log/uddns" {
		t.Fatalf("expected config log dir /var/log/uddns, got %q", config.dir.value)
	}
	if config.retentionDays.value != "14" {
		t.Fatalf("expected config retention days 14, got %q", config.retentionDays.value)
	}
}

func TestResolveLogConfigLetsEnvironmentOverrideConfig(t *testing.T) {
	t.Setenv("UDDNS_LOG_LEVEL", "warn")
	t.Setenv("UDDNS_LOG_DIR", "/tmp/uddns")
	t.Setenv("UDDNS_LOG_RETENTION_DAYS", "3")

	v := viper.New()
	v.Set("logging.level", "debug")
	v.Set("logging.dir", "/var/log/uddns")
	v.Set("logging.retention_days", 14)

	config := resolveLogConfig(v)

	if config.level.value != "warn" || config.level.source != "env:UDDNS_LOG_LEVEL" {
		t.Fatalf("expected env log level override, got %#v", config.level)
	}
	if config.dir.value != "/tmp/uddns" || config.dir.source != "env:UDDNS_LOG_DIR" {
		t.Fatalf("expected env log dir override, got %#v", config.dir)
	}
	if config.retentionDays.value != "3" || config.retentionDays.source != "env:UDDNS_LOG_RETENTION_DAYS" {
		t.Fatalf("expected env retention override, got %#v", config.retentionDays)
	}
}

func TestRunConfigCheckValidatesConfig(t *testing.T) {
	t.Setenv("UDDNS_INTERVAL", "")
	path := writeTempConfig(t, `
providers:
  use: ip_service
  ip_service:
    - ifconfig.me
updaters:
  use: duckdns
  duckdns:
    token: test-token
    domain: test-subdomain
`)

	code := run([]string{"config", "check", "-c", path})
	if code != 0 {
		t.Fatalf("expected config check to succeed, got exit code %d", code)
	}
}

func TestRunConfigCheckReportsInvalidConfig(t *testing.T) {
	t.Setenv("UDDNS_INTERVAL", "")
	path := writeTempConfig(t, `
providers:
  use: ip_service
  ip_service:
    - missing-service
updaters:
  use: duckdns
  duckdns:
    token: test-token
    domain: test-subdomain
`)

	code := run([]string{"config", "check", "-c", path})
	if code == 0 {
		t.Fatal("expected config check to fail")
	}
}

func TestRunConfigCheckSupportsJobs(t *testing.T) {
	t.Setenv("UDDNS_INTERVAL", "")
	path := writeTempConfig(t, `
providers:
  ip_service:
    - ifconfig.me
updaters:
  duckdns:
    token: test-token
jobs:
  - name: home
    provider: ip_service
    updater: duckdns
    record: home-subdomain
    families: [ipv4]
`)

	code := run([]string{"config", "check", "-c", path})
	if code != 0 {
		t.Fatalf("expected jobs config check to succeed, got exit code %d", code)
	}
}

func TestRunConfigCheckRejectsInvalidJobs(t *testing.T) {
	t.Setenv("UDDNS_INTERVAL", "")
	path := writeTempConfig(t, `
providers:
  ip_service:
    - ifconfig.me
updaters:
  duckdns:
    token: test-token
jobs:
  - name: home
    provider: ip_service
    updater: duckdns
    families: [ipv4]
`)

	code := run([]string{"config", "check", "-c", path})
	if code == 0 {
		t.Fatal("expected jobs config check to fail")
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "uddns.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
