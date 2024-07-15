package aliyun

import (
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/alidns"
	"github.com/spf13/viper"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
)

const (
	defaultRegionID = "cn-hangzhou"
	recordTypeA     = "A"
	recordTypeAAAA  = "AAAA"
)

type Config struct {
	AccessKeyID     string `mapstructure:"accesskeyid"`
	AccessKeySecret string `mapstructure:"accesskeysecret"`
	RegionID        string `mapstructure:"regionid"`
	Domain          string `mapstructure:"domain"`
}

type Aliyun struct {
	config *Config
	client *alidns.Client
}

func init() {
	updater.Register("Aliyun", func(v *viper.Viper) (updater.Updater, error) {
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
		return nil, fmt.Errorf("Aliyun AccessKeyID is not set in the configuration")
	}
	if config.AccessKeySecret == "" {
		return nil, fmt.Errorf("Aliyun AccessKeySecret is not set in the configuration")
	}
	if config.Domain == "" {
		return nil, fmt.Errorf("Aliyun Domain is not set in the configuration")
	}

	if config.RegionID == "" {
		config.RegionID = "cn-hangzhou"
		slog.Debug("[Aliyun] RegionID not set, using default value", "regionId", config.RegionID)
	}

	client, err := alidns.NewClientWithAccessKey(config.RegionID, config.AccessKeyID, config.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create Aliyun API client: %w", err)
	}

	return &Aliyun{
		config: config,
		client: client,
	}, nil
}

func (a *Aliyun) Update(ips *provider.IpResult) error {
	if ips.IPv4 != "" {
		if !isValidIPv4(ips.IPv4) {
			return fmt.Errorf("invalid IPv4 address: %s", ips.IPv4)
		}
		if err := a.updateDNSRecord(recordTypeA, ips.IPv4); err != nil {
			return fmt.Errorf("failed to update IPv4 record: %w", err)
		}
	}

	if ips.IPv6 != "" {
		if !isValidIPv6(ips.IPv6) {
			return fmt.Errorf("invalid IPv6 address: %s", ips.IPv6)
		}
		if err := a.updateDNSRecord(recordTypeAAAA, ips.IPv6); err != nil {
			return fmt.Errorf("failed to update IPv6 record: %w", err)
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

func (a *Aliyun) updateDNSRecord(recordType, ip string) error {
	domain := a.config.Domain
	parts := strings.Split(domain, ".")
	domainName := strings.Join(parts[len(parts)-2:], ".")
	rr := strings.TrimSuffix(domain, "."+domainName)

	request := alidns.CreateDescribeSubDomainRecordsRequest()
	request.SubDomain = domain
	request.Type = recordType

	response, err := a.client.DescribeSubDomainRecords(request)
	if err != nil {
		return fmt.Errorf("failed to get DNS records: %w", err)
	}

	if response.TotalCount > 0 && len(response.DomainRecords.Record) > 0 {
		existingRecord := response.DomainRecords.Record[0]

		if existingRecord.Value == ip {
			slog.Debug("[Aliyun] DNS record is already up to date", "type", recordType, "ip", ip)
			return nil
		}

		updateRequest := alidns.CreateUpdateDomainRecordRequest()
		updateRequest.RecordId = existingRecord.RecordId
		updateRequest.RR = rr
		updateRequest.Type = recordType
		updateRequest.Value = ip

		_, err := a.client.UpdateDomainRecord(updateRequest)
		if err != nil {
			return fmt.Errorf("failed to update DNS record: %w", err)
		}

		slog.Info("[Aliyun] DNS record updated successfully", "type", recordType, "ip", ip, "recordId", existingRecord.RecordId)
	} else {
		addRequest := alidns.CreateAddDomainRecordRequest()
		addRequest.DomainName = domainName
		addRequest.RR = rr
		addRequest.Type = recordType
		addRequest.Value = ip

		response, err := a.client.AddDomainRecord(addRequest)
		if err != nil {
			return fmt.Errorf("failed to add DNS record: %w", err)
		}

		slog.Info("[Aliyun] DNS record added successfully", "type", recordType, "ip", ip, "recordId", response.RecordId)
	}

	return nil
}
