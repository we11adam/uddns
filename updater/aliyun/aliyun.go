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

type Config struct {
	AccessKeyID     string `mapstructure:"accessKeyId"`
	AccessKeySecret string `mapstructure:"accessKeySecret"`
	RegionID        string `mapstructure:"regionId"`
	Domain          string `mapstructure:"domain"`
}

type Aliyun struct {
	config    *Config
	client    *alidns.Client
	recordIDs map[string]string
}

func init() {
	updater.Register("Aliyun", func(v *viper.Viper) (updater.Updater, error) {
		cfg := Config{}
		err := v.UnmarshalKey("updaters.aliyun", &cfg)
		if err != nil {
			return nil, err
		}
		return New(&cfg)
	})
}

func New(config *Config) (*Aliyun, error) {
	if config.RegionID == "" {
		config.RegionID = "cn-hangzhou"
		slog.Debug("[Aliyun] RegionID not set, using default value", "regionId", config.RegionID)
	} else {
		slog.Debug("[Aliyun] Using provided RegionID", "regionId", config.RegionID)
	}

	client, err := alidns.NewClientWithAccessKey(config.RegionID, config.AccessKeyID, config.AccessKeySecret)
	if err != nil {
		slog.Debug("[Aliyun] failed to create API client:", "error", err)
		return nil, err
	}

	return &Aliyun{
		config:    config,
		client:    client,
		recordIDs: make(map[string]string),
	}, nil
}

func (a *Aliyun) Update(ips *provider.IpResult) error {
	if ips.IPv4 != "" {
		if !isValidIPv4(ips.IPv4) {
			return fmt.Errorf("invalid IPv4 address: %s", ips.IPv4)
		}
		if err := a.updateDNSRecord("A", ips.IPv4); err != nil {
			slog.Error("[Aliyun] Failed to update IPv4 record", "error", err)
			return err
		}
	}

	if ips.IPv6 != "" {
		if !isValidIPv6(ips.IPv6) {
			return fmt.Errorf("invalid IPv6 address: %s", ips.IPv6)
		}
		if err := a.updateDNSRecord("AAAA", ips.IPv6); err != nil {
			slog.Error("[Aliyun] Failed to update IPv6 record", "error", err)
			return err
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
	rr := strings.Join(parts[:len(parts)-2], ".")
	domainName := strings.Join(parts[len(parts)-2:], ".")

	request := alidns.CreateDescribeSubDomainRecordsRequest()
	request.SubDomain = domain
	request.Type = recordType

	response, err := a.client.DescribeSubDomainRecords(request)
	if err != nil {
		slog.Error("[Aliyun] failed to get DNS records:", "error", err, "type", recordType)
		return err
	}

	if response.TotalCount > 0 && len(response.DomainRecords.Record) > 0 {
		existingRecord := response.DomainRecords.Record[0]

		if existingRecord.Value == ip {
			slog.Info("[Aliyun] DNS record is already up to date", "type", recordType, "ip", ip)
			return nil
		}

		updateRequest := alidns.CreateUpdateDomainRecordRequest()
		updateRequest.RecordId = existingRecord.RecordId
		updateRequest.RR = rr
		updateRequest.Type = recordType
		updateRequest.Value = ip

		_, err := a.client.UpdateDomainRecord(updateRequest)
		if err != nil {
			slog.Error("[Aliyun] failed to update DNS record", "error", err, "type", recordType)
			return err
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
			slog.Error("[Aliyun] failed to add DNS record", "error", err, "type", recordType)
			return err
		}

		slog.Info("[Aliyun] DNS record added successfully", "type", recordType, "ip", ip, "recordId", response.RecordId)
	}

	return nil
}
