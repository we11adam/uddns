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
