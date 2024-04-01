package updater

import (
	"fmt"
	"github.com/spf13/viper"
)

type Updater interface {
	Update(ip string) error
}

var Updaters = make(map[string]func(v *viper.Viper) (Updater, error))

func Register(name string, constructor func(v *viper.Viper) (Updater, error)) {
	fmt.Print("Registering provider: ", name, "\n")
	Updaters[name] = constructor
}
