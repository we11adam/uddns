package cloudflare

import (
	"context"
	"log/slog"
	"strings"

	"github.com/cloudflare/cloudflare-go"
	"github.com/spf13/viper"
	"github.com/we11adam/uddns/updater"
)

type Config struct {
	Email    string `mapstructure:"email"`
	APIKey   string `mapstructure:"apikey"`
	APIToken string `mapstructure:"apitoken"`
	Domain   string `mapstructure:"domain"`
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
	var (
		api *cloudflare.API
		err error
	)

	// If APIToken is provided, use it to create the API client
	if config.APIToken != "" {
		api, err = cloudflare.NewWithAPIToken(config.APIToken)
	} else {
		// Otherwise, use APIKey and Email to create the API client
		api, err = cloudflare.New(config.APIKey, config.Email)
	}

	// Check if there was an error creating the API client
	if err != nil {
		slog.Debug("[CloudFlare] failed to create API client:", "error", err)
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
		slog.Error("[CloudFlare] failed to list DNS records:", err)
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
			return err
		}
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
			slog.Debug("[CloudFlare] failed to create DNS record:", "error", err)
			return err
		}

		slog.Debug("[CloudFlare] DNS created successfully with new IP address:", "ip", newAddr)
	}
	return nil
}
