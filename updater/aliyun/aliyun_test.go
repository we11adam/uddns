package aliyun

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alibabacloud-go/tea/dara"
	"github.com/we11adam/uddns/provider"
)

type httpClientFunc func(*http.Request, *http.Transport) (*http.Response, error)

func (f httpClientFunc) Call(request *http.Request, transport *http.Transport) (*http.Response, error) {
	return f(request, transport)
}

func TestNewUsesHTTPSAndTimeouts(t *testing.T) {
	aliyun, err := New(&Config{
		AccessKeyID:     "access-key-id",
		AccessKeySecret: "access-key-secret",
		Domain:          "ddns.example.com",
	})
	if err != nil {
		t.Fatalf("create Aliyun updater: %v", err)
	}

	if got := dara.StringValue(aliyun.client.Protocol); got != "HTTPS" {
		t.Fatalf("expected Aliyun API protocol HTTPS, got %q", got)
	}
	if got := dara.IntValue(aliyun.client.ConnectTimeout); got != int(connectTimeout.Milliseconds()) {
		t.Fatalf("expected connect timeout %dms, got %dms", connectTimeout.Milliseconds(), got)
	}
	if got := dara.IntValue(aliyun.client.ReadTimeout); got != int(readTimeout.Milliseconds()) {
		t.Fatalf("expected read timeout %dms, got %dms", readTimeout.Milliseconds(), got)
	}
	if got := dara.IntValue(aliyun.runtime.ConnectTimeout); got != int(connectTimeout.Milliseconds()) {
		t.Fatalf("expected runtime connect timeout %dms, got %dms", connectTimeout.Milliseconds(), got)
	}
	if got := dara.IntValue(aliyun.runtime.ReadTimeout); got != int(readTimeout.Milliseconds()) {
		t.Fatalf("expected runtime read timeout %dms, got %dms", readTimeout.Milliseconds(), got)
	}
}

func TestNewRejectsNilConfig(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("expected nil config to be rejected")
	}
}

func TestBoundedHTTPClientLimitsResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, strings.Repeat("x", responseBodyMax+1))
	}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	transport, ok := server.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("test server transport has type %T", server.Client().Transport)
	}
	response, err := (boundedHTTPClient{}).Call(request, transport)
	if err != nil {
		t.Fatalf("call bounded client: %v", err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read bounded response: %v", err)
	}
	if len(content) != responseBodyMax {
		t.Fatalf("response body length = %d, want %d", len(content), responseBodyMax)
	}
}

func TestOperationsCheckContextBeforeRequest(t *testing.T) {
	aliyun, err := New(&Config{
		AccessKeyID:     "access-key-id",
		AccessKeySecret: "access-key-secret",
		Domain:          "ddns.example.com",
	})
	if err != nil {
		t.Fatalf("create Aliyun updater: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("Update", func(t *testing.T) {
		err := aliyun.Update(ctx, &provider.IpResult{IPv4: "192.0.2.1"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled update, got %v", err)
		}
	})

	t.Run("Current", func(t *testing.T) {
		_, err := aliyun.Current(ctx, provider.FamilyRequest{IPv4: true})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled read, got %v", err)
		}
	})
}

