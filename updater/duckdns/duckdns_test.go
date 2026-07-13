package duckdns

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

func TestUpdateIPRedactsToken(t *testing.T) {
	token := "duck+/token =secret"

	t.Run("transport error", func(t *testing.T) {
		duckDNS := mustNewDuckDNS(t, &Config{Domain: "home", Token: token})
		duckDNS.httpclient.SetRetryCount(0)
		duckDNS.httpclient.SetTransport(roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return nil, errors.New("request failed for " + url.PathEscape(token))
		}))

		err := duckDNS.updateIP(context.Background(), "192.0.2.1")
		if err == nil {
			t.Fatal("expected transport error")
		}
		assertTokenRedacted(t, err.Error(), token)
	})

	t.Run("response body", func(t *testing.T) {
		duckDNS := mustNewDuckDNS(t, &Config{Domain: "home", Token: token})
		duckDNS.httpclient.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body := "invalid token " + token + " / " + url.QueryEscape(token)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    request,
			}, nil
		}))

		err := duckDNS.updateIP(context.Background(), "192.0.2.1")
		if err == nil {
			t.Fatal("expected response error")
		}
		assertTokenRedacted(t, err.Error(), token)
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

	duckDNS := mustNewDuckDNS(t, &Config{Domain: "home", Token: "token"})
	duckDNS.httpclient.SetBaseURL(server.URL).
		SetRetryWaitTime(time.Millisecond).
		SetRetryMaxWaitTime(2 * time.Millisecond)
	if err := duckDNS.updateIP(context.Background(), "192.0.2.1"); err != nil {
		t.Fatalf("update after transient failures: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestUpdateIPRequiresHTTP200AndOKBody(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		wantErr      bool
		wantAttempts int32
	}{
		{name: "HTTP 200 and OK", status: http.StatusOK, body: "OK", wantAttempts: 1},
		{name: "HTTP 500 and OK", status: http.StatusInternalServerError, body: "OK", wantErr: true, wantAttempts: int32(restyretry.MaxRetries + 1)},
		{name: "HTTP 200 and KO", status: http.StatusOK, body: "KO", wantErr: true, wantAttempts: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()

			const token = "duck-secret-token"
			duckDNS := mustNewDuckDNS(t, &Config{Domain: "home", Token: token})
			duckDNS.httpclient.SetBaseURL(server.URL).
				SetRetryWaitTime(time.Millisecond).
				SetRetryMaxWaitTime(2 * time.Millisecond)
			err := duckDNS.updateIP(context.Background(), "192.0.2.1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("update error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				assertTokenRedacted(t, err.Error(), token)
			}
			if got := attempts.Load(); got != tt.wantAttempts {
				t.Fatalf("attempts = %d, want %d", got, tt.wantAttempts)
			}
		})
	}
}

func TestUpdateIPDoesNotRetryPermanentClientError(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(w, "invalid request", http.StatusBadRequest)
	}))
	defer server.Close()

	duckDNS := mustNewDuckDNS(t, &Config{Domain: "home", Token: "token"})
	duckDNS.httpclient.SetBaseURL(server.URL).
		SetRetryWaitTime(time.Millisecond).
		SetRetryMaxWaitTime(2 * time.Millisecond)
	if err := duckDNS.updateIP(context.Background(), "192.0.2.1"); err == nil {
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

	duckDNS := mustNewDuckDNS(t, &Config{Domain: "home", Token: "token"})
	duckDNS.httpclient.SetBaseURL(server.URL).
		SetRetryWaitTime(500 * time.Millisecond).
		SetRetryMaxWaitTime(500 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- duckDNS.updateIP(ctx, "192.0.2.1")
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
	duckDNS := mustNewDuckDNS(t, &Config{Domain: "home", Token: "token"})
	duckDNS.httpclient.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
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

	if err := duckDNS.updateIP(ctx, "192.0.2.1"); err != nil {
		t.Fatalf("update IP: %v", err)
	}
}

func TestNewNormalizesAndValidatesDomain(t *testing.T) {
	duckDNS := mustNewDuckDNS(t, &Config{Domain: " Home.DuckDNS.org. ", Token: "token"})
	if duckDNS.config.Domain != "home.duckdns.org" {
		t.Fatalf("domain = %q, want %q", duckDNS.config.Domain, "home.duckdns.org")
	}
	if duckDNS.httpclient.ResponseBodyLimit != responseBodyLimit {
		t.Fatalf("response body limit = %d, want %d", duckDNS.httpclient.ResponseBodyLimit, responseBodyLimit)
	}
	if duckDNS.httpclient.GetClient().Timeout != requestTimeout {
		t.Fatalf("request timeout = %s, want %s", duckDNS.httpclient.GetClient().Timeout, requestTimeout)
	}
	if duckDNS.httpclient.RetryCount != restyretry.MaxRetries {
		t.Fatalf("retry count = %d, want %d", duckDNS.httpclient.RetryCount, restyretry.MaxRetries)
	}

	for _, domain := range []string{
		"",
		"   ",
		"home..duckdns.org",
		"home_name.duckdns.org",
		"-home.duckdns.org",
		"home-.duckdns.org",
		"home/duckdns.org",
	} {
		t.Run(domain, func(t *testing.T) {
			if _, err := New(&Config{Domain: domain, Token: "token"}); err == nil {
				t.Fatalf("expected domain %q to be rejected", domain)
			}
		})
	}
}

func mustNewDuckDNS(t *testing.T, cfg *Config) *DuckDNS {
	t.Helper()
	duckDNS, err := New(cfg)
	if err != nil {
		t.Fatalf("new DuckDNS updater: %v", err)
	}
	return duckDNS
}

func assertTokenRedacted(t *testing.T, value, token string) {
	t.Helper()
	for _, sensitive := range []string{token, url.QueryEscape(token), url.PathEscape(token)} {
		if strings.Contains(value, sensitive) {
			t.Fatalf("error still contains token %q: %q", sensitive, value)
		}
	}
}
