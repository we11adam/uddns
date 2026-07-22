package scaleway

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/scaleway/scaleway-sdk-go/api/domain/v2beta1"
	"github.com/scaleway/scaleway-sdk-go/scw"
	"golang.org/x/net/publicsuffix"

	"github.com/we11adam/uddns/provider"
	"github.com/we11adam/uddns/updater"
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
	client *scw.Client
	config *Config
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

	if cfg.Zone != "" {
		cfg.Zone, err = publicsuffix.EffectiveTLDPlusOne(cfg.Zone)
		if err != nil {
			return nil, fmt.Errorf("could not retrieve Scaleway zone: %w", err)
		}
	}
	if cfg.TTL == nil {
		cfg.TTL = new(150)
	}
	if *cfg.TTL < 0 {
		return nil, fmt.Errorf("invalid TTL value: %d", *cfg.TTL)
	}

	sw = &Scaleway{
		config: cfg,
	}
	sw.client, err = scw.NewClient(
		scw.WithAuth(cfg.AccessKey, cfg.SecretKey),
		scw.WithDefaultProjectID(cfg.ProjectID),
		scw.WithHTTPClient(&http.Client{
			Timeout: 10 * time.Second,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Scaleway client: %w", err)
	}

	return sw, nil
}

func (s *Scaleway) Update(ctx context.Context, ips *provider.IpResult) error {
	api := domain.NewAPI(s.client)

	name := getRecordName(s.config.Domain, s.config.Zone)

	records := []*domain.Record{}
	if ips.IPv4 != "" {
		records = append(records, &domain.Record{
			Name: name,
			Type: domain.RecordTypeA,
			Data: ips.IPv4,
			TTL:  uint32(*s.config.TTL),
		})
	}
	if ips.IPv6 != "" {
		records = append(records, &domain.Record{
			Name: name,
			Type: domain.RecordTypeAAAA,
			Data: ips.IPv6,
			TTL:  uint32(*s.config.TTL),
		})
	}

	_, err := api.UpdateDNSZoneRecords(&domain.UpdateDNSZoneRecordsRequest{
		DNSZone: s.config.Zone,
		Changes: []*domain.RecordChange{
			{
				Set: &domain.RecordChangeSet{
					Records: records,
				},
			},
		},
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
		Name:    getRecordName(s.config.Domain, s.config.Zone),
		Type:    recordType,
	}, scw.WithContext(ctx))
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

func getRecordName(domain string, zone string) string {
	name := ""
	if domain != zone {
		return strings.TrimSuffix(domain, "."+zone)
	}
	return name
}
