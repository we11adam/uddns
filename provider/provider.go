package provider

import (
	"context"

	"github.com/we11adam/uddns/internal/registry"
)

type IpResult struct {
	IPv4 string
	IPv6 string
}

type Provider interface {
	GetIPs(context.Context) (*IpResult, error)
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
