package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindFileUsesProvidedReadablePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "uddns.yaml")
	if err := os.WriteFile(path, []byte("providers: {}\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got, err := FindFile(path)
	if err != nil {
		t.Fatalf("FindFile returned error: %v", err)
	}
	if got != path {
		t.Fatalf("expected %q, got %q", path, got)
	}
}

func TestFindFileRejectsUnreadableProvidedPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "uddns.yaml"), []byte("providers: {}\n"), 0644); err != nil {
		t.Fatalf("write fallback config: %v", err)
	}
	t.Chdir(dir)

	missing := filepath.Join(dir, "missing.yaml")
	if _, err := FindFile(missing); err == nil {
		t.Fatal("expected unreadable provided path to return an error")
	}
}

func TestFindFileRejectsUnreadableEnvironmentPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "uddns.yaml"), []byte("providers: {}\n"), 0644); err != nil {
		t.Fatalf("write fallback config: %v", err)
	}
	t.Chdir(dir)
	t.Setenv("UDDNS_CONFIG", filepath.Join(dir, "missing.yaml"))

	if _, err := FindFile(""); err == nil {
		t.Fatal("expected unreadable UDDNS_CONFIG path to return an error")
	}
}

func TestIntervalUsesDefault(t *testing.T) {
	t.Setenv("UDDNS_INTERVAL", "")
	cfg := &Config{}

	duration, raw, err := cfg.Interval()
	if err != nil {
		t.Fatalf("Interval returned error: %v", err)
	}
	if duration != DefaultInterval {
		t.Fatalf("expected default interval %s, got %s", DefaultInterval, duration)
	}
	if raw != "" {
		t.Fatalf("expected empty raw interval, got %q", raw)
	}
}

func TestIntervalParsesEnvironment(t *testing.T) {
	t.Setenv("UDDNS_INTERVAL", "5m")
	cfg := &Config{}

	duration, raw, err := cfg.Interval()
	if err != nil {
		t.Fatalf("Interval returned error: %v", err)
	}
	if duration != 5*time.Minute {
		t.Fatalf("expected 5m, got %s", duration)
	}
	if raw != "5m" {
		t.Fatalf("expected raw interval 5m, got %q", raw)
	}
}

func TestIntervalReturnsDefaultOnInvalidEnvironment(t *testing.T) {
	t.Setenv("UDDNS_INTERVAL", "daily")
	cfg := &Config{}

	duration, raw, err := cfg.Interval()
	if err == nil {
		t.Fatal("expected invalid interval error")
	}
	if duration != DefaultInterval {
		t.Fatalf("expected default interval %s, got %s", DefaultInterval, duration)
	}
	if raw != "daily" {
		t.Fatalf("expected raw interval daily, got %q", raw)
	}
}

func TestIntervalRejectsValuesOutsideAllowedRange(t *testing.T) {
	tests := []string{"1ns", "9s", "24h1s"}
	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			t.Setenv("UDDNS_INTERVAL", value)
			cfg := &Config{}

			duration, raw, err := cfg.Interval()
			if err == nil {
				t.Fatalf("expected interval %q to be rejected", value)
			}
			if duration != DefaultInterval || raw != value {
				t.Fatalf("expected default/%q, got %s/%q", value, duration, raw)
			}
		})
	}
}

func TestIntervalAcceptsAllowedBoundaries(t *testing.T) {
	tests := []struct {
		value string
		want  time.Duration
	}{
		{value: "10s", want: MinInterval},
		{value: "24h", want: MaxInterval},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("UDDNS_INTERVAL", tt.value)
			cfg := &Config{}

			duration, _, err := cfg.Interval()
			if err != nil {
				t.Fatalf("Interval returned error: %v", err)
			}
			if duration != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, duration)
			}
		})
	}
}

func TestJobsParsesConfiguredJobs(t *testing.T) {
	path := writeConfigFile(t, `
jobs:
  - name: home
    provider: ip_service
    updater: duckdns
    record: home
    families: [ipv4]
    verify: off
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	jobs, ok, err := cfg.Jobs()
	if err != nil {
		t.Fatalf("Jobs returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected jobs to be configured")
	}
	if len(jobs) != 1 || jobs[0].Name != "home" || jobs[0].Record != "home" || jobs[0].VerifyMode() != "off" {
		t.Fatalf("unexpected jobs: %#v", jobs)
	}
}

func TestWithOverridesAppliesNestedValues(t *testing.T) {
	path := writeConfigFile(t, `
updaters:
  duckdns:
    token: test-token
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	overlaid := cfg.WithOverrides(map[string]any{
		"updaters.duckdns.domain": "home",
	})

	var duckDNS struct {
		Token  string `mapstructure:"token"`
		Domain string `mapstructure:"domain"`
	}
	if err := overlaid.UnmarshalKey("updaters.duckdns", &duckDNS); err != nil {
		t.Fatalf("UnmarshalKey returned error: %v", err)
	}
	if duckDNS.Token != "test-token" || duckDNS.Domain != "home" {
		t.Fatalf("unexpected overlaid config: %#v", duckDNS)
	}
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "uddns.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
