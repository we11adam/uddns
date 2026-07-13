package aliyun

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/alidns"
	"github.com/we11adam/uddns/internal/dnsname"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
)

const (
	defaultRegionID = "cn-hangzhou"
	connectTimeout  = 5 * time.Second
	recordTypeA     = "A"
	recordTypeAAAA  = "AAAA"
)

type Config struct {
	AccessKeyID     string `mapstructure:"accesskeyid"`
	AccessKeySecret string `mapstructure:"accesskeysecret"`
	RegionID        string `mapstructure:"regionid"`
	Domain          string `mapstructure:"domain"`
	Zone            string `mapstructure:"zone"`
}

type Aliyun struct {
	config    *Config
	client    *alidns.Client
	transport http.RoundTripper
}

type contextRoundTripper struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t contextRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(request.WithContext(t.ctx))
}

func init() {
	updater.Register("Aliyun", "updaters.aliyun", func(v updater.ConfigReader) (updater.Updater, error) {
		if !v.IsSet("updaters.aliyun") {
			return nil, updater.ErrNotConfigured
		}

		cfg := Config{}
		err := v.UnmarshalKey("updaters.aliyun", &cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal Aliyun config: %w", err)
		}
		return New(&cfg)
	})
}

func New(config *Config) (*Aliyun, error) {
	if config.AccessKeyID == "" {
		return nil, fmt.Errorf("Aliyun access key ID is not set in the configuration")
	}
	if config.AccessKeySecret == "" {
		return nil, fmt.Errorf("Aliyun access key secret is not set in the configuration")
	}
	if config.Domain == "" {
		return nil, fmt.Errorf("Aliyun domain is not set in the configuration")
	}
	domain, err := dnsname.Normalize(config.Domain)
	if err != nil {
		return nil, fmt.Errorf("invalid Aliyun domain: %w", err)
	}
	config.Domain = domain
	if config.Zone != "" {
		zone, err := dnsname.Normalize(config.Zone)
		if err != nil {
			return nil, fmt.Errorf("invalid Aliyun zone: %w", err)
		}
		config.Zone = zone
	}
	if _, _, err := dnsname.SplitRecord(config.Domain, config.Zone); err != nil {
		return nil, fmt.Errorf("invalid Aliyun DNS record: %w", err)
	}

	if config.RegionID == "" {
		config.RegionID = "cn-hangzhou"
		slog.Debug("region ID not set, using default", "updater", "aliyun", "region_id", config.RegionID)
	}

	client, err := newClient(config, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Aliyun API client: %w", err)
	}

	return &Aliyun{
		config:    config,
		client:    client,
		transport: defaultTransport(),
	}, nil
}

func newClient(config *Config, transport http.RoundTripper) (*alidns.Client, error) {
	sdkConfig := sdk.NewConfig().WithScheme(requests.HTTPS)
	sdkConfig.Transport = transport
	credential := credentials.NewAccessKeyCredential(config.AccessKeyID, config.AccessKeySecret)
	return alidns.NewClientWithOptions(config.RegionID, sdkConfig, credential)
}

func (a *Aliyun) clientWithContext(ctx context.Context) (*alidns.Client, error) {
	transport := a.transport
	if transport == nil {
		transport = defaultTransport()
	}
	return newClient(a.config, contextRoundTripper{ctx: ctx, base: transport})
}

func defaultTransport() http.RoundTripper {
	return &http.Transport{
		Proxy:       http.ProxyFromEnvironment,
		DialContext: sdk.Timeout(connectTimeout),
	}
}

