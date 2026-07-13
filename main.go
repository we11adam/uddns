package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/we11adam/uddns/app"
	"github.com/we11adam/uddns/internal/config"
	"github.com/we11adam/uddns/notifier"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"

	_ "github.com/we11adam/uddns/notifier/telegram"
	_ "github.com/we11adam/uddns/provider/ip_service"
	_ "github.com/we11adam/uddns/provider/netif"
	_ "github.com/we11adam/uddns/provider/routeros"
	_ "github.com/we11adam/uddns/updater/aliyun"
	_ "github.com/we11adam/uddns/updater/cloudflare"
	_ "github.com/we11adam/uddns/updater/duckdns"
	_ "github.com/we11adam/uddns/updater/lightdns"
)

func init() {
	configureLogger()
}

func main() {
	os.Exit(run(os.Args[1:]))
}

type runtimeConfig struct {
	notifierName string
	notifier     notifier.Notifier
	jobs         []app.Job
	interval     time.Duration
}

func run(args []string) int {
	if len(args) > 0 && args[0] == "config" {
		return runConfigCommand(args[1:])
	}

	rt, ok := loadRuntimeFromFlags("uddns", args)
	if !ok {
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app.NewApp(rt.jobs, rt.notifierName, rt.notifier, rt.interval).Run(ctx)
	return 0
}

func runConfigCommand(args []string) int {
	if len(args) == 0 {
		slog.Error("missing config subcommand", "usage", "uddns config check [-c path]")
		return 2
	}

	switch args[0] {
	case "check":
		rt, ok := loadRuntimeFromFlags("uddns config check", args[1:])
		if !ok {
			return 1
		}
		slog.Info(
			"config valid",
			"notifier", rt.notifierName,
			"jobs", len(rt.jobs),
			"interval", rt.interval,
		)
		return 0
	default:
		slog.Error("unknown config subcommand", "subcommand", args[0], "usage", "uddns config check [-c path]")
		return 2
	}
}

func loadRuntimeFromFlags(name string, args []string) (*runtimeConfig, bool) {
	var configPath string
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.StringVar(&configPath, "c", "", "Path to the configuration file")
	if err := flags.Parse(args); err != nil {
		return nil, false
	}
	if flags.NArg() > 0 {
		slog.Error("unexpected argument", "argument", flags.Arg(0))
		return nil, false
	}

	rt, err := loadRuntime(configPath)
	if err != nil {
		slog.Error("failed to validate config", "error", err)
		return nil, false
	}

	return rt, true
}

func loadRuntime(configPath string) (*runtimeConfig, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config file: %w", err)
	}
	configureLoggerFromConfig(cfg)
	slog.Info("using config file", "config", cfg.Path())

	jobs, err := loadJobs(cfg)
	if err != nil {
		return nil, err
	}

	notifierName, n, err := notifier.GetNotifier(cfg)
	if err != nil {
		return nil, fmt.Errorf("notifier configuration error: %w", err)
	}
	slog.Info("notifier selected", "notifier", notifierName)

	interval, rawInterval, err := cfg.Interval()
	if err != nil {
		slog.Warn("invalid update interval, using default", "env_var", "UDDNS_INTERVAL", "value", rawInterval, "default", config.DefaultInterval, "error", err)
	}

	return &runtimeConfig{
		notifierName: notifierName,
		notifier:     n,
		jobs:         jobs,
		interval:     interval,
	}, nil
}

