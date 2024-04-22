package updater

import (
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
