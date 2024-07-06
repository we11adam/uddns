package netif

import (
	"errors"
	"fmt"
	"net"

	"github.com/spf13/viper"
	"github.com/we11adam/uddns/provider"
)

type Netif struct {
	iface *net.Interface
}

type Config struct {
	Name string `mapstructure:"name"`
}

func init() {
	provider.Register("NetworkInterface", func(v *viper.Viper) (provider.Provider, error) {
		cfg := Config{}
		err := v.UnmarshalKey("providers.netif", &cfg)
		if err != nil {
			return nil, err
		}
		if cfg.Name == "" {
			return nil, errors.New("[NetworkInterface] missing required fields")
		}
		return New(&cfg)
	})
}

func New(cfg *Config) (*Netif, error) {
	iface, err := net.InterfaceByName(cfg.Name)
	if err != nil {
		return nil, err
	}
	return &Netif{
		iface: iface,
	}, nil
}

func (n *Netif) GetIPs() (*provider.IpResult, error) {
	addrs, err := n.iface.Addrs()
	if err != nil {
		return nil, err
	}

	result := &provider.IpResult{}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipv4 := ipnet.IP.To4(); ipv4 != nil {
				result.IPv4 = ipv4.String()
			} else if ipnet.IP.To16() != nil {
				result.IPv6 = ipnet.IP.String()
			}
		}
	}

	if result.IPv4 == "" && result.IPv6 == "" {
		return nil, fmt.Errorf("[NetworkInterface] no IP address found on network interface %s", n.iface.Name)
	}

	return result, nil
}
