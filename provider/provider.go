package provider

import (
	"fmt"
	"github.com/spf13/viper"
)

type Provider interface {
	Ip() (string, error)
}

var Providers = make(map[string]func(v *viper.Viper) (Provider, error))

func Register(name string, constructor func(v *viper.Viper) (Provider, error)) {
	fmt.Print("Registering provider: ", name, "\n")
	Providers[name] = constructor
}
