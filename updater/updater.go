package updater

import (
	"fmt"

	"github.com/spf13/viper"
	"github.com/we11adam/uddns/provider"
)

type Updater interface {
	Update(ips *provider.IpResult) error
}

type constructor func(v *viper.Viper) (Updater, error)

var Updaters = make(map[string]constructor)

func Register(name string, constructor constructor) {
	Updaters[name] = constructor
}

func GetUpdater(v *viper.Viper) (string, Updater, error) {
	for n, c := range Updaters {
		u, err := c(v)
		if err == nil {
			return n, u, nil
		}
	}

	return "", nil, fmt.Errorf("no updater can be initialized")
}
