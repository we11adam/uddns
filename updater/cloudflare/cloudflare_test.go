package cloudflare

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	cloudflareapi "github.com/cloudflare/cloudflare-go"
	"github.com/we11adam/uddns/provider"
)

func TestHTTPClientHasRequestTimeout(t *testing.T) {
	client := newHTTPClient(nil)
	if client.Timeout != requestTimeout {
		t.Fatalf("expected timeout %s, got %s", requestTimeout, client.Timeout)
	}
	if client.Transport == http.DefaultTransport {
		t.Fatal("expected an isolated transport clone")
	}
}

func TestProxyLogValueRedactsCredentials(t *testing.T) {
	proxy, err := url.Parse("http://user:secret@127.0.0.1:8080/path?token=hidden")
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}

	value := proxyLogValue(proxy)
	if value != "http://127.0.0.1:8080" {
		t.Fatalf("expected redacted proxy URL, got %q", value)
	}
}

func TestZoneLookupUsesContext(t *testing.T) {
	api, err := cloudflareapi.NewWithAPIToken("token", cloudflareapi.HTTPClient(&http.Client{}))
	if err != nil {
		t.Fatalf("create Cloudflare API client: %v", err)
	}
	updater := &Cloudflare{
		config: &Config{Domain: "ddns.example.com"},
		client: api,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = updater.Update(ctx, &provider.IpResult{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled zone lookup, got %v", err)
	}
}
