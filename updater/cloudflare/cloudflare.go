package cloudflare

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudflare/cloudflare-go"
	"github.com/spf13/viper"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
)

const (
	defaultTTL     = 60
	recordTypeA    = "A"
	recordTypeAAAA = "AAAA"
)

type Config struct {
	Email    string `mapstructure:"email"`
	APIKey   string `mapstructure:"apikey"`
	APIToken string `mapstructure:"apitoken"`
	Domain   string `mapstructure:"domain"`
	Proxy    string `mapstructure:"proxy"`
}

type Cloudflare struct {
	config *Config
	client *cloudflare.API
	zoneID string
}

func init() {
	updater.Register("Cloudflare", func(v *viper.Viper) (updater.Updater, error) {
		cfg := Config{}
		err := v.UnmarshalKey("updaters.cloudflare", &cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal Cloudflare config: %w", err)
		}
		return New(&cfg)
	})
}

func New(config *Config) (*Cloudflare, error) {
	if config.Domain == "" {
		return nil, fmt.Errorf("Cloudflare Domain is not set in the configuration")
	}

	var (
		api        *cloudflare.API
		httpClient *http.Client
		err        error
	)

	if config.Proxy == "" {
		httpClient = &http.Client{}
	} else {
		proxy, err := url.Parse(config.Proxy)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Cloudflare proxy URL: %w", err)
		}
		slog.Info("[Cloudflare] Using proxy", "proxy", config.Proxy)
		httpClient = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(proxy),
			},
		}
	}

	if config.APIToken != "" {
		slog.Debug("[Cloudflare] Using API Token for authentication")
		api, err = cloudflare.NewWithAPIToken(config.APIToken, cloudflare.HTTPClient(httpClient))
	} else if config.APIKey != "" && config.Email != "" {
		slog.Debug("[Cloudflare] Using API Key and Email for authentication")
		api, err = cloudflare.New(config.APIKey, config.Email, cloudflare.HTTPClient(httpClient))
	} else {
		return nil, fmt.Errorf("Cloudflare configuration error: either APIToken or both APIKey and Email must be provided")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create Cloudflare API client: %w", err)
	}

	return &Cloudflare{
		config: config,
		client: api,
	}, nil
}

func (c *Cloudflare) Update(ips *provider.IpResult) error {
	if c.zoneID == "" {
		domain := c.config.Domain
		parts := strings.Split(domain, ".")
		if len(parts) < 2 {
			return fmt.Errorf("invalid Cloudflare domain format: %s", domain)
		}

		l := len(parts)
		zone := parts[l-2] + "." + parts[l-1]
		zoneID, err := c.client.ZoneIDByName(zone)
		if err != nil {
			c.zoneID = ""
			return fmt.Errorf("failed to get Cloudflare zone ID for domain %s: %w", domain, err)
		}

		c.zoneID = zoneID
		slog.Debug("[Cloudflare] Zone ID retrieved", "domain", domain, "zoneID", zoneID)
	}

	ctx := context.Background()

	if ips.IPv4 != "" {
		if !isValidIPv4(ips.IPv4) {
			return fmt.Errorf("invalid IPv4 address: %s", ips.IPv4)
		}
		if err := c.updateDNSRecord(ctx, recordTypeA, ips.IPv4); err != nil {
			return fmt.Errorf("failed to update Cloudflare IPv4 record: %w", err)
		}
	}

	if ips.IPv6 != "" {
		if !isValidIPv6(ips.IPv6) {
			return fmt.Errorf("invalid IPv6 address: %s", ips.IPv6)
		}
		if err := c.updateDNSRecord(ctx, recordTypeAAAA, ips.IPv6); err != nil {
			return fmt.Errorf("failed to update Cloudflare IPv6 record: %w", err)
		}
	}

	return nil
}

func isValidIPv4(ip string) bool {
	return net.ParseIP(ip) != nil && strings.Contains(ip, ".")
}

func isValidIPv6(ip string) bool {
	return net.ParseIP(ip) != nil && strings.Contains(ip, ":")
}

func (c *Cloudflare) updateDNSRecord(ctx context.Context, recordType, ip string) error {
	domain := c.config.Domain

	params := cloudflare.ListDNSRecordsParams{Type: recordType, Name: domain}
	dnsRecords, _, err := c.client.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(c.zoneID), params)
	if err != nil {
		return fmt.Errorf("failed to list Cloudflare DNS records: %w", err)
	}

	if len(dnsRecords) > 0 {
		record := dnsRecords[0]

		if record.Content == ip {
			slog.Debug("[Cloudflare] DNS record is already up to date", "type", recordType, "ip", ip)
			return nil
		}

		updateParams := cloudflare.UpdateDNSRecordParams{
			ID:      record.ID,
			Type:    recordType,
			Name:    domain,
			Content: ip,
			TTL:     record.TTL,
			Proxied: record.Proxied,
		}

		_, err := c.client.UpdateDNSRecord(ctx, cloudflare.ZoneIdentifier(c.zoneID), updateParams)
		if err != nil {
			return fmt.Errorf("failed to update Cloudflare DNS record: %w", err)
		}
		slog.Info("[Cloudflare] DNS record updated successfully", "type", recordType, "ip", ip)
	} else {
		createParams := cloudflare.CreateDNSRecordParams{
			Type:    recordType,
			Name:    domain,
			Content: ip,
			TTL:     defaultTTL,
			Proxied: cloudflare.BoolPtr(false),
		}

		_, err := c.client.CreateDNSRecord(ctx, cloudflare.ZoneIdentifier(c.zoneID), createParams)
		if err != nil {
			return fmt.Errorf("failed to create Cloudflare DNS record: %w", err)
		}
		slog.Info("[Cloudflare] DNS record created successfully", "type", recordType, "ip", ip)
	}

	return nil
}
