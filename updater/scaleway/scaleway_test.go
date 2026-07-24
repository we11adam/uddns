package scaleway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/scaleway/scaleway-sdk-go/api/domain/v2beta1"
	"github.com/scaleway/scaleway-sdk-go/scw"
	"github.com/spf13/viper"

	"github.com/we11adam/uddns/provider"
)

const (
	testAccessKey = "SCW1234567890ABCDEFG"
	testSecretKey = "7363616c-6577-6573-6862-6f7579616161"
	testProjectID = "6170692e-7363-616c-6577-61792e636f6e"
	testAPIURL    = "https://api.scaleway.test"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestNewRejectsNilConfig(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("expected nil config to be rejected")
	}
}

func TestDocumentedConfigKeysAreAccepted(t *testing.T) {
	v := viper.New()
	v.Set("updaters.use", "scaleway")
	v.Set("updaters.scaleway.access_key", testAccessKey)
	v.Set("updaters.scaleway.secret_key", testSecretKey)
	v.Set("updaters.scaleway.project_id", testProjectID)
	v.Set("updaters.scaleway.domain", "home.example.com")

	var cfg Config
	if err := v.UnmarshalKey("updaters.scaleway", &cfg); err != nil {
		t.Fatalf("UnmarshalKey returned an error: %v", err)
	}
	scalewayUpdater, err := New(&cfg)
	if err != nil {
		t.Fatalf("New returned an error: %v", err)
	}

	if scalewayUpdater.config.AccessKey != testAccessKey ||
		scalewayUpdater.config.SecretKey != testSecretKey ||
		scalewayUpdater.config.ProjectID != testProjectID {
		t.Fatalf("unexpected Scaleway credentials: %#v", scalewayUpdater.config)
	}
}

func TestNewNormalizesAndSplitsRecord(t *testing.T) {
	tests := []struct {
		name       string
		domain     string
		zone       string
		wantDomain string
		wantZone   string
		wantRecord string
	}{
		{
			name:       "infer public suffix zone",
			domain:     "Home.Example.CO.UK.",
			wantDomain: "home.example.co.uk",
			wantZone:   "example.co.uk",
			wantRecord: "home",
		},
		{
			name:       "preserve explicit subzone",
			domain:     "Host.Dev.Example.COM.",
			zone:       "DEV.Example.COM.",
			wantDomain: "host.dev.example.com",
			wantZone:   "dev.example.com",
			wantRecord: "host",
		},
		{
			name:       "apex record uses empty API name",
			domain:     "Example.COM.",
			wantDomain: "example.com",
			wantZone:   "example.com",
			wantRecord: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scalewayUpdater, err := New(testConfig(tt.domain, tt.zone))
			if err != nil {
				t.Fatalf("New returned an error: %v", err)
			}
			if scalewayUpdater.config.Domain != tt.wantDomain {
				t.Fatalf("domain = %q, want %q", scalewayUpdater.config.Domain, tt.wantDomain)
			}
			if scalewayUpdater.config.Zone != tt.wantZone {
				t.Fatalf("zone = %q, want %q", scalewayUpdater.config.Zone, tt.wantZone)
			}
			if scalewayUpdater.recordName != tt.wantRecord {
				t.Fatalf("record name = %q, want %q", scalewayUpdater.recordName, tt.wantRecord)
			}
		})
	}
}

func TestNewRejectsRecordOutsideExplicitZone(t *testing.T) {
	_, err := New(testConfig("home.example.net", "example.com"))
	if err == nil {
		t.Fatal("expected a record outside the explicit zone to be rejected")
	}
}

