package redact

import (
	"errors"
	"net/url"
	"sort"
	"strings"
)

const replacement = "[REDACTED]"

// String removes secrets in their raw, URL query-escaped, and URL path-escaped
// forms. The escaped forms are needed for transport errors that include the
// request URL.
func String(value string, secrets ...string) string {
	variants := make(map[string]struct{}, len(secrets)*3)
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		variants[secret] = struct{}{}
		variants[url.QueryEscape(secret)] = struct{}{}
		variants[url.PathEscape(secret)] = struct{}{}
	}

	ordered := make([]string, 0, len(variants))
	for variant := range variants {
		ordered = append(ordered, variant)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return len(ordered[i]) > len(ordered[j])
	})

	for _, variant := range ordered {
		value = strings.ReplaceAll(value, variant, replacement)
	}
	return value
}

// Error returns an error whose text does not expose the supplied secrets.
func Error(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	return errors.New(String(err.Error(), secrets...))
}
