package provider

import (
	"errors"
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

func GetProvider(v *viper.Viper) (string, Provider, error) {
	for n, c := range Providers {
		p, err := c(v)
		if err == nil {
			return n, p, nil
		}
	}

	return "", nil, errors.New("no provider can be initialized")
}