func TestUpdateSetsEachAddressFamilyByIdentifier(t *testing.T) {
	type capturedRequest struct {
		method string
		path   string
		body   domain.UpdateDNSZoneRecordsRequest
		err    error
	}
	requests := make(chan capturedRequest, 1)
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body domain.UpdateDNSZoneRecordsRequest
		err := json.NewDecoder(request.Body).Decode(&body)
		requests <- capturedRequest{
			method: request.Method,
			path:   request.URL.Path,
			body:   body,
			err:    err,
		}
		return jsonResponse(request, `{"records":[]}`), nil
	})

	ttl := 300
	cfg := testConfig("Home.Example.CO.UK.", "")
	cfg.TTL = &ttl
	scalewayUpdater, err := New(cfg)
	if err != nil {
		t.Fatalf("New returned an error: %v", err)
	}
	scalewayUpdater.client = newTestClient(t, transport)

	err = scalewayUpdater.Update(context.Background(), &provider.IpResult{
		IPv4: "203.0.113.10",
		IPv6: "2001:db8::10",
	})
	if err != nil {
		t.Fatalf("Update returned an error: %v", err)
	}

	captured := <-requests
	if captured.err != nil {
		t.Fatalf("decode request: %v", captured.err)
	}
	if captured.method != http.MethodPatch {
		t.Fatalf("method = %s, want PATCH", captured.method)
	}
	if captured.path != "/domain/v2beta1/dns-zones/example.co.uk/records" {
		t.Fatalf("path = %q, want inferred DNS zone path", captured.path)
	}
	if len(captured.body.Changes) != 2 {
		t.Fatalf("change count = %d, want 2", len(captured.body.Changes))
	}
	if captured.body.ReturnAllRecords == nil || *captured.body.ReturnAllRecords {
		t.Fatal("expected return_all_records=false")
	}
	if !captured.body.DisallowNewZoneCreation {
		t.Fatal("expected new DNS zone creation to be disabled")
	}

	assertSetChange(t, captured.body.Changes[0], domain.RecordTypeA, "home", "203.0.113.10", uint32(ttl))
	assertSetChange(t, captured.body.Changes[1], domain.RecordTypeAAAA, "home", "2001:db8::10", uint32(ttl))
}

func TestCurrentUsesNormalizedZoneAndRecord(t *testing.T) {
	requests := make(chan *http.Request, 1)
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests <- request.Clone(request.Context())
		return jsonResponse(request, `{
			"total_count": 2,
			"records": [
				{"name":"home","type":"A","data":"203.0.113.20","ttl":150},
				{"name":"home","type":"AAAA","data":"2001:db8::20","ttl":150}
			]
		}`), nil
	})

	scalewayUpdater, err := New(testConfig("HOME.EXAMPLE.COM.", ""))
	if err != nil {
		t.Fatalf("New returned an error: %v", err)
	}
	scalewayUpdater.client = newTestClient(t, transport)

	current, err := scalewayUpdater.Current(context.Background(), provider.FamilyRequest{
		IPv4: true,
		IPv6: true,
	})
	if err != nil {
		t.Fatalf("Current returned an error: %v", err)
	}
	if current.IPv4 != "203.0.113.20" || current.IPv6 != "2001:db8::20" {
		t.Fatalf("unexpected current addresses: %#v", current)
	}

	request := <-requests
	if request.URL.Path != "/domain/v2beta1/dns-zones/example.com/records" {
		t.Fatalf("path = %q, want normalized DNS zone path", request.URL.Path)
	}
	if got := request.URL.Query().Get("name"); got != "home" {
		t.Fatalf("record filter = %q, want home", got)
	}
}

func assertSetChange(
	t *testing.T,
	change *domain.RecordChange,
	recordType domain.RecordType,
	name string,
	data string,
	ttl uint32,
) {
	t.Helper()

	if change.Set == nil {
		t.Fatal("expected a set change")
	}
	if change.Set.ID != nil {
		t.Fatal("expected the set change to use ID fields")
	}
	if change.Set.IDFields == nil {
		t.Fatal("expected set ID fields")
	}
	if change.Set.IDFields.Name != name || change.Set.IDFields.Type != recordType {
		t.Fatalf("unexpected set identifier: %#v", change.Set.IDFields)
	}
	if len(change.Set.Records) != 1 {
		t.Fatalf("record count = %d, want 1", len(change.Set.Records))
	}

	record := change.Set.Records[0]
	if record.Name != name || record.Type != recordType || record.Data != data || record.TTL != ttl {
		t.Fatalf("unexpected replacement record: %#v", record)
	}
}

func newTestClient(t *testing.T, transport http.RoundTripper) *scw.Client {
	t.Helper()

	client, err := scw.NewClient(
		scw.WithoutAuth(),
		scw.WithAPIURL(testAPIURL),
		scw.WithHTTPClient(&http.Client{Transport: transport}),
	)
	if err != nil {
		t.Fatalf("create test Scaleway client: %v", err)
	}
	return client
}

func jsonResponse(request *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: int64(len(body)),
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:    io.NopCloser(strings.NewReader(body)),
		Request: request,
	}
}

func testConfig(record, zone string) *Config {
	return &Config{
		Domain:    record,
		Zone:      zone,
		ProjectID: testProjectID,
		AccessKey: testAccessKey,
		SecretKey: testSecretKey,
	}
}
