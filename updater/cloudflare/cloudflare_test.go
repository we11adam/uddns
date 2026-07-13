package cloudflare

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestNewValidatesProxyURLWithoutExposingCredentials(t *testing.T) {
	_, err := New(&Config{
		APIToken: "token",
		Domain:   "home.example.com",
		Proxy:    "http://user:proxy-secret@proxy.example/tunnel",
	})
	if err == nil {
		t.Fatal("expected invalid proxy URL to fail")
	}
	for _, sensitive := range []string{"user", "proxy-secret", "tunnel"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("proxy validation error exposes %q: %v", sensitive, err)
		}
	}

	if _, err := New(&Config{
		APIToken: "token",
		Domain:   "home.example.com",
		Proxy:    "https://user:proxy-secret@proxy.example:8443/",
	}); err != nil {
		t.Fatalf("expected valid authenticated proxy URL: %v", err)
	}
}

func TestDuplicateDNSRecordsAreReconciledAndMixedValuesTriggerRepair(t *testing.T) {
	updated := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case http.MethodGet:
			if request.URL.Path != "/zones/zone-id/dns_records" {
				t.Errorf("unexpected list path: %s", request.URL.Path)
			}
			_, _ = io.WriteString(w, `{
				"success": true,
				"errors": [],
				"messages": [],
				"result": [
					{"id":"current","type":"A","name":"home.example.com","content":"192.0.2.10","ttl":120,"proxied":false},
					{"id":"stale-one","type":"A","name":"home.example.com","content":"192.0.2.11","ttl":300,"proxied":false},
					{"id":"stale-two","type":"A","name":"home.example.com","content":"192.0.2.12","ttl":1,"proxied":true}
				],
				"result_info":{"page":1,"per_page":100,"count":3,"total_count":3,"total_pages":1}
			}`)
		case http.MethodPatch:
			recordID := strings.TrimPrefix(request.URL.Path, "/zones/zone-id/dns_records/")
			var body struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode update request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			updated[recordID] = body.Content
			_, _ = io.WriteString(w, `{"success":true,"errors":[],"messages":[],"result":{"id":"`+recordID+`"}}`)
		default:
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	api, err := cloudflareapi.NewWithAPIToken("token", cloudflareapi.HTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("create Cloudflare API client: %v", err)
	}
	api.BaseURL = server.URL
	cloudflare := &Cloudflare{
		config: &Config{Domain: "home.example.com"},
		client: api,
		zoneID: "zone-id",
	}

	const desired = "192.0.2.10"
	if err := cloudflare.updateDNSRecord(context.Background(), recordTypeA, desired); err != nil {
		t.Fatalf("update duplicate records: %v", err)
	}
	if len(updated) != 2 || updated["stale-one"] != desired || updated["stale-two"] != desired {
		t.Fatalf("updated records = %#v, want both stale records", updated)
	}
	if _, ok := updated["current"]; ok {
		t.Fatal("already-current record was updated")
	}

	current, err := cloudflare.currentDNSRecord(context.Background(), recordTypeA)
	if err != nil {
		t.Fatalf("read duplicate records: %v", err)
	}
	if current != "" {
		t.Fatalf("mixed duplicate values returned %q; want empty value to trigger repair", current)
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
