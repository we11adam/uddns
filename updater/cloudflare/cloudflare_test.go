package cloudflare

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	cloudflareapi "github.com/cloudflare/cloudflare-go"
	"github.com/we11adam/uddns/provider"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type trackingBody struct {
	reader *strings.Reader
	closed bool
}

func (b *trackingBody) Read(p []byte) (int, error) {
	return b.reader.Read(p)
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

func TestHTTPClientHasRequestTimeout(t *testing.T) {
	client := newHTTPClient(nil)
	if client.Timeout != requestTimeout {
		t.Fatalf("expected timeout %s, got %s", requestTimeout, client.Timeout)
	}
	if client.Transport == http.DefaultTransport {
		t.Fatal("expected an isolated transport clone")
	}
}

func TestRetryResponseTransportClosesRetryableResponseBody(t *testing.T) {
	for _, statusCode := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			body := &trackingBody{reader: strings.NewReader("retry response")}
			transport := retryResponseBodyClosingTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: statusCode, Body: body}, nil
			})}

			response, err := transport.RoundTrip(&http.Request{})
			if err != nil {
				t.Fatalf("round trip: %v", err)
			}
			if !body.closed {
				t.Fatal("expected retryable response body to be closed")
			}
			if body.reader.Len() != 0 {
				t.Fatalf("expected retryable response body to be drained, %d bytes remain", body.reader.Len())
			}
			content, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("read replacement body: %v", err)
			}
			if len(content) != 0 {
				t.Fatalf("expected empty replacement body, got %q", content)
			}
		})
	}
}

func TestRetryResponseTransportLeavesNonRetryableResponseBodyUntouched(t *testing.T) {
	body := &trackingBody{reader: strings.NewReader("client error")}
	transport := retryResponseBodyClosingTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Body: body}, nil
	})}

	response, err := transport.RoundTrip(&http.Request{})
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if response.Body != body {
		t.Fatal("expected non-retryable response body to remain unchanged")
	}
	if body.closed {
		t.Fatal("expected non-retryable response body to remain open")
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
