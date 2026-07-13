package aliyun

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/we11adam/uddns/provider"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestNewUsesHTTPS(t *testing.T) {
	aliyun, err := New(&Config{
		AccessKeyID:     "access-key-id",
		AccessKeySecret: "access-key-secret",
		Domain:          "ddns.example.com",
	})
	if err != nil {
		t.Fatalf("create Aliyun updater: %v", err)
	}

	if got := aliyun.client.GetConfig().Scheme; got != requests.HTTPS {
		t.Fatalf("expected Aliyun API scheme %q, got %q", requests.HTTPS, got)
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

	started := make(chan struct{})
	var startedOnce sync.Once
	aliyun.transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		startedOnce.Do(func() { close(started) })
		<-request.Context().Done()
		return nil, request.Context().Err()
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- aliyun.Update(ctx, &provider.IpResult{IPv4: "192.0.2.1"})
	}()

	select {
	case <-started:
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
