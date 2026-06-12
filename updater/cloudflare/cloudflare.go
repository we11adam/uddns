package cloudflare

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/cloudflare/cloudflare-go"
	"github.com/we11adam/uddns/internal/dnsname"
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
	Zone     string `mapstructure:"zone"`
	Proxy    string `mapstructure:"proxy"`
}

type Cloudflare struct {
	config *Config
	client *cloudflare.API
	zoneID string
}

func init() {
	updater.Register("Cloudflare", "updaters.cloudflare", func(v updater.ConfigReader) (updater.Updater, error) {
		if !v.IsSet("updaters.cloudflare") {
			return nil, updater.ErrNotConfigured
		}

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
		return nil, fmt.Errorf("Cloudflare domain is not set in the configuration")
	}
	domain, err := dnsname.Normalize(config.Domain)
	if err != nil {
		return nil, fmt.Errorf("invalid Cloudflare domain: %w", err)
	}
	config.Domain = domain
	if config.Zone != "" {
		zone, err := dnsname.Normalize(config.Zone)
		if err != nil {
			return nil, fmt.Errorf("invalid Cloudflare zone: %w", err)
		}
		config.Zone = zone
	}
	if _, _, err := dnsname.SplitRecord(config.Domain, config.Zone); err != nil {
		return nil, fmt.Errorf("invalid Cloudflare DNS record: %w", err)
	}

	var (
		api        *cloudflare.API
		httpClient *http.Client
	)

	if config.Proxy != "" {
		proxy, err := url.Parse(config.Proxy)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Cloudflare proxy URL: %w", err)
		}
		slog.Info("using proxy", "updater", "cloudflare", "proxy", config.Proxy)
		httpClient = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(proxy),
			},
		}
	}

	if config.APIToken != "" {
		slog.Debug("using API token for authentication", "updater", "cloudflare")
		api, err = cloudflare.NewWithAPIToken(config.APIToken, cloudflare.HTTPClient(httpClient))
	} else if config.APIKey != "" && config.Email != "" {
		slog.Debug("using API key and email for authentication", "updater", "cloudflare")
		api, err = cloudflare.New(config.APIKey, config.Email, cloudflare.HTTPClient(httpClient))
	} else {
		return nil, fmt.Errorf("Cloudflare configuration error: either API token or both API key and email must be provided")
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
	if err := c.ensureZoneID(); err != nil {
		return err
	}

	ctx := context.Background()

	if ips.IPv4 != "" {
		if !provider.IsValidIPv4(ips.IPv4) {
			return fmt.Errorf("invalid IPv4 address: %s", ips.IPv4)
		}
		if err := c.updateDNSRecord(ctx, recordTypeA, ips.IPv4); err != nil {
			return fmt.Errorf("failed to update Cloudflare IPv4 record: %w", err)
		}
	}

	if ips.IPv6 != "" {
		if !provider.IsValidIPv6(ips.IPv6) {
			return fmt.Errorf("invalid IPv6 address: %s", ips.IPv6)
		}
		if err := c.updateDNSRecord(ctx, recordTypeAAAA, ips.IPv6); err != nil {
			return fmt.Errorf("failed to update Cloudflare IPv6 record: %w", err)
		}
	}

	return nil
}

func (c *Cloudflare) Current() (*provider.IpResult, error) {
	if err := c.ensureZoneID(); err != nil {
		return nil, err
	}

	ctx := context.Background()
	result := &provider.IpResult{}

	ipv4, err := c.currentDNSRecord(ctx, recordTypeA)
	if err != nil {
		return nil, fmt.Errorf("failed to get Cloudflare IPv4 record: %w", err)
	}
	result.IPv4 = ipv4

	ipv6, err := c.currentDNSRecord(ctx, recordTypeAAAA)
	if err != nil {
		return nil, fmt.Errorf("failed to get Cloudflare IPv6 record: %w", err)
	}
	result.IPv6 = ipv6

	return result, nil
}

func (c *Cloudflare) ensureZoneID() error {
	if c.zoneID != "" {
		return nil
	}

	zone, _, err := dnsname.SplitRecord(c.config.Domain, c.config.Zone)
	if err != nil {
		return fmt.Errorf("invalid Cloudflare DNS record: %w", err)
	}
	zoneID, err := c.client.ZoneIDByName(zone)
	if err != nil {
		c.zoneID = ""
		return fmt.Errorf("failed to get Cloudflare zone ID for zone %s: %w", zone, err)
	}

	c.zoneID = zoneID
	slog.Debug("zone ID retrieved", "updater", "cloudflare", "domain", c.config.Domain, "zone", zone, "zone_id", zoneID)
	return nil
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
			slog.Debug("skipping current DNS record", "updater", "cloudflare", "record_type", recordType, "ip", ip)
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
		slog.Info("updated DNS record", "updater", "cloudflare", "record_type", recordType, "ip", ip)
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
		slog.Info("created DNS record", "updater", "cloudflare", "record_type", recordType, "ip", ip)
	}

	return nil
}

func (c *Cloudflare) currentDNSRecord(ctx context.Context, recordType string) (string, error) {
	domain := c.config.Domain

	params := cloudflare.ListDNSRecordsParams{Type: recordType, Name: domain}
	dnsRecords, _, err := c.client.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(c.zoneID), params)
	if err != nil {
		return "", fmt.Errorf("failed to list Cloudflare DNS records: %w", err)
	}
	if len(dnsRecords) == 0 {
		return "", nil
	}

	return dnsRecords[0].Content, nil
}
