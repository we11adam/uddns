package duckdns

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestUpdateIPRedactsToken(t *testing.T) {
	token := "duck+/token =secret"

	t.Run("transport error", func(t *testing.T) {
		duckDNS := mustNewDuckDNS(t, &Config{Domain: "home", Token: token})
		duckDNS.httpclient.SetTransport(roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return nil, errors.New("request failed for " + url.PathEscape(token))
		}))

		err := duckDNS.updateIP(context.Background(), "192.0.2.1")
		if err == nil {
			t.Fatal("expected transport error")
		}
		assertTokenRedacted(t, err.Error(), token)
	})

	t.Run("response body", func(t *testing.T) {
		duckDNS := mustNewDuckDNS(t, &Config{Domain: "home", Token: token})
		duckDNS.httpclient.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body := "invalid token " + token + " / " + url.QueryEscape(token)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    request,
			}, nil
		}))

		err := duckDNS.updateIP(context.Background(), "192.0.2.1")
		if err == nil {
			t.Fatal("expected response error")
		}
		assertTokenRedacted(t, err.Error(), token)
	})
}

func TestUpdateIPPropagatesContext(t *testing.T) {
	type contextKey struct{}
	key := contextKey{}
	ctx := context.WithValue(context.Background(), key, "request-value")
	duckDNS := mustNewDuckDNS(t, &Config{Domain: "home", Token: "token"})
	duckDNS.httpclient.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Context().Value(key); got != "request-value" {
			t.Fatalf("expected request context value, got %#v", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("OK")),
			Request:    request,
		}, nil
	}))

	if err := duckDNS.updateIP(ctx, "192.0.2.1"); err != nil {
		t.Fatalf("update IP: %v", err)
	}
}

func TestNewNormalizesAndValidatesDomain(t *testing.T) {
	duckDNS := mustNewDuckDNS(t, &Config{Domain: " Home.DuckDNS.org. ", Token: "token"})
	if duckDNS.config.Domain != "home.duckdns.org" {
		t.Fatalf("domain = %q, want %q", duckDNS.config.Domain, "home.duckdns.org")
	}
	if duckDNS.httpclient.ResponseBodyLimit != responseBodyLimit {
		t.Fatalf("response body limit = %d, want %d", duckDNS.httpclient.ResponseBodyLimit, responseBodyLimit)
	}

	for _, domain := range []string{
		"",
		"   ",
		"home..duckdns.org",
		"home_name.duckdns.org",
		"-home.duckdns.org",
		"home-.duckdns.org",
		"home/duckdns.org",
	} {
		t.Run(domain, func(t *testing.T) {
			if _, err := New(&Config{Domain: domain, Token: "token"}); err == nil {
				t.Fatalf("expected domain %q to be rejected", domain)
			}
		})
	}
}

func mustNewDuckDNS(t *testing.T, cfg *Config) *DuckDNS {
	t.Helper()
	duckDNS, err := New(cfg)
	if err != nil {
		t.Fatalf("new DuckDNS updater: %v", err)
	}
	return duckDNS
}

func assertTokenRedacted(t *testing.T, value, token string) {
	t.Helper()
	for _, sensitive := range []string{token, url.QueryEscape(token), url.PathEscape(token)} {
		if strings.Contains(value, sensitive) {
			t.Fatalf("error still contains token %q: %q", sensitive, value)
		}
	}
}
