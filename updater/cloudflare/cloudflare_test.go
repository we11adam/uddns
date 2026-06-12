package cloudflare

import (
	"net/url"
	"testing"
)

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
