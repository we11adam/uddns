package restyretry

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/go-resty/resty/v2"
)

func TestConfigureTransient(t *testing.T) {
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
	if client.RetryAfter != nil {
		t.Fatal("RetryAfter must remain nil so Resty uses exponential backoff with jitter")
	}
}

func TestShouldRetry(t *testing.T) {
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
			if got := shouldRetry(tt.response, tt.err); got != tt.want {
				t.Fatalf("shouldRetry() = %v, want %v", got, tt.want)
			}
		})
	}
}