func (a *Aliyun) Update(ctx context.Context, ips *provider.IpResult) error {
	client, err := a.clientWithContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Aliyun API client: %w", err)
	}

	if ips.IPv4 != "" {
		if !provider.IsValidIPv4(ips.IPv4) {
			return fmt.Errorf("invalid IPv4 address: %s", ips.IPv4)
		}
		if err := a.updateDNSRecord(ctx, client, recordTypeA, ips.IPv4); err != nil {
			return fmt.Errorf("failed to update IPv4 record: %w", err)
		}
	}

	if ips.IPv6 != "" {
		if !provider.IsValidIPv6(ips.IPv6) {
			return fmt.Errorf("invalid IPv6 address: %s", ips.IPv6)
		}
		if err := a.updateDNSRecord(ctx, client, recordTypeAAAA, ips.IPv6); err != nil {
			return fmt.Errorf("failed to update IPv6 record: %w", err)
		}
	}

	return nil
}

func (a *Aliyun) Current(ctx context.Context) (*provider.IpResult, error) {
	client, err := a.clientWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create Aliyun API client: %w", err)
	}
	result := &provider.IpResult{}

	ipv4, err := a.currentDNSRecord(ctx, client, recordTypeA)
	if err != nil {
		return nil, fmt.Errorf("failed to get Aliyun IPv4 record: %w", err)
	}
	result.IPv4 = ipv4

	ipv6, err := a.currentDNSRecord(ctx, client, recordTypeAAAA)
	if err != nil {
		return nil, fmt.Errorf("failed to get Aliyun IPv6 record: %w", err)
	}
	result.IPv6 = ipv6

	return result, nil
}

func (a *Aliyun) updateDNSRecord(ctx context.Context, client *alidns.Client, recordType, ip string) error {
	domain := a.config.Domain
	domainName, rr, err := dnsname.SplitRecord(domain, a.config.Zone)
	if err != nil {
		return fmt.Errorf("invalid Aliyun DNS record: %w", err)
	}

	request := alidns.CreateDescribeSubDomainRecordsRequest()
	request.SubDomain = domain
	request.Type = recordType

	if err := ctx.Err(); err != nil {
		return err
	}
	response, err := client.DescribeSubDomainRecords(request)
	if err != nil {
		return fmt.Errorf("failed to get DNS records: %w", err)
	}

	if response.TotalCount > 0 && len(response.DomainRecords.Record) > 0 {
		existingRecord := response.DomainRecords.Record[0]

		if existingRecord.Value == ip {
			slog.Debug("skipping current DNS record", "updater", "aliyun", "record", domain, "record_type", recordType, "ip", ip)
			return nil
		}

		updateRequest := alidns.CreateUpdateDomainRecordRequest()
		updateRequest.RecordId = existingRecord.RecordId
		updateRequest.RR = rr
		updateRequest.Type = recordType
		updateRequest.Value = ip

		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := client.UpdateDomainRecord(updateRequest)
		if err != nil {
			return fmt.Errorf("failed to update DNS record: %w", err)
		}

		slog.Info("updated DNS record", "updater", "aliyun", "record", domain, "record_type", recordType, "ip", ip, "record_id", existingRecord.RecordId)
	} else {
		addRequest := alidns.CreateAddDomainRecordRequest()
		addRequest.DomainName = domainName
		addRequest.RR = rr
		addRequest.Type = recordType
		addRequest.Value = ip

		if err := ctx.Err(); err != nil {
			return err
		}
		response, err := client.AddDomainRecord(addRequest)
		if err != nil {
			return fmt.Errorf("failed to add DNS record: %w", err)
		}

		slog.Info("added DNS record", "updater", "aliyun", "record", domain, "record_type", recordType, "ip", ip, "record_id", response.RecordId)
	}

	return nil
}

func (a *Aliyun) currentDNSRecord(ctx context.Context, client *alidns.Client, recordType string) (string, error) {
	request := alidns.CreateDescribeSubDomainRecordsRequest()
	request.SubDomain = a.config.Domain
	request.Type = recordType

	if err := ctx.Err(); err != nil {
		return "", err
	}
	response, err := client.DescribeSubDomainRecords(request)
	if err != nil {
		return "", fmt.Errorf("failed to get DNS records: %w", err)
	}
	if response.TotalCount == 0 || len(response.DomainRecords.Record) == 0 {
		return "", nil
	}

	return response.DomainRecords.Record[0].Value, nil
}
