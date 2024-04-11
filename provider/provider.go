package provider

import (
	"github.com/spf13/viper"
)

type Provider interface {
	Ip() (string, error)
}

var Providers = make(map[string]func(v *viper.Viper) (Provider, error))

func Register(name string, constructor func(v *viper.Viper) (Provider, error)) {
	Providers[name] = constructor
}
