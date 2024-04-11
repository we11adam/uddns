package cloudflare

import (
	"context"
	"fmt"
	"github.com/cloudflare/cloudflare-go"
	"github.com/spf13/viper"
	"github.com/we11adam/uddns/updater"
	"os"
	"strings"
)

type Config struct {
	Email  string `mapstructure:"email"`
	APIKey string `mapstructure:"apikey"`
	Domain string `mapstructure:"domain"`
}

type Cloudflare struct {
	config *Config
	client *cloudflare.API
}

func init() {
	updater.Register("Cloudflare", func(v *viper.Viper) (updater.Updater, error) {
		cfg := Config{}
		err := v.UnmarshalKey("updaters.cloudflare", &cfg)
		if err != nil {
			return nil, err
		}
		return New(&cfg)
	})
}

func New(config *Config) (updater.Updater, error) {
	api, err := cloudflare.New(config.APIKey, config.Email)
	if err != nil {
		fmt.Println("Error creating Cloudflare API client: ", err)
		return nil, err
	}

	return &Cloudflare{
		config: config,
		client: api,
	}, nil
}

func (c *Cloudflare) Update(newAddr string) error {

	domain := c.config.Domain
	parts := strings.Split(domain, ".")
	l := len(parts)
	zone := parts[l-2] + "." + parts[l-1]
	zoneID, err := c.client.ZoneIDByName(zone)
	if err != nil {
		return err
	}

	params := cloudflare.ListDNSRecordsParams{Name: domain}
	dnsRecords, _, err := c.client.ListDNSRecords(context.Background(), cloudflare.ZoneIdentifier(zoneID), params)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error fetching DNS records: ", err)
		return err
	}

	if len(dnsRecords) > 0 {
		rr := cloudflare.UpdateDNSRecordParams{}
		for _, r := range dnsRecords {
			if r.Name == domain {
				rr.ID = r.ID
				rr.Type = "A"
				rr.Content = newAddr
				rr.TTL = r.TTL
				rr.Proxied = r.Proxied
				rr.Priority = r.Priority
				break
			}
		}

		_, err := c.client.UpdateDNSRecord(context.Background(), cloudflare.ZoneIdentifier(zoneID), rr)
		if err != nil {
			fmt.Println("Error updating DNS record:", err)
			return err
		}

		fmt.Println("DNS updated successfully with new IP address: ", newAddr)
	} else {
		proxied := false
		priority := uint16(10)
		rr := cloudflare.CreateDNSRecordParams{
			Name:     domain,
			Type:     "A",
			Content:  newAddr,
			TTL:      60,
			Proxied:  &proxied,
			Priority: &priority,
		}

		_, err := c.client.CreateDNSRecord(context.Background(), cloudflare.ZoneIdentifier(zoneID), rr)
		if err != nil {
			fmt.Println("Error creating DNS record:", err)
			return err
		}

		fmt.Println("DNS created successfully with new IP address: ", newAddr)
	}
	return nil
}
