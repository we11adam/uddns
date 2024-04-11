package updater

import (
	"github.com/spf13/viper"
)

type Updater interface {
	Update(ip string) error
}

var Updaters = make(map[string]func(v *viper.Viper) (Updater, error))

func Register(name string, constructor func(v *viper.Viper) (Updater, error)) {
	Updaters[name] = constructor
}
