package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/we11adam/uddns/app"
	"github.com/we11adam/uddns/internal/config"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
)

func TestParseLogLevel(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
	t.Parallel()
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
			t.Parallel()
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

func TestRunConfigCheckSupportsScaleway(t *testing.T) {
	t.Setenv("UDDNS_INTERVAL", "")
	path := writeTempConfig(t, `
providers:
  ip_service:
    - ifconfig.me
updaters:
  scaleway:
    access_key: SCW1234567890ABCDEFG
    secret_key: 7363616c-6577-6573-6862-6f7579616161
    project_id: 6170692e-7363-616c-6577-61792e636f6e
jobs:
  - name: home
    provider: ip_service
    updater: scaleway
    record: home.dev.example.com
    zone: dev.example.com
    verify: updater_api
`)

	code := run([]string{"config", "check", "-c", path})
	if code != 0 {
		t.Fatalf("expected Scaleway config check to succeed, got exit code %d", code)
	}
}

func TestJobUpdaterSettingsReplaceRecordFields(t *testing.T) {
	t.Parallel()
	instance := updater.Instance{
		Name:     "duckdns",
		Settings: map[string]any{"token": "test-token", "domain": "inherited", "zone": "inherited.example"},
	}
	settings, err := jobUpdaterSettings(instance, "home", "")
	if err != nil {
		t.Fatalf("jobUpdaterSettings returned an error: %v", err)
	}
	if settings["domain"] != "home" || settings["token"] != "test-token" {
		t.Fatalf("unexpected job updater settings: %#v", settings)
	}
	if zone, ok := settings["zone"]; !ok || zone != "" {
		t.Fatalf("expected an explicit empty zone, got %#v, %v", zone, ok)
	}
	if instance.Settings.(map[string]any)["domain"] != "inherited" {
		t.Fatal("jobUpdaterSettings must not modify the shared instance settings")
	}
}

func TestLoadJobsSupportsNamedUpdaterInstances(t *testing.T) {
	path := writeTempConfig(t, `
providers:
  ip_service:
    - ifconfig.me
updaters:
  scw-a:
    type: scaleway
    access_key: SCW1234567890ABCDEFG
    secret_key: 7363616c-6577-6573-6862-6f7579616161
    project_id: 6170692e-7363-616c-6577-61792e636f6e
  scw-b:
    type: Scaleway
    access_key: SCWABCDEFG1234567890
    secret_key: 7363616c-6577-6573-6862-6f7579616162
    project_id: 6170692e-7363-616c-6577-61792e636f6f
jobs:
  - name: a
    provider: ip_service
    updater: scw-a
    record: a.example.com
  - name: b
    provider: ip_service
    updater: SCW-B
    record: b.example.com
`)

	jobs := mustLoadJobs(t, path)
	if len(jobs) != 2 {
		t.Fatalf("expected two jobs, got %d", len(jobs))
	}
	if jobs[0].UpdaterName != "scw-a (Scaleway)" || jobs[1].UpdaterName != "scw-b (Scaleway)" {
		t.Fatalf("unexpected updater names: %q, %q", jobs[0].UpdaterName, jobs[1].UpdaterName)
	}
}

func TestLoadJobsRejectsAmbiguousUpdaterType(t *testing.T) {
	path := writeTempConfig(t, `
providers:
  ip_service:
    - ifconfig.me
updaters:
  duck-a:
    type: duckdns
    token: token-a
  duck-b:
    type: duckdns
    token: token-b
jobs:
  - provider: ip_service
    updater: duckdns
    record: home
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	_, err = loadJobs(cfg)
	if err == nil || !strings.Contains(err.Error(), `updater "duckdns" is ambiguous; select one of: duck-a, duck-b`) {
		t.Fatalf("expected ambiguous updater error, got %v", err)
	}
}

func TestLoadJobsSupportsNamedProviderInstancesAndFallback(t *testing.T) {
	path := writeTempConfig(t, `
providers:
  public:
    type: ip_service
    services: [ifconfig.me, ip.fm]
  ip_service:
    - ip.sb
updaters:
  duckdns:
    token: test-token
jobs:
  - name: home
    provider: [public, ip_service]
    updater: duckdns
    record: home
`)

	jobs := mustLoadJobs(t, path)
	if jobs[0].ProviderName != "public (IpService) -> IpService" {
		t.Fatalf("unexpected provider name: %q", jobs[0].ProviderName)
	}
	if _, ok := jobs[0].Provider.(*provider.Chain); !ok {
		t.Fatalf("expected a provider chain, got %T", jobs[0].Provider)
	}
}

func TestLoadJobsRejectsInstanceWithoutType(t *testing.T) {
	path := writeTempConfig(t, `
providers:
  ip_service:
    - ifconfig.me
updaters:
  home:
    token: test-token
    domain: home
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	_, err = loadJobs(cfg)
	if err == nil || !strings.Contains(err.Error(), `updater "home" needs a "type"`) {
		t.Fatalf("expected missing type error, got %v", err)
	}
}

func TestLoadJobsRejectsReservedInstanceName(t *testing.T) {
	path := writeTempConfig(t, `
providers:
  ip_service:
    - ifconfig.me
updaters:
  cloudflare:
    type: duckdns
    token: test-token
    domain: home
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	_, err = loadJobs(cfg)
	if err == nil || !strings.Contains(err.Error(), `updater name "cloudflare" is reserved for type Cloudflare`) {
		t.Fatalf("expected reserved name error, got %v", err)
	}
}

func TestLoadDefaultJobsRunsEverySelectedUpdater(t *testing.T) {
	path := writeTempConfig(t, `
providers:
  use: [ip_service, public]
  ip_service:
    - ifconfig.me
  public:
    type: ip_service
    services: [ip.fm]
updaters:
  use: [duckdns, lightdns]
  duckdns:
    token: test-token
    domain: home
  lightdns:
    key: test-key
    domain: home.example.com
  cloudflare:
    apitoken: unused-token
    domain: unused.example.com
`)

	jobs := mustLoadJobs(t, path)
	if len(jobs) != 2 {
		t.Fatalf("expected one job per selected updater, got %d", len(jobs))
	}
	if jobs[0].Name != "duckdns" || jobs[0].Record != "home" || jobs[1].Name != "lightdns" || jobs[1].Record != "home.example.com" {
		t.Fatalf("unexpected jobs: %+v / %+v", jobs[0], jobs[1])
	}
	if jobs[0].Provider != jobs[1].Provider {
		t.Fatal("expected default jobs to share the provider")
	}
	if _, ok := jobs[0].Provider.(*provider.Chain); !ok {
		t.Fatalf("expected a provider chain, got %T", jobs[0].Provider)
	}
}

func TestLoadDefaultJobsKeepsSingleUpdaterJobName(t *testing.T) {
	path := writeTempConfig(t, `
providers:
  ip_service:
    - ifconfig.me
updaters:
  use: DuckDNS
  duckdns:
    token: test-token
    domain: home
`)

	jobs := mustLoadJobs(t, path)
	if len(jobs) != 1 || jobs[0].Name != "default" || jobs[0].UpdaterName != "DuckDNS" || jobs[0].ProviderName != "IpService" {
		t.Fatalf("unexpected jobs: %+v", jobs)
	}
}

func TestLoadDefaultJobsUsesFirstUpdaterWithoutSelection(t *testing.T) {
	path := writeTempConfig(t, `
providers:
  ip_service:
    - ifconfig.me
updaters:
  lightdns:
    key: test-key
    domain: home.example.com
  duckdns:
    token: test-token
    domain: home
`)

	jobs := mustLoadJobs(t, path)
	if len(jobs) != 1 || jobs[0].UpdaterName != "DuckDNS" {
		t.Fatalf("expected the first updater in registry order, got %+v", jobs)
	}
}

func TestRunConfigCheckSupportsUpdaterAPIVerify(t *testing.T) {
	t.Setenv("UDDNS_INTERVAL", "")
	path := writeTempConfig(t, `
providers:
  ip_service:
    - ifconfig.me
updaters:
  cloudflare:
    apitoken: test-token
jobs:
  - name: home
    provider: ip_service
    updater: cloudflare
    record: home.example.com
    verify: updater_api
`)

	code := run([]string{"config", "check", "-c", path})
	if code != 0 {
		t.Fatalf("expected updater_api verify config check to succeed, got exit code %d", code)
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

func TestRunConfigCheckRejectsUnsupportedUpdaterAPIVerify(t *testing.T) {
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
    verify: updater_api
`)

	code := run([]string{"config", "check", "-c", path})
	if code == 0 {
		t.Fatal("expected unsupported updater_api verify config check to fail")
	}
}

func TestRunConfigCheckRejectsUnknownVerifyMode(t *testing.T) {
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
    verify: provider_api
`)

	code := run([]string{"config", "check", "-c", path})
	if code == 0 {
		t.Fatal("expected unknown verify config check to fail")
	}
}

func TestRunConfigCheckRejectsUnsupportedTopLevelUpdaterAPIVerify(t *testing.T) {
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
    domain: home-subdomain
verify: updater_api
`)

	code := run([]string{"config", "check", "-c", path})
	if code == 0 {
		t.Fatal("expected unsupported top-level updater_api verify config check to fail")
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "uddns.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func mustLoadJobs(t *testing.T, path string) []app.Job {
	t.Helper()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	jobs, err := loadJobs(cfg)
	if err != nil {
		t.Fatalf("loadJobs returned error: %v", err)
	}
	return jobs
}
