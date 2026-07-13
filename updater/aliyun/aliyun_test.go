package aliyun

import (
	"context"
	"errors"
	"net/http"
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
