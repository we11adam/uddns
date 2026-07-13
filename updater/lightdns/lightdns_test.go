package lightdns

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we11adam/uddns/internal/restyretry"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestUpdateIPRedactsKey(t *testing.T) {
	key := "light+/key =secret"

	t.Run("transport error", func(t *testing.T) {
		lightDNS := mustNewLightDNS(t, &Config{Domain: "home.example.com", Key: key})
		lightDNS.httpclient.SetRetryCount(0)
		lightDNS.httpclient.SetTransport(roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return nil, errors.New("request failed for " + url.PathEscape(key))
		}))

		err := lightDNS.updateIP(context.Background(), "192.0.2.1")
		if err == nil {
			t.Fatal("expected transport error")
		}
		assertKeyRedacted(t, err.Error(), key)
	})

	t.Run("response body", func(t *testing.T) {
		lightDNS := mustNewLightDNS(t, &Config{Domain: "home.example.com", Key: key})
		lightDNS.httpclient.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body := "invalid key " + key + " / " + url.QueryEscape(key)
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    request,
			}, nil
		}))

		err := lightDNS.updateIP(context.Background(), "192.0.2.1")
		if err == nil {
			t.Fatal("expected response error")
		}
		assertKeyRedacted(t, err.Error(), key)
	})
}

func TestUpdateIPRetriesTransientFailures(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch attempts.Add(1) {
		case 1:
			http.Error(w, "temporary failure", http.StatusInternalServerError)
		case 2:
			http.Error(w, "rate limited", http.StatusTooManyRequests)
		default:
			_, _ = io.WriteString(w, "OK")
		}
	}))
	defer server.Close()

	lightDNS := mustNewLightDNS(t, &Config{Domain: "home.example.com", Key: "key"})
	lightDNS.httpclient.SetBaseURL(server.URL).
		SetRetryWaitTime(time.Millisecond).
		SetRetryMaxWaitTime(2 * time.Millisecond)
	if err := lightDNS.updateIP(context.Background(), "192.0.2.1"); err != nil {
		t.Fatalf("update after transient failures: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestUpdateIPDoesNotRetryPermanentClientError(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(w, "invalid request", http.StatusBadRequest)
	}))
	defer server.Close()

	lightDNS := mustNewLightDNS(t, &Config{Domain: "home.example.com", Key: "key"})
	lightDNS.httpclient.SetBaseURL(server.URL).
		SetRetryWaitTime(time.Millisecond).
		SetRetryMaxWaitTime(2 * time.Millisecond)
	if err := lightDNS.updateIP(context.Background(), "192.0.2.1"); err == nil {
		t.Fatal("expected permanent client error")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestUpdateIPStopsRetriesWhenContextIsCanceled(t *testing.T) {
	var attempts atomic.Int32
	firstResponse := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt := attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "temporary failure")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		if attempt == 1 {
			close(firstResponse)
		}
	}))
	defer server.Close()

	lightDNS := mustNewLightDNS(t, &Config{Domain: "home.example.com", Key: "key"})
	lightDNS.httpclient.SetBaseURL(server.URL).
		SetRetryWaitTime(500 * time.Millisecond).
		SetRetryMaxWaitTime(500 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- lightDNS.updateIP(ctx, "192.0.2.1")
	}()

	select {
	case <-firstResponse:
		cancel()
	case <-time.After(time.Second):
		cancel()
		t.Fatal("first request did not complete")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected context cancellation error")
		}
	case <-time.After(time.Second):
		t.Fatal("retry loop did not stop after context cancellation")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestUpdateIPPropagatesContext(t *testing.T) {
	type contextKey struct{}
	key := contextKey{}
	ctx := context.WithValue(context.Background(), key, "request-value")
	lightDNS := mustNewLightDNS(t, &Config{Domain: "home.example.com", Key: "key"})
	lightDNS.httpclient.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Context().Value(key); got != "request-value" {
			t.Fatalf("expected request context value, got %#v", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("OK")),
			Request:    request,
		}, nil
	}))

	if err := lightDNS.updateIP(ctx, "192.0.2.1"); err != nil {
		t.Fatalf("update IP: %v", err)
	}
}

func TestNewNormalizesAndValidatesDomain(t *testing.T) {
	lightDNS := mustNewLightDNS(t, &Config{Domain: " Home.Example.COM. ", Key: "key"})
	if lightDNS.config.Domain != "home.example.com" {
		t.Fatalf("domain = %q, want %q", lightDNS.config.Domain, "home.example.com")
	}
	if lightDNS.httpclient.ResponseBodyLimit != responseBodyLimit {
		t.Fatalf("response body limit = %d, want %d", lightDNS.httpclient.ResponseBodyLimit, responseBodyLimit)
	}
	if lightDNS.httpclient.GetClient().Timeout != requestTimeout {
		t.Fatalf("request timeout = %s, want %s", lightDNS.httpclient.GetClient().Timeout, requestTimeout)
	}
	if lightDNS.httpclient.RetryCount != restyretry.MaxRetries {
		t.Fatalf("retry count = %d, want %d", lightDNS.httpclient.RetryCount, restyretry.MaxRetries)
	}

	for _, domain := range []string{
		"",
		"   ",
		"home..example.com",
		"home_name.example.com",
		"-home.example.com",
		"home-.example.com",
		"home/example.com",
	} {
		t.Run(domain, func(t *testing.T) {
			if _, err := New(&Config{Domain: domain, Key: "key"}); err == nil {
				t.Fatalf("expected domain %q to be rejected", domain)
			}
		})
	}
}

func mustNewLightDNS(t *testing.T, cfg *Config) *LightDNS {
	t.Helper()
	lightDNS, err := New(cfg)
	if err != nil {
		t.Fatalf("new LightDNS updater: %v", err)
	}
	return lightDNS
}

func assertKeyRedacted(t *testing.T, value, key string) {
	t.Helper()
	for _, sensitive := range []string{key, url.QueryEscape(key), url.PathEscape(key)} {
		if strings.Contains(value, sensitive) {
			t.Fatalf("error still contains key %q: %q", sensitive, value)
		}
	}
}
