package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudflare/cloudflare-go"
	"github.com/we11adam/uddns/internal/dnsname"
	"github.com/we11adam/uddns/internal/httpbody"
	"github.com/we11adam/uddns/internal/proxyurl"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
)

const (
	defaultTTL      = 60
	recordTypeA     = "A"
	recordTypeAAAA  = "AAAA"
	requestTimeout  = 15 * time.Second
	retryBodyDrain  = 64 << 10
	responseBodyMax = 1 << 20
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
	if config == nil {
		return nil, fmt.Errorf("Cloudflare config is nil")
	}
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

	var api *cloudflare.API
	httpClient := newHTTPClient(nil)

	if config.Proxy != "" {
		proxy, err := proxyurl.Parse(config.Proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid Cloudflare proxy configuration: %w", err)
		}
		slog.Info("using proxy", "updater", "cloudflare", "proxy", proxyLogValue(proxy))
		httpClient = newHTTPClient(proxy)
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

func newHTTPClient(proxy *url.URL) *http.Client {
	var transport http.RoundTripper = http.DefaultTransport
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		clone := defaultTransport.Clone()
		if proxy != nil {
			clone.Proxy = http.ProxyURL(proxy)
		}
		transport = clone
	} else if proxy != nil {
		transport = &http.Transport{Proxy: http.ProxyURL(proxy)}
	}
	if transport == nil {
		transport = &http.Transport{}
	}
	return &http.Client{
		Transport: retryResponseBodyClosingTransport{base: transport},
		Timeout:   requestTimeout,
	}
}

type retryResponseBodyClosingTransport struct {
	base http.RoundTripper
}

func (t retryResponseBodyClosingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if response == nil || response.Body == nil {
		return response, err
	}
	if !isRetryableStatus(response.StatusCode) {
		response.Body = httpbody.Limit(response.Body, responseBodyMax)
		return response, err
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, retryBodyDrain))
	_ = response.Body.Close()
	response.Body = http.NoBody
	return response, err
}

func isRetryableStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
}

func (c *Cloudflare) Update(ctx context.Context, ips *provider.IpResult) error {
	if err := c.ensureZoneID(ctx); err != nil {
		return err
	}

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

func (c *Cloudflare) Current(ctx context.Context, families provider.FamilyRequest) (*provider.IpResult, error) {
	if !families.IPv4 && !families.IPv6 {
		return nil, fmt.Errorf("no IP families requested")
	}
	if err := c.ensureZoneID(ctx); err != nil {
		return nil, err
	}
	result := &provider.IpResult{}

	if families.IPv4 {
		ipv4, err := c.currentDNSRecord(ctx, recordTypeA)
		if err != nil {
			return nil, fmt.Errorf("failed to get Cloudflare IPv4 record: %w", err)
		}
		result.IPv4 = ipv4
	}

	if families.IPv6 {
		ipv6, err := c.currentDNSRecord(ctx, recordTypeAAAA)
		if err != nil {
			return nil, fmt.Errorf("failed to get Cloudflare IPv6 record: %w", err)
		}
		result.IPv6 = ipv6
	}

	return result, nil
}

func (c *Cloudflare) ensureZoneID(ctx context.Context) error {
	if c.zoneID != "" {
		return nil
	}

	zone, _, err := dnsname.SplitRecord(c.config.Domain, c.config.Zone)
	if err != nil {
		return fmt.Errorf("invalid Cloudflare DNS record: %w", err)
	}
	zoneID, err := c.zoneIDByName(ctx, zone)
	if err != nil {
		c.zoneID = ""
		return fmt.Errorf("failed to get Cloudflare zone ID for zone %s: %w", zone, err)
	}

	c.zoneID = zoneID
	slog.Debug("zone ID retrieved", "updater", "cloudflare", "record", c.config.Domain, "zone", zone, "zone_id", zoneID)
	return nil
}

func (c *Cloudflare) zoneIDByName(ctx context.Context, zone string) (string, error) {
	response, err := c.client.ListZonesContext(ctx, cloudflare.WithZoneFilters(zone, "", ""))
	if err != nil {
		return "", fmt.Errorf("ListZonesContext command failed: %w", err)
	}

	switch len(response.Result) {
	case 0:
		return "", errors.New("zone could not be found")
	case 1:
		return response.Result[0].ID, nil
	default:
		return "", errors.New("ambiguous zone name; an account ID might help")
	}
}

func (c *Cloudflare) updateDNSRecord(ctx context.Context, recordType, ip string) error {
	domain := c.config.Domain

	params := cloudflare.ListDNSRecordsParams{Type: recordType, Name: domain}
	dnsRecords, _, err := c.client.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(c.zoneID), params)
	if err != nil {
		return fmt.Errorf("failed to list Cloudflare DNS records: %w", err)
	}

	if len(dnsRecords) > 0 {
		updated := false
		for _, record := range dnsRecords {
			if record.Content == ip {
				continue
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
				return fmt.Errorf("failed to update Cloudflare DNS record %s: %w", record.ID, err)
			}
			updated = true
			slog.Info("updated DNS record", "updater", "cloudflare", "record", domain, "record_type", recordType, "ip", ip, "record_id", record.ID)
		}
		if !updated {
			slog.Debug("skipping current DNS records", "updater", "cloudflare", "record", domain, "record_type", recordType, "ip", ip)
		}
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
		slog.Info("created DNS record", "updater", "cloudflare", "record", domain, "record_type", recordType, "ip", ip)
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

	value := dnsRecords[0].Content
	for _, record := range dnsRecords[1:] {
		if record.Content != value {
			return "", nil
		}
	}
	return value, nil
}

func proxyLogValue(proxy *url.URL) string {
	if proxy == nil {
		return ""
	}
	if proxy.Scheme == "" {
		return proxy.Host
	}
	return proxy.Scheme + "://" + proxy.Host
}
