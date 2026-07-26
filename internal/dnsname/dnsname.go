package dnsname

import (
	"fmt"
	"strings"

	"golang.org/x/net/publicsuffix"
)

func Normalize(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimSuffix(name, ".")
	if name == "" {
		return "", fmt.Errorf("DNS name is empty")
	}
	if len(name) > 253 {
		return "", fmt.Errorf("DNS name is too long: %s", name)
	}

	labels := strings.Split(name, ".")
	for _, label := range labels {
		if err := validateLabel(label); err != nil {
			return "", fmt.Errorf("invalid DNS name %q: %w", name, err)
		}
	}

	return name, nil
}

func SplitRecord(recordName, zoneName string) (string, string, error) {
	record, err := Normalize(recordName)
	if err != nil {
		return "", "", err
	}

	if zoneName != "" {
		zone, err := Normalize(zoneName)
		if err != nil {
			return "", "", err
		}
		return splitRecordWithZone(record, zone)
	}

	zone, err := publicsuffix.EffectiveTLDPlusOne(record)
	if err != nil {
		return "", "", fmt.Errorf(
			"cannot infer DNS zone for record %q; set zone explicitly: %w",
			record,
			err,
		)
	}
	return splitRecordWithZone(record, zone)
}

func splitRecordWithZone(record, zone string) (string, string, error) {
	if record == zone {
		return zone, "@", nil
	}

	suffix := "." + zone
	if !strings.HasSuffix(record, suffix) {
		return "", "", fmt.Errorf("DNS record %q is not under zone %q", record, zone)
	}

	rr := strings.TrimSuffix(record, suffix)
	if rr == "" {
		rr = "@"
	}
	return zone, rr, nil
}

func validateLabel(label string) error {
	if label == "" {
		return fmt.Errorf("empty label")
	}
	if len(label) > 63 {
		return fmt.Errorf("label %q is too long", label)
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return fmt.Errorf("label %q starts or ends with hyphen", label)
	}

	for _, r := range label {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '-' {
			continue
		}
		return fmt.Errorf("label %q contains unsupported character %q", label, r)
	}

	return nil
}
