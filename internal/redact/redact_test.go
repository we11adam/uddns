package redact

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestStringRedactsRawAndURLEncodedSecrets(t *testing.T) {
	secret := "token+/with space=&"
	value := strings.Join([]string{
		secret,
		url.QueryEscape(secret),
		url.PathEscape(secret),
	}, " ")

	got := String(value, secret)
	for _, sensitive := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
		if strings.Contains(got, sensitive) {
			t.Fatalf("redacted value still contains %q: %q", sensitive, got)
		}
	}
}

func TestErrorHandlesNil(t *testing.T) {
	if err := Error(nil, "secret"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if err := Error(errors.New("request contains secret"), "secret"); err.Error() != "request contains [REDACTED]" {
		t.Fatalf("unexpected redacted error: %v", err)
	}
}
