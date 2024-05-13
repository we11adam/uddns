//go:build !windows

package netif

import (
	"errors"
	"fmt"
	"github.com/spf13/viper"
	"github.com/we11adam/uddns/provider"
	"net"
)

type Netif struct {
	iface *net.Interface
}

type Config struct {
	Name string `mapstructure:"name"`
}

func init() {
	provider.Register("NetworkIneterface", func(v *viper.Viper) (provider.Provider, error) {
		cfg := Config{}
		err := v.UnmarshalKey("providers.netif", &cfg)
		if err != nil {
			return nil, err
		}
		if cfg.Name == "" {
			return nil, errors.New("[NetworkIneterface] missing required fields")
		}
		return New(&cfg)
	})
}

func New(cfg *Config) (provider.Provider, error) {
	iface, err := net.InterfaceByName(cfg.Name)
	if err != nil {
		return nil, err
	}
	return &Netif{
		iface: iface,
	}, nil
}

func (n *Netif) Ip() (string, error) {
	addrs, err := n.iface.Addrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}

	return "", fmt.Errorf("[NetworkIneterface] no IP address found on network interface %s", n.iface.Name)
}