func loadJobs(cfg *config.Config) ([]app.Job, error) {
	jobConfigs, ok, err := cfg.Jobs()
	if err != nil {
		return nil, fmt.Errorf("failed to read jobs: %w", err)
	}
	if !ok {
		job, err := loadDefaultJob(cfg)
		if err != nil {
			return nil, err
		}
		return []app.Job{job}, nil
	}
	if len(jobConfigs) == 0 {
		return nil, fmt.Errorf("jobs must contain at least one job")
	}

	jobs := make([]app.Job, 0, len(jobConfigs))
	seen := map[string]struct{}{}
	for i, jobConfig := range jobConfigs {
		job, err := loadConfiguredJob(cfg, jobConfig, i)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[job.Name]; exists {
			return nil, fmt.Errorf("job %q is duplicated", job.Name)
		}
		seen[job.Name] = struct{}{}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func loadDefaultJob(cfg *config.Config) (app.Job, error) {
	providerName, p, err := provider.GetProvider(cfg)
	if err != nil {
		return app.Job{}, fmt.Errorf("no provider found: %w", err)
	}
	updaterName, u, err := updater.GetUpdater(cfg)
	if err != nil {
		return app.Job{}, fmt.Errorf("no updater found: %w", err)
	}
	verify, err := parseVerify(cfg.Verify())
	if err != nil {
		return app.Job{}, fmt.Errorf("default job verify error: %w", err)
	}
	if err := validateVerifySupport("default", updaterName, u, verify); err != nil {
		return app.Job{}, err
	}
	record, zone := defaultJobRecord(cfg, updaterName)

	slog.Info("job selected", "job", "default", "provider", providerName, "updater", updaterName, "record", record, "zone", zone, "families", app.AllFamilies().String(), "verify", verify)
	return app.NewJob("default", providerName, p, updaterName, u, record, zone, app.AllFamilies(), verify), nil
}

func loadConfiguredJob(cfg *config.Config, jobConfig config.Job, index int) (app.Job, error) {
	name := strings.TrimSpace(jobConfig.Name)
	if name == "" {
		name = fmt.Sprintf("job-%d", index+1)
	}
	if strings.TrimSpace(jobConfig.Provider) == "" {
		return app.Job{}, fmt.Errorf("job %q missing provider", name)
	}
	if strings.TrimSpace(jobConfig.Updater) == "" {
		return app.Job{}, fmt.Errorf("job %q missing updater", name)
	}
	record := strings.TrimSpace(jobConfig.Record)
	zone := strings.TrimSpace(jobConfig.Zone)
	if record == "" {
		return app.Job{}, fmt.Errorf("job %q missing record", name)
	}

	families, err := parseFamilies(jobConfig.Families)
	if err != nil {
		return app.Job{}, fmt.Errorf("job %q invalid families: %w", name, err)
	}
	verify, err := parseVerify(jobConfig.VerifyMode())
	if err != nil {
		return app.Job{}, fmt.Errorf("job %q verify error: %w", name, err)
	}

	overrides, err := jobOverrides(jobConfig)
	if err != nil {
		return app.Job{}, fmt.Errorf("job %q updater error: %w", name, err)
	}
	jobReader := cfg.WithOverrides(overrides)
	providerName, p, err := provider.GetProvider(jobReader)
	if err != nil {
		return app.Job{}, fmt.Errorf("job %q provider error: %w", name, err)
	}
	updaterName, u, err := updater.GetUpdater(jobReader)
	if err != nil {
		return app.Job{}, fmt.Errorf("job %q updater error: %w", name, err)
	}
	if err := validateVerifySupport(name, updaterName, u, verify); err != nil {
		return app.Job{}, err
	}

	slog.Info("job selected", "job", name, "provider", providerName, "updater", updaterName, "record", record, "zone", zone, "families", families.String(), "verify", verify)
	return app.NewJob(name, providerName, p, updaterName, u, record, zone, families, verify), nil
}

func defaultJobRecord(cfg *config.Config, updaterName string) (string, string) {
	configKey, ok := updater.ConfigKey(updaterName)
	if !ok {
		return "", ""
	}
	return strings.TrimSpace(cfg.GetString(configKey + ".domain")), strings.TrimSpace(cfg.GetString(configKey + ".zone"))
}

func jobOverrides(job config.Job) (map[string]any, error) {
	record := strings.TrimSpace(job.Record)
	zone := strings.TrimSpace(job.Zone)
	configKey, ok := updater.ConfigKey(job.Updater)
	if !ok {
		return nil, fmt.Errorf("unknown updater %q", strings.TrimSpace(job.Updater))
	}
	overrides := map[string]any{
		"providers.use":       strings.TrimSpace(job.Provider),
		"updaters.use":        strings.TrimSpace(job.Updater),
		configKey + ".domain": record,
	}
	if zone != "" {
		overrides[configKey+".zone"] = zone
	}
	return overrides, nil
}

func parseFamilies(values []string) (app.Families, error) {
	if len(values) == 0 {
		return app.AllFamilies(), nil
	}

	var families app.Families
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "ipv4", "v4", "4":
			families.IPv4 = true
		case "ipv6", "v6", "6":
			families.IPv6 = true
		case "":
			continue
		default:
			return app.Families{}, fmt.Errorf("unsupported family %q", value)
		}
	}
	if !families.IPv4 && !families.IPv6 {
		return app.Families{}, fmt.Errorf("at least one family is required")
	}
	return families, nil
}

func parseVerify(value string) (app.VerifyMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(app.VerifyAuto):
		return app.VerifyAuto, nil
	case string(app.VerifyOff):
		return app.VerifyOff, nil
	case string(app.VerifyUpdaterAPI):
		return app.VerifyUpdaterAPI, nil
	default:
		return "", fmt.Errorf("unsupported verify mode %q", value)
	}
}

func validateVerifySupport(jobName, updaterName string, u updater.Updater, verify app.VerifyMode) error {
	if verify != app.VerifyUpdaterAPI {
		return nil
	}
	if _, ok := u.(updater.RecordReader); !ok {
		return fmt.Errorf("job %q updater %q does not support verify mode %q", jobName, updaterName, verify)
	}
	return nil
}
