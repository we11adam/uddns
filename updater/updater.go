package updater

import (
	"context"

	"github.com/we11adam/uddns/internal/registry"
	"github.com/we11adam/uddns/provider"
)

type Updater interface {
	Update(ctx context.Context, ips *provider.IpResult) error
}

type RecordReader interface {
	Current(ctx context.Context) (*provider.IpResult, error)
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
