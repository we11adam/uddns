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

func TestNewRejectsNilConfig(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("expected nil config to be rejected")
	}
}

func TestHTTPClientSupportsCustomDefaultTransport(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })

	custom := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       http.NoBody,
			Request:    request,
		}, nil
	})
	http.DefaultTransport = custom

	client := newHTTPClient(nil)
	request, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("custom default transport request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusNoContent)
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

func TestRetryResponseTransportLimitsNonRetryableResponseBody(t *testing.T) {
	body := &trackingBody{reader: strings.NewReader(strings.Repeat("x", responseBodyMax+1))}
	transport := retryResponseBodyClosingTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Body: body}, nil
	})}

	response, err := transport.RoundTrip(&http.Request{})
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if body.closed {
		t.Fatal("expected non-retryable response body to remain open")
	}
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read limited response body: %v", err)
	}
	if len(content) != responseBodyMax {
		t.Fatalf("response body length = %d, want %d", len(content), responseBodyMax)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close limited response body: %v", err)
	}
	if !body.closed {
		t.Fatal("expected close to be forwarded to the original body")
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

func TestCurrentRequestsOnlySelectedFamilies(t *testing.T) {
	tests := []struct {
		name     string
		families provider.FamilyRequest
		wantType string
		wantIPv4 string
		wantIPv6 string
	}{
		{
			name:     "IPv4 only",
			families: provider.FamilyRequest{IPv4: true},
			wantType: recordTypeA,
			wantIPv4: "192.0.2.10",
		},
		{
			name:     "IPv6 only",
			families: provider.FamilyRequest{IPv6: true},
			wantType: recordTypeAAAA,
			wantIPv6: "2001:db8::10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestedTypes []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/zones/zone-id/dns_records" {
					t.Errorf("unexpected path: %s", request.URL.Path)
				}
				recordType := request.URL.Query().Get("type")
				requestedTypes = append(requestedTypes, recordType)
				content := "192.0.2.10"
				if recordType == recordTypeAAAA {
					content = "2001:db8::10"
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{
					"success":true,
					"errors":[],
					"messages":[],
					"result":[{"id":"record-id","type":"`+recordType+`","name":"home.example.com","content":"`+content+`"}],
					"result_info":{"page":1,"per_page":100,"count":1,"total_count":1,"total_pages":1}
				}`)
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

			current, err := cloudflare.Current(context.Background(), tt.families)
			if err != nil {
				t.Fatalf("read current records: %v", err)
			}
			if len(requestedTypes) != 1 || requestedTypes[0] != tt.wantType {
				t.Fatalf("requested record types = %v, want [%s]", requestedTypes, tt.wantType)
			}
			if current.IPv4 != tt.wantIPv4 || current.IPv6 != tt.wantIPv6 {
				t.Fatalf("current records = %+v, want IPv4=%q IPv6=%q", current, tt.wantIPv4, tt.wantIPv6)
			}
		})
	}
}

func TestUpdateRefreshesStaleZoneID(t *testing.T) {
	var staleLists, zoneLookups, freshLists, creates int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/zones/stale-zone/dns_records":
			staleLists++
			writeCloudflareError(w, http.StatusBadRequest, invalidObjectIdentifierCode)
		case "/zones":
			zoneLookups++
			if got := request.URL.Query().Get("name"); got != "example.com" {
				t.Errorf("zone lookup name = %q, want example.com", got)
			}
			writeCloudflareResult(w, []map[string]any{{"id": "fresh-zone", "name": "example.com"}}, 1)
		case "/zones/fresh-zone/dns_records":
			switch request.Method {
			case http.MethodGet:
				freshLists++
				writeCloudflareResult(w, []any{}, 0)
			case http.MethodPost:
				creates++
				writeCloudflareResult(w, map[string]any{"id": "record-id"}, 1)
			default:
				t.Errorf("unexpected fresh-zone method: %s", request.Method)
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		default:
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cloudflare := newCloudflareTestUpdater(t, server, "stale-zone")
	if err := cloudflare.Update(context.Background(), &provider.IpResult{IPv4: "192.0.2.10"}); err != nil {
		t.Fatalf("update with stale zone ID: %v", err)
	}
	if cloudflare.zoneID != "fresh-zone" {
		t.Fatalf("zone ID = %q, want fresh-zone", cloudflare.zoneID)
	}
	if staleLists != 1 || zoneLookups != 1 || freshLists != 1 || creates != 1 {
		t.Fatalf("request counts stale=%d lookup=%d fresh=%d create=%d, want all 1", staleLists, zoneLookups, freshLists, creates)
	}
}

func TestCurrentRefreshesStaleZoneID(t *testing.T) {
	var staleLists, zoneLookups, freshLists int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/zones/stale-zone/dns_records":
			staleLists++
			writeCloudflareError(w, http.StatusNotFound, 10000)
		case "/zones":
			zoneLookups++
			writeCloudflareResult(w, []map[string]any{{"id": "fresh-zone", "name": "example.com"}}, 1)
		case "/zones/fresh-zone/dns_records":
			freshLists++
			writeCloudflareResult(w, []map[string]any{{
				"id": "record-id", "type": "A", "name": "home.example.com", "content": "192.0.2.10",
			}}, 1)
		default:
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cloudflare := newCloudflareTestUpdater(t, server, "stale-zone")
	current, err := cloudflare.Current(context.Background(), provider.FamilyRequest{IPv4: true})
	if err != nil {
		t.Fatalf("read with stale zone ID: %v", err)
	}
	if current.IPv4 != "192.0.2.10" {
		t.Fatalf("current IPv4 = %q, want 192.0.2.10", current.IPv4)
	}
	if cloudflare.zoneID != "fresh-zone" {
		t.Fatalf("zone ID = %q, want fresh-zone", cloudflare.zoneID)
	}
	if staleLists != 1 || zoneLookups != 1 || freshLists != 1 {
		t.Fatalf("request counts stale=%d lookup=%d fresh=%d, want all 1", staleLists, zoneLookups, freshLists)
	}
}

func TestStaleZoneIDRefreshRetriesOnlyOnce(t *testing.T) {
	var staleLists, zoneLookups, freshLists int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/zones/stale-zone/dns_records":
			staleLists++
			writeCloudflareError(w, http.StatusBadRequest, invalidObjectIdentifierCode)
		case "/zones":
			zoneLookups++
			writeCloudflareResult(w, []map[string]any{{"id": "fresh-zone", "name": "example.com"}}, 1)
		case "/zones/fresh-zone/dns_records":
			freshLists++
			writeCloudflareError(w, http.StatusNotFound, 10000)
		default:
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cloudflare := newCloudflareTestUpdater(t, server, "stale-zone")
	if _, err := cloudflare.Current(context.Background(), provider.FamilyRequest{IPv4: true}); err == nil {
		t.Fatal("expected refreshed stale zone ID to return an error")
	}
	if cloudflare.zoneID != "" {
		t.Fatalf("zone ID = %q, want cleared cache after second stale error", cloudflare.zoneID)
	}
	if staleLists != 1 || zoneLookups != 1 || freshLists != 1 {
		t.Fatalf("request counts stale=%d lookup=%d fresh=%d, want all 1", staleLists, zoneLookups, freshLists)
	}
}

func TestZoneIDIsNotRefreshedForUnrelatedAPIErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		errorCode  int
	}{
		{name: "unauthorized", statusCode: http.StatusUnauthorized, errorCode: 9109},
		{name: "forbidden", statusCode: http.StatusForbidden, errorCode: 9109},
		{name: "rate limited", statusCode: http.StatusTooManyRequests, errorCode: 1015},
		{name: "other bad request", statusCode: http.StatusBadRequest, errorCode: 10000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var recordLists, zoneLookups int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/zones/cached-zone/dns_records":
					recordLists++
					writeCloudflareError(w, tt.statusCode, tt.errorCode)
				case "/zones":
					zoneLookups++
					writeCloudflareResult(w, []map[string]any{{"id": "fresh-zone", "name": "example.com"}}, 1)
				default:
					t.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			cloudflare := newCloudflareTestUpdater(t, server, "cached-zone")
			if _, err := cloudflare.Current(context.Background(), provider.FamilyRequest{IPv4: true}); err == nil {
				t.Fatal("expected API error")
			}
			if cloudflare.zoneID != "cached-zone" {
				t.Fatalf("zone ID = %q, want cached-zone", cloudflare.zoneID)
			}
			if recordLists != 1 || zoneLookups != 0 {
				t.Fatalf("request counts records=%d lookup=%d, want 1 and 0", recordLists, zoneLookups)
			}
		})
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

func newCloudflareTestUpdater(t *testing.T, server *httptest.Server, zoneID string) *Cloudflare {
	t.Helper()
	api, err := cloudflareapi.NewWithAPIToken(
		"token",
		cloudflareapi.HTTPClient(server.Client()),
		cloudflareapi.UsingRateLimit(100000),
		cloudflareapi.UsingRetryPolicy(0, 0, 0),
	)
	if err != nil {
		t.Fatalf("create Cloudflare API client: %v", err)
	}
	api.BaseURL = server.URL
	return &Cloudflare{
		config: &Config{Domain: "home.example.com"},
		client: api,
		zoneID: zoneID,
	}
}

func writeCloudflareError(w http.ResponseWriter, statusCode, errorCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Connection", "close")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":  false,
		"errors":   []map[string]any{{"code": errorCode, "message": "test error"}},
		"messages": []any{},
		"result":   nil,
	})
}

func writeCloudflareResult(w http.ResponseWriter, result any, count int) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":  true,
		"errors":   []any{},
		"messages": []any{},
		"result":   result,
		"result_info": map[string]any{
			"page": 1, "per_page": 100, "count": count, "total_count": count, "total_pages": 1,
		},
	})
}
