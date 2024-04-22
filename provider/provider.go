package provider

import (
	"github.com/spf13/viper"
)

type Provider interface {
	Ip() (string, error)
}

type constructor func(v *viper.Viper) (Provider, error)

var Providers = make(map[string]constructor)

func Register(name string, constructor constructor) {
	Providers[name] = constructor
}
