package aliyun

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	alidns "github.com/alibabacloud-go/alidns-20150109/v4/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/we11adam/uddns/internal/dnsname"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
)

const (
	defaultRegionID = "cn-hangzhou"
	connectTimeout  = 5 * time.Second
	readTimeout     = 10 * time.Second
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
	config  *Config
	client  *alidns.Client
	runtime *dara.RuntimeOptions
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
		config.RegionID = defaultRegionID
		slog.Debug("region ID not set, using default", "updater", "aliyun", "region_id", config.RegionID)
	}

	client, err := newClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Aliyun API client: %w", err)
	}

	return &Aliyun{
		config: config,
		client: client,
		runtime: (&dara.RuntimeOptions{}).
			SetConnectTimeout(int(connectTimeout.Milliseconds())).
			SetReadTimeout(int(readTimeout.Milliseconds())),
	}, nil
}

func newClient(config *Config) (*alidns.Client, error) {
	sdkConfig := (&openapi.Config{}).
		SetAccessKeyId(config.AccessKeyID).
		SetAccessKeySecret(config.AccessKeySecret).
		SetRegionId(config.RegionID).
		SetProtocol("HTTPS").
		SetConnectTimeout(int(connectTimeout.Milliseconds())).
		SetReadTimeout(int(readTimeout.Milliseconds()))
	return alidns.NewClient(sdkConfig)
}

func (a *Aliyun) Update(ctx context.Context, ips *provider.IpResult) error {
	if ips.IPv4 != "" {
		if !provider.IsValidIPv4(ips.IPv4) {
			return fmt.Errorf("invalid IPv4 address: %s", ips.IPv4)
		}
		if err := a.updateDNSRecord(ctx, recordTypeA, ips.IPv4); err != nil {
			return fmt.Errorf("failed to update IPv4 record: %w", err)
		}
	}

	if ips.IPv6 != "" {
		if !provider.IsValidIPv6(ips.IPv6) {
			return fmt.Errorf("invalid IPv6 address: %s", ips.IPv6)
		}
		if err := a.updateDNSRecord(ctx, recordTypeAAAA, ips.IPv6); err != nil {
			return fmt.Errorf("failed to update IPv6 record: %w", err)
		}
	}

	return nil
}

func (a *Aliyun) Current(ctx context.Context) (*provider.IpResult, error) {
	result := &provider.IpResult{}

	ipv4, err := a.currentDNSRecord(ctx, recordTypeA)
	if err != nil {
		return nil, fmt.Errorf("failed to get Aliyun IPv4 record: %w", err)
	}
	result.IPv4 = ipv4

	ipv6, err := a.currentDNSRecord(ctx, recordTypeAAAA)
	if err != nil {
		return nil, fmt.Errorf("failed to get Aliyun IPv6 record: %w", err)
	}
	result.IPv6 = ipv6

	return result, nil
}

func (a *Aliyun) updateDNSRecord(ctx context.Context, recordType, ip string) error {
	domain := a.config.Domain
	domainName, rr, err := dnsname.SplitRecord(domain, a.config.Zone)
	if err != nil {
		return fmt.Errorf("invalid Aliyun DNS record: %w", err)
	}

	request := (&alidns.DescribeSubDomainRecordsRequest{}).
		SetSubDomain(domain).
		SetType(recordType)
	response, err := a.client.DescribeSubDomainRecordsWithContext(ctx, request, a.runtime)
	if err != nil {
		return fmt.Errorf("failed to get DNS records: %w", err)
	}

	records := responseRecords(response)
	if responseTotalCount(response) > 0 && len(records) > 0 && records[0] != nil {
		existingRecord := records[0]
		existingValue := dara.StringValue(existingRecord.Value)
		recordID := dara.StringValue(existingRecord.RecordId)

		if existingValue == ip {
			slog.Debug("skipping current DNS record", "updater", "aliyun", "record", domain, "record_type", recordType, "ip", ip)
			return nil
		}

		updateRequest := (&alidns.UpdateDomainRecordRequest{}).
			SetRecordId(recordID).
			SetRR(rr).
			SetType(recordType).
			SetValue(ip)
		_, err := a.client.UpdateDomainRecordWithContext(ctx, updateRequest, a.runtime)
		if err != nil {
			return fmt.Errorf("failed to update DNS record: %w", err)
		}

		slog.Info("updated DNS record", "updater", "aliyun", "record", domain, "record_type", recordType, "ip", ip, "record_id", recordID)
	} else {
		addRequest := (&alidns.AddDomainRecordRequest{}).
			SetDomainName(domainName).
			SetRR(rr).
			SetType(recordType).
			SetValue(ip)
		response, err := a.client.AddDomainRecordWithContext(ctx, addRequest, a.runtime)
		if err != nil {
			return fmt.Errorf("failed to add DNS record: %w", err)
		}

		recordID := ""
		if response != nil && response.Body != nil {
			recordID = dara.StringValue(response.Body.RecordId)
		}
		slog.Info("added DNS record", "updater", "aliyun", "record", domain, "record_type", recordType, "ip", ip, "record_id", recordID)
	}

	return nil
}

func (a *Aliyun) currentDNSRecord(ctx context.Context, recordType string) (string, error) {
	request := (&alidns.DescribeSubDomainRecordsRequest{}).
		SetSubDomain(a.config.Domain).
		SetType(recordType)
	response, err := a.client.DescribeSubDomainRecordsWithContext(ctx, request, a.runtime)
	if err != nil {
		return "", fmt.Errorf("failed to get DNS records: %w", err)
	}
	records := responseRecords(response)
	if responseTotalCount(response) == 0 || len(records) == 0 || records[0] == nil {
		return "", nil
	}

	return dara.StringValue(records[0].Value), nil
}

func responseRecords(response *alidns.DescribeSubDomainRecordsResponse) []*alidns.DescribeSubDomainRecordsResponseBodyDomainRecordsRecord {
	if response == nil || response.Body == nil || response.Body.DomainRecords == nil {
		return nil
	}
	return response.Body.DomainRecords.Record
}

func responseTotalCount(response *alidns.DescribeSubDomainRecordsResponse) int64 {
	if response == nil || response.Body == nil {
		return 0
	}
	return dara.Int64Value(response.Body.TotalCount)
}
