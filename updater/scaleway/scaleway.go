package scaleway

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/scaleway/scaleway-sdk-go/api/domain/v2beta1"
	"github.com/scaleway/scaleway-sdk-go/scw"
	"golang.org/x/net/publicsuffix"

	"github.com/we11adam/uddns/internal/dnsname"
	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
)

const (
	defaultTTL     = 150
	requestTimeout = 10 * time.Second
)

type Config struct {
	Domain    string `mapstructure:"domain"`
	ProjectID string `mapstructure:"project_id"`
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`

	// Optional zone name for the DNS record. If not provided, the root domain will be used.
	Zone string `mapstructure:"zone"`
	// Optional TTL for the DNS record. If not provided, the default TTL (150) will be used.
	TTL *int `mapstructure:"ttl"`
}

type Scaleway struct {
	client     *scw.Client
	config     *Config
	recordName string
}

func init() {
	updater.Register("Scaleway", "updaters.scaleway", func(v updater.ConfigReader) (updater.Updater, error) {
		if !v.IsSet("updaters.scaleway") {
			return nil, updater.ErrNotConfigured
		}

		cfg := Config{}
		err := v.UnmarshalKey("updaters.scaleway", &cfg)
		if err != nil {
			return nil, err
		}
		return New(&cfg)
	})
}

func New(cfg *Config) (sw *Scaleway, err error) {
	if cfg == nil {
		return nil, fmt.Errorf("Scaleway config is nil")
	}
	if cfg.Domain == "" || cfg.ProjectID == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("missing required Scaleway fields")
	}

	normalizedConfig := *cfg
	normalizedConfig.Domain, err = dnsname.Normalize(cfg.Domain)
	if err != nil {
		return nil, fmt.Errorf("invalid Scaleway domain: %w", err)
	}

	if cfg.Zone == "" {
		normalizedConfig.Zone, err = publicsuffix.EffectiveTLDPlusOne(normalizedConfig.Domain)
		if err != nil {
			return nil, fmt.Errorf("could not infer Scaleway zone: %w", err)
		}
	} else {
		normalizedConfig.Zone, err = dnsname.Normalize(cfg.Zone)
		if err != nil {
			return nil, fmt.Errorf("invalid Scaleway zone: %w", err)
		}
	}

	var swRecordName string
	normalizedConfig.Zone, swRecordName, err = dnsname.SplitRecord(normalizedConfig.Domain, normalizedConfig.Zone)
	if err != nil {
		return nil, fmt.Errorf("invalid Scaleway DNS record: %w", err)
	}
	if swRecordName == "@" {
		swRecordName = ""
	}

	ttl := defaultTTL
	if cfg.TTL != nil {
		ttl = *cfg.TTL
	}
	if ttl < 0 || uint64(ttl) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("invalid TTL value: %d", ttl)
	}
	normalizedConfig.TTL = &ttl

	sw = &Scaleway{
		config:     &normalizedConfig,
		recordName: swRecordName,
	}
	sw.client, err = scw.NewClient(
		scw.WithAuth(normalizedConfig.AccessKey, normalizedConfig.SecretKey),
		scw.WithDefaultProjectID(normalizedConfig.ProjectID),
		scw.WithHTTPClient(&http.Client{
			Timeout: requestTimeout,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Scaleway client: %w", err)
	}

	return sw, nil
}

func (s *Scaleway) Update(ctx context.Context, ips *provider.IpResult) error {
	if ips == nil {
		return fmt.Errorf("Scaleway IP result is nil")
	}

	api := domain.NewAPI(s.client)

	records := make([]*domain.Record, 0, 2)
	changes := make([]*domain.RecordChange, 0, 2)
	addChange := func(recordType domain.RecordType, data string) {
		record := &domain.Record{
			Name: s.recordName,
			Type: recordType,
			Data: data,
			TTL:  uint32(*s.config.TTL),
		}
		records = append(records, record)
		changes = append(changes, &domain.RecordChange{
			Set: &domain.RecordChangeSet{
				IDFields: &domain.RecordIdentifier{
					Name: s.recordName,
					Type: recordType,
				},
				Records: []*domain.Record{record},
			},
		})
	}

	if ips.IPv4 != "" {
		if !provider.IsValidIPv4(ips.IPv4) {
			return fmt.Errorf("invalid IPv4 address: %s", ips.IPv4)
		}
		addChange(domain.RecordTypeA, ips.IPv4)
	}
	if ips.IPv6 != "" {
		if !provider.IsValidIPv6(ips.IPv6) {
			return fmt.Errorf("invalid IPv6 address: %s", ips.IPv6)
		}
		addChange(domain.RecordTypeAAAA, ips.IPv6)
	}
	if len(changes) == 0 {
		return nil
	}

	returnAllRecords := false
	_, err := api.UpdateDNSZoneRecords(&domain.UpdateDNSZoneRecordsRequest{
		DNSZone:                 s.config.Zone,
		Changes:                 changes,
		ReturnAllRecords:        &returnAllRecords,
		DisallowNewZoneCreation: true,
	}, scw.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("failed to update Scaleway DNS records: %w", err)
	}

	for _, record := range records {
		slog.Info("updated DNS record", "updater", "scaleway", "record", s.config.Domain, "ip", record.Data)
	}

	return nil
}

func (s *Scaleway) Current(ctx context.Context, families provider.FamilyRequest) (*provider.IpResult, error) {
	if !families.IPv4 && !families.IPv6 {
		return nil, fmt.Errorf("no IP families requested")
	}
	var result *provider.IpResult

	api := domain.NewAPI(s.client)

	recordType := domain.RecordTypeUnknown
	if !families.IPv4 || !families.IPv6 {
		if families.IPv4 {
			recordType = domain.RecordTypeA
		} else if families.IPv6 {
			recordType = domain.RecordTypeAAAA
		}
	}

	res, err := api.ListDNSZoneRecords(&domain.ListDNSZoneRecordsRequest{
		DNSZone: s.config.Zone,
		Name:    s.recordName,
		Type:    recordType,
	}, scw.WithAllPages(), scw.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to get Scaleway DNS records: %w", err)
	}

	if len(res.Records) == 0 {
		return &provider.IpResult{}, nil
	}

	result = &provider.IpResult{}
	for _, record := range res.Records {
		switch record.Type {
		case domain.RecordTypeA:
			if families.IPv4 {
				result.IPv4 = record.Data
			}
		case domain.RecordTypeAAAA:
			if families.IPv6 {
				result.IPv6 = record.Data
			}
		}
	}

	return result, nil
}
