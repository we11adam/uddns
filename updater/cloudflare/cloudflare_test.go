package cloudflare

import (
	"net/http"
	"net/url"
	"testing"
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
