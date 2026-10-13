package restyretry

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
)

func TestConfigureTransient(t *testing.T) {
	t.Parallel()
	client := ConfigureTransient(resty.New())
	if client.RetryCount != MaxRetries {
		t.Fatalf("retry count = %d, want %d", client.RetryCount, MaxRetries)
	}
	if client.RetryWaitTime != WaitTime {
		t.Fatalf("retry wait time = %s, want %s", client.RetryWaitTime, WaitTime)
	}
	if client.RetryMaxWaitTime != MaxWaitTime {
		t.Fatalf("retry max wait time = %s, want %s", client.RetryMaxWaitTime, MaxWaitTime)
	}
	if len(client.RetryConditions) != 1 {
		t.Fatalf("retry conditions = %d, want 1", len(client.RetryConditions))
	}
	if client.RetryAfter == nil {
		t.Fatal("RetryAfter must honor Retry-After headers")
	}
	if delay, err := client.RetryAfter(client, &resty.Response{}); delay != 0 || err != nil {
		t.Fatalf("RetryAfter without a header = %s, %v; want Resty's default backoff", delay, err)
	}
}

func TestShouldRetry(t *testing.T) {
	t.Parallel()
	response := func(status int) *resty.Response {
		return &resty.Response{RawResponse: &http.Response{StatusCode: status}}
	}
	tests := []struct {
		name     string
		response *resty.Response
		err      error
		want     bool
	}{
		{name: "network error", err: &url.Error{Op: "Get", URL: "https://example.com", Err: errors.New("connection reset")}, want: true},
		{name: "net error", err: &net.DNSError{Err: "temporary DNS failure", Name: "example.com", IsTemporary: true}, want: true},
		{name: "generic error", err: errors.New("request configuration failed")},
		{name: "rate limit", response: response(http.StatusTooManyRequests), want: true},
		{name: "server error", response: response(http.StatusServiceUnavailable), want: true},
		{name: "request timeout response", response: response(http.StatusRequestTimeout)},
		{name: "bad request", response: response(http.StatusBadRequest)},
		{name: "success", response: response(http.StatusOK)},
		{name: "empty response", response: &resty.Response{}},
		{name: "no response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldRetry(tt.response, tt.err); got != tt.want {
				t.Fatalf("shouldRetry() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeaderDelay(t *testing.T) {
	t.Parallel()
	response := func(value string) *resty.Response {
		header := http.Header{}
		if value != "" {
			header.Set("Retry-After", value)
		}
		return &resty.Response{RawResponse: &http.Response{StatusCode: http.StatusTooManyRequests, Header: header}}
	}
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "missing"},
		{name: "seconds", value: "3", want: 3 * time.Second},
		{name: "fractional seconds", value: "0.5", want: 500 * time.Millisecond},
		{name: "negative", value: "-1"},
		{name: "past date", value: "Mon, 02 Jan 2006 15:04:05 GMT"},
		{name: "invalid", value: "soon"},
	}
	for _, tt := range tests {
		if got := HeaderDelay(response(tt.value)); got != tt.want {
			t.Errorf("%s: HeaderDelay = %s, want %s", tt.name, got, tt.want)
		}
	}
	future := time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)
	if got := HeaderDelay(response(future)); got < 59*time.Minute || got > time.Hour {
		t.Errorf("date: HeaderDelay = %s, want about 1h", got)
	}
	if got := HeaderDelay(nil); got != 0 {
		t.Errorf("nil response: HeaderDelay = %s, want 0", got)
	}
}

func TestConfigureTransientHonorsRetryAfter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		retryAfter   string
		wantAttempts int32
	}{
		{name: "short", retryAfter: "0.01", wantAttempts: MaxRetries + 1},
		{name: "long", retryAfter: "60", wantAttempts: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.Header().Set("Retry-After", tt.retryAfter)
				w.WriteHeader(http.StatusTooManyRequests)
			}))
			defer server.Close()

			client := ConfigureTransient(resty.New()).SetRetryWaitTime(time.Millisecond)
			response, err := client.R().Get(server.URL)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if response.StatusCode() != http.StatusTooManyRequests {
				t.Fatalf("status = %d, want 429", response.StatusCode())
			}
			if got := attempts.Load(); got != tt.wantAttempts {
				t.Fatalf("attempts = %d, want %d", got, tt.wantAttempts)
			}
		})
	}
}
