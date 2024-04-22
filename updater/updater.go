package updater

import (
	"errors"
	"github.com/spf13/viper"
)

type Updater interface {
	Update(ip string) error
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

	return "", nil, errors.New("no updater can be initialized")
}