func TestUpdateCancelsInFlightRequest(t *testing.T) {
	aliyun, err := New(&Config{
		AccessKeyID:     "access-key-id",
		AccessKeySecret: "access-key-secret",
		Domain:          "ddns.example.com",
	})
	if err != nil {
		t.Fatalf("create Aliyun updater: %v", err)
	}

	requests := make(chan *http.Request, 1)
	aliyun.client.HttpClient = httpClientFunc(func(request *http.Request, _ *http.Transport) (*http.Response, error) {
		select {
		case requests <- request:
		default:
		}
		<-request.Context().Done()
		return nil, request.Context().Err()
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- aliyun.Update(ctx, &provider.IpResult{IPv4: "192.0.2.1"})
	}()

	select {
	case request := <-requests:
		if request.URL.Scheme != "https" {
			t.Fatalf("expected HTTPS request, got %s", request.URL.Scheme)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Aliyun request to start")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled in-flight update, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Aliyun update did not stop after context cancellation")
	}
}

func TestDuplicateDNSRecordsAreReconciledAndMixedValuesTriggerRepair(t *testing.T) {
	aliyun, err := New(&Config{
		AccessKeyID:     "access-key-id",
		AccessKeySecret: "access-key-secret",
		Domain:          "home.example.com",
	})
	if err != nil {
		t.Fatalf("create Aliyun updater: %v", err)
	}

	updates := make(map[string]string)
	aliyun.client.HttpClient = httpClientFunc(func(request *http.Request, _ *http.Transport) (*http.Response, error) {
		form := request.URL.Query()
		if request.Body != nil {
			encoded, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			bodyForm, err := url.ParseQuery(string(encoded))
			if err != nil {
				return nil, err
			}
			for key, values := range bodyForm {
				for _, value := range values {
					form.Add(key, value)
				}
			}
		}
		action := form.Get("Action")
		if action == "" {
			for name, values := range request.Header {
				if strings.EqualFold(name, "x-acs-action") && len(values) > 0 {
					action = values[0]
					break
				}
			}
		}

		body := ""
		switch action {
		case "DescribeSubDomainRecords":
			body = `{
				"TotalCount":3,
				"PageNumber":1,
				"PageSize":100,
				"DomainRecords":{"Record":[
					{"RecordId":"current","Value":"192.0.2.10"},
					{"RecordId":"stale-one","Value":"192.0.2.11"},
					{"RecordId":"stale-two","Value":"192.0.2.12"}
				]}
			}`
		case "UpdateDomainRecord":
			recordID := form.Get("RecordId")
			updates[recordID] = form.Get("Value")
			body = `{"RecordId":"` + recordID + `"}`
		default:
			return nil, errors.New("unexpected Aliyun action: " + action)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})

	const desired = "192.0.2.10"
	if err := aliyun.updateDNSRecord(context.Background(), recordTypeA, desired); err != nil {
		t.Fatalf("update duplicate records: %v", err)
	}
	if len(updates) != 2 || updates["stale-one"] != desired || updates["stale-two"] != desired {
		t.Fatalf("updated records = %#v, want both stale records", updates)
	}
	if _, ok := updates["current"]; ok {
		t.Fatal("already-current record was updated")
	}

	current, err := aliyun.currentDNSRecord(context.Background(), recordTypeA)
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
			aliyun, err := New(&Config{
				AccessKeyID:     "access-key-id",
				AccessKeySecret: "access-key-secret",
				Domain:          "home.example.com",
			})
			if err != nil {
				t.Fatalf("create Aliyun updater: %v", err)
			}

			var requestedTypes []string
			aliyun.client.HttpClient = httpClientFunc(func(request *http.Request, _ *http.Transport) (*http.Response, error) {
				parameters := request.URL.Query()
				if request.Body != nil {
					encoded, err := io.ReadAll(request.Body)
					if err != nil {
						return nil, err
					}
					bodyParameters, err := url.ParseQuery(string(encoded))
					if err != nil {
						return nil, err
					}
					for key, values := range bodyParameters {
						for _, value := range values {
							parameters.Add(key, value)
						}
					}
				}

				recordType := parameters.Get("Type")
				requestedTypes = append(requestedTypes, recordType)
				content := "192.0.2.10"
				if recordType == recordTypeAAAA {
					content = "2001:db8::10"
				}
				body := `{
					"TotalCount":1,
					"PageNumber":1,
					"PageSize":100,
					"DomainRecords":{"Record":[{"RecordId":"record-id","Value":"` + content + `"}]}
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(body)),
					Request:    request,
				}, nil
			})

			current, err := aliyun.Current(context.Background(), tt.families)
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
