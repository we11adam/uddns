package provider

import (
	"context"

	"github.com/we11adam/uddns/internal/registry"
)

type IpResult struct {
	IPv4 string
	IPv6 string
}

type FamilyRequest struct {
	IPv4 bool
	IPv6 bool
}

type Provider interface {
	GetIPs(context.Context, FamilyRequest) (*IpResult, error)
}

type ConfigReader = registry.ConfigReader

type constructor = registry.Constructor[Provider]

var ErrNotConfigured = registry.ErrNotConfigured

var providers = registry.New[Provider]("provider", "providers.use")

func Register(name, configKey string, constructor constructor) {
	providers.Register(name, configKey, constructor)
}

func GetProvider(config ConfigReader) (string, Provider, error) {
	return providers.Get(config)
}

// Instance is one configured provider below the providers section.
type Instance = registry.Instance

// Instances returns the configured provider instances.
func Instances(config ConfigReader) ([]Instance, error) {
	return providers.Instances(config)
}

// Find resolves a provider instance name or implementation alias.
func Find(instances []Instance, selector string) (Instance, error) {
	return providers.Find(instances, selector)
}

// Select resolves provider selectors in order, or returns the first configured
// instance when there are no selectors.
func Select(instances []Instance, selectors []string) ([]Instance, error) {
	return providers.Select(instances, selectors)
}

// Build constructs an instance from a config that exposes its settings below
// instance.ConfigKey.
func Build(instance Instance, config ConfigReader) (Provider, error) {
	return providers.Build(instance, config)
}
