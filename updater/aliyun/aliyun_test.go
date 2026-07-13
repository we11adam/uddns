package aliyun

import (
	"context"
	"errors"
	"io"
	"net/http"
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
		_, err := aliyun.Current(ctx)
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
