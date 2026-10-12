package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/we11adam/uddns/app"
	"github.com/we11adam/uddns/internal/config"
	"github.com/we11adam/uddns/internal/registry"
	"github.com/we11adam/uddns/notifier"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"

	_ "github.com/we11adam/uddns/notifier/discord"
	_ "github.com/we11adam/uddns/notifier/telegram"
	_ "github.com/we11adam/uddns/provider/ip_service"
	_ "github.com/we11adam/uddns/provider/netif"
	_ "github.com/we11adam/uddns/provider/routeros"
	_ "github.com/we11adam/uddns/updater/aliyun"
	_ "github.com/we11adam/uddns/updater/cloudflare"
	_ "github.com/we11adam/uddns/updater/duckdns"
	_ "github.com/we11adam/uddns/updater/lightdns"
	_ "github.com/we11adam/uddns/updater/scaleway"
)

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
	return runWithIO(args, os.Stdout, os.Stderr, defaultCommandDependencies())
}

func runWithIO(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	dependencies commandDependencies,
) int {
	if len(args) > 0 {
		switch args[0] {
		case "config":
			configureLogger()
			return runConfigCommand(args[1:])
		case "version":
			return runVersionCommand(args[1:], stdout, stderr)
		case "self-update":
			ctx, stop := signal.NotifyContext(
				context.Background(),
				os.Interrupt,
				syscall.SIGTERM,
			)
			defer stop()
			return runSelfUpdateCommand(
				ctx,
				args[1:],
				stdout,
				stderr,
				dependencies,
			)
		}
	}

	configureLogger()
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
	providers, err := provider.Instances(cfg)
	if err != nil {
		return nil, fmt.Errorf("provider configuration error: %w", err)
	}
	updaters, err := updater.Instances(cfg)
	if err != nil {
		return nil, fmt.Errorf("updater configuration error: %w", err)
	}

	jobConfigs, ok, err := cfg.Jobs()
	if err != nil {
		return nil, fmt.Errorf("failed to read jobs: %w", err)
	}
	if !ok {
		return loadDefaultJobs(cfg, providers, updaters)
	}
	if len(jobConfigs) == 0 {
		return nil, fmt.Errorf("jobs must contain at least one job")
	}
	for _, key := range []string{"providers.use", "updaters.use"} {
		if cfg.IsSet(key) {
			slog.Warn("setting is ignored when jobs are configured; each job selects its own provider and updater", "setting", key)
		}
	}

	jobs := make([]app.Job, 0, len(jobConfigs))
	seen := map[string]struct{}{}
	warnedRecords := map[string]struct{}{}
	for i, jobConfig := range jobConfigs {
		job, updaterInstance, err := loadConfiguredJob(cfg, providers, updaters, jobConfig, i)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[job.Name]; exists {
			return nil, fmt.Errorf("job %q is duplicated", job.Name)
		}
		seen[job.Name] = struct{}{}
		if _, warned := warnedRecords[updaterInstance.Name]; !warned && hasRecordSettings(updaterInstance) {
			warnedRecords[updaterInstance.Name] = struct{}{}
			slog.Warn("updater domain and zone are ignored when jobs are configured; each job's record and zone are used instead", "updater", updaterInstance.Label())
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func loadDefaultJobs(cfg *config.Config, providers []provider.Instance, updaters []updater.Instance) ([]app.Job, error) {
	providerSelectors, err := cfg.StringList("providers.use")
	if err != nil {
		return nil, err
	}
	providerName, p, err := buildProviders(cfg, providers, providerSelectors)
	if err != nil {
		return nil, fmt.Errorf("no provider found: %w", err)
	}

	updaterSelectors, err := cfg.StringList("updaters.use")
	if err != nil {
		return nil, err
	}
	selectedUpdaters, err := updater.Select(updaters, updaterSelectors)
	if err != nil {
		return nil, fmt.Errorf("no updater found: %w", err)
	}
	warnUnselected("updater", "updaters.use", updaters, updaterSelectors)

	verify, err := parseVerify(cfg.Verify())
	if err != nil {
		return nil, fmt.Errorf("default job verify error: %w", err)
	}

	jobs := make([]app.Job, 0, len(selectedUpdaters))
	for _, instance := range selectedUpdaters {
		name := "default"
		if len(selectedUpdaters) > 1 {
			name = instance.Name
		}
		scoped := cfg.Scoped(instance.ConfigKey, instance.Settings)
		u, err := updater.Build(instance, scoped)
		if err != nil {
			return nil, fmt.Errorf("no updater found: %w", err)
		}
		if err := validateVerifySupport(name, instance.Label(), u, verify); err != nil {
			return nil, err
		}
		record := strings.TrimSpace(scoped.GetString(instance.ConfigKey + ".domain"))
		zone := strings.TrimSpace(scoped.GetString(instance.ConfigKey + ".zone"))

		slog.Info("job selected", "job", name, "provider", providerName, "updater", instance.Label(), "record", record, "zone", zone, "families", app.AllFamilies().String(), "verify", verify)
		jobs = append(jobs, app.NewJob(name, providerName, p, instance.Label(), u, record, zone, app.AllFamilies(), verify))
	}
	return jobs, nil
}

func loadConfiguredJob(cfg *config.Config, providers []provider.Instance, updaters []updater.Instance, jobConfig config.Job, index int) (app.Job, updater.Instance, error) {
	name := strings.TrimSpace(jobConfig.Name)
	if name == "" {
		name = fmt.Sprintf("job-%d", index+1)
	}
	providerSelectors := trimmedNonEmpty(jobConfig.Provider)
	if len(providerSelectors) == 0 {
		return app.Job{}, updater.Instance{}, fmt.Errorf("job %q missing provider", name)
	}
	if strings.TrimSpace(jobConfig.Updater) == "" {
		return app.Job{}, updater.Instance{}, fmt.Errorf("job %q missing updater", name)
	}
	record := strings.TrimSpace(jobConfig.Record)
	zone := strings.TrimSpace(jobConfig.Zone)
	if record == "" {
		return app.Job{}, updater.Instance{}, fmt.Errorf("job %q missing record", name)
	}

	families, err := parseFamilies(jobConfig.Families)
	if err != nil {
		return app.Job{}, updater.Instance{}, fmt.Errorf("job %q invalid families: %w", name, err)
	}
	verify, err := parseVerify(jobConfig.VerifyMode())
	if err != nil {
		return app.Job{}, updater.Instance{}, fmt.Errorf("job %q verify error: %w", name, err)
	}

	providerName, p, err := buildProviders(cfg, providers, providerSelectors)
	if err != nil {
		return app.Job{}, updater.Instance{}, fmt.Errorf("job %q provider error: %w", name, err)
	}

	instance, err := updater.Find(updaters, jobConfig.Updater)
	if err != nil {
		return app.Job{}, updater.Instance{}, fmt.Errorf("job %q updater error: %w", name, err)
	}
	settings, err := jobUpdaterSettings(instance, record, zone)
	if err != nil {
		return app.Job{}, updater.Instance{}, fmt.Errorf("job %q updater error: %w", name, err)
	}
	u, err := updater.Build(instance, cfg.Scoped(instance.ConfigKey, settings))
	if err != nil {
		return app.Job{}, updater.Instance{}, fmt.Errorf("job %q updater error: %w", name, err)
	}
	if err := validateVerifySupport(name, instance.Label(), u, verify); err != nil {
		return app.Job{}, updater.Instance{}, err
	}

	slog.Info("job selected", "job", name, "provider", providerName, "updater", instance.Label(), "record", record, "zone", zone, "families", families.String(), "verify", verify)
	return app.NewJob(name, providerName, p, instance.Label(), u, record, zone, families, verify), instance, nil
}

// buildProviders constructs the selected providers. Several providers form a
// chain that falls back in order for each address family.
func buildProviders(cfg *config.Config, instances []provider.Instance, selectors []string) (string, provider.Provider, error) {
	selected, err := provider.Select(instances, selectors)
	if err != nil {
		return "", nil, err
	}
	warnUnselected("provider", "providers.use", instances, selectors)

	names := make([]string, 0, len(selected))
	providers := make([]provider.Provider, 0, len(selected))
	for _, instance := range selected {
		p, err := provider.Build(instance, cfg.Scoped(instance.ConfigKey, instance.Settings))
		if err != nil {
			return "", nil, err
		}
		names = append(names, instance.Label())
		providers = append(providers, p)
	}
	if len(providers) == 1 {
		return names[0], providers[0], nil
	}

	chain, err := provider.NewChain(names, providers)
	if err != nil {
		return "", nil, err
	}
	return strings.Join(names, " -> "), chain, nil
}

func warnUnselected(kind, setting string, instances []registry.Instance, selectors []string) {
	if len(selectors) > 0 || len(instances) < 2 {
		return
	}
	ignored := make([]string, 0, len(instances)-1)
	for _, instance := range instances[1:] {
		ignored = append(ignored, instance.Label())
	}
	slog.Warn(
		"multiple "+kind+"s configured without a selection; only the first is used",
		"selected", instances[0].Label(),
		"ignored", strings.Join(ignored, ", "),
		"hint", "list the "+kind+"s to use in "+setting,
	)
}

// jobUpdaterSettings replaces the updater instance's record fields with the
// job's record and zone. An empty job zone clears an inherited zone because a
// zone only applies to the record it was written for.
func jobUpdaterSettings(instance updater.Instance, record, zone string) (map[string]any, error) {
	settings, ok := instance.Settings.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("updater %q settings must be a mapping", instance.Name)
	}
	settings = maps.Clone(settings)
	settings["domain"] = record
	settings["zone"] = zone
	return settings, nil
}

func hasRecordSettings(instance updater.Instance) bool {
	settings, ok := instance.Settings.(map[string]any)
	if !ok {
		return false
	}
	_, hasDomain := settings["domain"]
	_, hasZone := settings["zone"]
	return hasDomain || hasZone
}

func trimmedNonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
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
		return app.Families{}, errors.New("at least one family is required")
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
