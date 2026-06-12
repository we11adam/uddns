package updater

import (
	"github.com/we11adam/uddns/internal/registry"
	"github.com/we11adam/uddns/provider"
)

type Updater interface {
	Update(ips *provider.IpResult) error
}

type RecordReader interface {
	Current() (*provider.IpResult, error)
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
