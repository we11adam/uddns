package telegram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/we11adam/uddns/internal/retry"
	"github.com/we11adam/uddns/internal/testutil"
	"github.com/we11adam/uddns/notifier"
)

type failingTransport struct {
	message string
}

func (f failingTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, errors.New(f.message)
}

func TestHTTPClientTimeout(t *testing.T) {
	t.Parallel()
	client := newHTTPClient("token", nil)
	if got := client.GetClient().Timeout; got != requestTimeout {
		t.Fatalf("expected Telegram request timeout %s, got %s", requestTimeout, got)
	}
	if client.ResponseBodyLimit != responseBodyLimit {
		t.Fatalf("expected Telegram response body limit %d, got %d", responseBodyLimit, client.ResponseBodyLimit)
	}
}

func TestNewValidatesProxyURLWithoutExposingCredentials(t *testing.T) {
	t.Parallel()
	_, err := New(&Telegram{
		Token:  "token",
		ChatID: "123456",
		Proxy:  "http://user:proxy-secret@proxy.example?token=hidden",
	})
	if err == nil {
		t.Fatal("expected invalid proxy URL to fail")
	}
	for _, sensitive := range []string{"user", "proxy-secret", "hidden"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("proxy validation error exposes %q: %v", sensitive, err)
		}
	}

	if _, err := New(&Telegram{
		Token:  "token",
		ChatID: "123456",
		Proxy:  "http://user:proxy-secret@proxy.example:8080/",
	}); err != nil {
		t.Fatalf("expected valid authenticated proxy URL: %v", err)
	}
}

func TestNotifyRedactsTokenFromTransportError(t *testing.T) {
	t.Parallel()
	token := "telegram+/token =secret"
	telegram := &Telegram{
		Token:  token,
		ChatID: "123456",
		hc: resty.New().
			SetBaseURL("https://api.telegram.org/bot" + url.PathEscape(token) + "/sendMessage").
			SetTransport(failingTransport{message: "request failed for " + url.QueryEscape(token)}),
	}

	err := telegram.Notify(context.Background(), notifier.Notification{Message: "test"})
	if err == nil {
		t.Fatal("expected transport error")
	}
	testutil.AssertTokenRedacted(t, err.Error(), token)
}

func TestNotifyChecksTelegramAPIResponse(t *testing.T) {
	t.Parallel()
	token := "telegram+/token =secret"
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			body:       `{"ok":true,"result":{}}`,
		},
		{
			name:       "http 400",
			statusCode: http.StatusBadRequest,
			body:       `{"ok":false,"error_code":400,"description":"bad token ` + token + ` / ` + url.QueryEscape(token) + `"}`,
			wantErr:    true,
		},
		{
			name:       "http 200 api error",
			statusCode: http.StatusOK,
			body:       `{"ok":false,"error_code":400,"description":"bad token ` + token + ` / ` + url.PathEscape(token) + `"}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			telegram := &Telegram{
				Token:  token,
				ChatID: "123456",
				hc:     resty.New().SetBaseURL(server.URL),
			}
			err := telegram.Notify(context.Background(), notifier.Notification{Message: "test"})
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if err != nil {
				testutil.AssertTokenRedacted(t, err.Error(), token)
			}
		})
	}
}

func TestNotifyRetriesAndClassifiesFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		responses      []string
		statusCodes    []int
		wantAttempts   int32
		wantErr        bool
		wantPermanent  bool
		wantRetryAfter time.Duration
	}{
		{
			name:         "transient server error",
			statusCodes:  []int{http.StatusBadGateway, http.StatusOK},
			responses:    []string{`{"ok":false,"error_code":502,"description":"Bad Gateway"}`, `{"ok":true,"result":{}}`},
			wantAttempts: 2,
		},
		{
			name:           "long rate limit",
			statusCodes:    []int{http.StatusTooManyRequests},
			responses:      []string{`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 30","parameters":{"retry_after":30}}`},
			wantAttempts:   1,
			wantErr:        true,
			wantRetryAfter: 30 * time.Second,
		},
		{
			name:          "unauthorized",
			statusCodes:   []int{http.StatusUnauthorized},
			responses:     []string{`{"ok":false,"error_code":401,"description":"Unauthorized"}`},
			wantAttempts:  1,
			wantErr:       true,
			wantPermanent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempt := min(int(attempts.Add(1)), len(tt.responses)) - 1
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCodes[attempt])
				_, _ = w.Write([]byte(tt.responses[attempt]))
			}))
			defer server.Close()

			telegram := &Telegram{
				Token:  "token",
				ChatID: "123456",
				hc:     newHTTPClient("token", nil).SetBaseURL(server.URL).SetRetryWaitTime(time.Millisecond),
			}
			err := telegram.Notify(context.Background(), notifier.Notification{Message: "test"})
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if got := attempts.Load(); got != tt.wantAttempts {
				t.Fatalf("attempts = %d, want %d", got, tt.wantAttempts)
			}
			if retry.IsPermanent(err) != tt.wantPermanent {
				t.Fatalf("permanent = %t, want %t for %v", retry.IsPermanent(err), tt.wantPermanent, err)
			}
			if delay, _ := retry.RetryAfter(err); delay != tt.wantRetryAfter {
				t.Fatalf("retry after = %s, want %s", delay, tt.wantRetryAfter)
			}
		})
	}
}

func TestNotifyCancelsInFlightRequest(t *testing.T) {
	t.Parallel()
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
	}))
	defer server.Close()
	defer close(releaseRequest)

	telegram := &Telegram{
		Token:  "token",
		ChatID: "123456",
		hc:     resty.New().SetBaseURL(server.URL),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- telegram.Notify(ctx, notifier.Notification{Message: "test"})
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("Telegram request did not start")
	}
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected canceled Telegram request to return an error")
		}
	case <-time.After(time.Second):
		t.Fatal("Telegram request did not return after context cancellation")
	}
}
