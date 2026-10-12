package updater

import (
	"context"

	"github.com/we11adam/uddns/internal/registry"
	"github.com/we11adam/uddns/provider"
)

// Updater writes the detected addresses to one DNS record.
//
// Implementations register a constructor with Register from an init function.
// The constructor reads its settings below its config key, for example
// "updaters.cloudflare", and must return ErrNotConfigured when that key is not
// set. Settings must use the common "domain" field for the DNS record and, when
// the DNS service has zones, the optional "zone" field: in jobs mode UDDNS
// replaces both with the job's record and zone.
type Updater interface {
	Update(ctx context.Context, ips *provider.IpResult) error
}

// RecordReader is implemented by updaters that can read the current DNS record
// through their DNS service API, enabling the updater_api verify mode.
type RecordReader interface {
	Current(ctx context.Context, families provider.FamilyRequest) (*provider.IpResult, error)
}

type ConfigReader = registry.ConfigReader

type constructor = registry.Constructor[Updater]

var ErrNotConfigured = registry.ErrNotConfigured

var updaters = registry.New[Updater]("updater", "updaters.use")

func Register(name, configKey string, constructor constructor) {
	updaters.Register(name, configKey, constructor)
}

func GetUpdater(config ConfigReader) (string, Updater, error) {
	return updaters.Get(config)
}

// Instance is one configured updater below the updaters section.
type Instance = registry.Instance

// Instances returns the configured updater instances.
func Instances(config ConfigReader) ([]Instance, error) {
	return updaters.Instances(config)
}

// Find resolves a updater instance name or implementation alias.
func Find(instances []Instance, selector string) (Instance, error) {
	return updaters.Find(instances, selector)
}

// Select resolves updater selectors in order, or returns the first configured
// instance when there are no selectors.
func Select(instances []Instance, selectors []string) ([]Instance, error) {
	return updaters.Select(instances, selectors)
}

// Build constructs an instance from a config that exposes its settings below
// instance.ConfigKey.
func Build(instance Instance, config ConfigReader) (Updater, error) {
	return updaters.Build(instance, config)
}
