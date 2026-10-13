package discord

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestClientBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   *Discord
		wantBase string
		wantErr  bool
	}{
		{
			name: "valid URL",
			config: &Discord{
				URL: "https://discord.com/api/webhooks/ID/TOKEN",
			},
			wantBase: "https://discord.com/api/webhooks/ID/TOKEN",
		},
		{
			name: "valid ID and Token",
			config: &Discord{
				ID:    "ID",
				Token: "TOKEN",
			},
			wantBase: "https://discord.com/api/webhooks/ID/TOKEN",
		},
		{
			name:    "missing URL and ID/Token",
			config:  &Discord{},
			wantErr: true,
		},
		{
			name: "URL, ID and Token provided",
			config: &Discord{
				URL:   "https://discord.com/api/webhooks/ID/TOKEN",
				ID:    "ID",
				Token: "TOKEN",
			},
			wantErr: true,
		},
		{
			name: "only ID provided",
			config: &Discord{
				ID: "ID",
			},
			wantErr: true,
		},
		{
			name: "only Token provided",
			config: &Discord{
				Token: "TOKEN",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, err := New(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				baseURL := client.hc.BaseURL
				if baseURL != tt.wantBase {
					t.Errorf("New() BaseURL = %v, want %v", baseURL, tt.wantBase)
				}
				if got := client.hc.Header.Get("User-Agent"); got != userAgent {
					t.Errorf("New() User-Agent = %q, want %q", got, userAgent)
				}
			}
		})
	}
}

func TestNotifyRedactTokenFromTransportError(t *testing.T) {
	t.Parallel()
	token := "discord+/token =secret"
	discord, err := New(&Discord{
		ID:    "123456",
		Token: token,
	})
	if err != nil {
		t.Fatalf("failed to create Discord client: %v", err)
	}
	discord.hc.SetTransport(failingTransport{message: "request failed for " + url.QueryEscape(discord.Token)})

	err = discord.Notify(context.Background(), notifier.Notification{Message: "test"})
	if err == nil {
		t.Fatal("expected transport error")
	}
	testutil.AssertTokenRedacted(t, err.Error(), token)
}

func TestNotifyRedactsTokenFromURLTransportError(t *testing.T) {
	t.Parallel()
	token := "discord-webhook-token"
	discord, err := New(&Discord{
		URL: "https://discord.com/api/webhooks/123456/" + token,
	})
	if err != nil {
		t.Fatalf("failed to create Discord client: %v", err)
	}
	discord.hc.SetTransport(failingTransport{message: "request failed"})

	err = discord.Notify(context.Background(), notifier.Notification{Message: "test"})
	if err == nil {
		t.Fatal("expected transport error")
	}
	testutil.AssertTokenRedacted(t, err.Error(), token)
}

func TestNotifyDiscordAPIResponse(t *testing.T) {
	t.Parallel()
	token := "discord+/token =secret"
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{
			name:       "http 200",
			statusCode: http.StatusOK,
			body:       `{"id":"message-id"}`,
		},
		{
			name:       "http 204",
			statusCode: http.StatusNoContent,
		},
		{
			name:       "http 201",
			statusCode: http.StatusCreated,
			body:       `{"id":"message-id"}`,
		},
		{
			name:       "http 202 with empty body",
			statusCode: http.StatusAccepted,
		},
		{
			name:       "http 400",
			statusCode: http.StatusBadRequest,
			body:       `{"code":0,"message":""}`,
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

			discord := &Discord{
				ID:    "123456",
				Token: token,
				hc:    resty.New().SetBaseURL(server.URL),
			}
			err := discord.Notify(context.Background(), notifier.Notification{Message: "test"})
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
		statusCodes    []int
		responses      []string
		retryAfter     string
		wantAttempts   int32
		wantErr        bool
		wantPermanent  bool
		wantRetryAfter time.Duration
	}{
		{
			name:         "transient server error",
			statusCodes:  []int{http.StatusServiceUnavailable, http.StatusNoContent},
			responses:    []string{`{"message":"unavailable","code":0}`, ``},
			wantAttempts: 2,
		},
		{
			name:         "short rate limit",
			statusCodes:  []int{http.StatusTooManyRequests, http.StatusNoContent},
			responses:    []string{`{"message":"You are being rate limited.","retry_after":0.01,"global":false}`, ``},
			wantAttempts: 2,
		},
		{
			name:           "long rate limit",
			statusCodes:    []int{http.StatusTooManyRequests},
			responses:      []string{`{"message":"You are being rate limited.","retry_after":12.5,"global":false}`},
			wantAttempts:   1,
			wantErr:        true,
			wantRetryAfter: 12500 * time.Millisecond,
		},
		{
			name:           "rate limit header",
			statusCodes:    []int{http.StatusTooManyRequests},
			retryAfter:     "30",
			wantAttempts:   1,
			wantErr:        true,
			wantRetryAfter: 30 * time.Second,
		},
		{
			name:          "unknown webhook",
			statusCodes:   []int{http.StatusNotFound},
			responses:     []string{`{"message":"Unknown Webhook","code":10015}`},
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
				attempt := min(int(attempts.Add(1)), len(tt.statusCodes)) - 1
				w.Header().Set("Content-Type", "application/json")
				if tt.retryAfter != "" {
					w.Header().Set("Retry-After", tt.retryAfter)
				}
				w.WriteHeader(tt.statusCodes[attempt])
				if attempt < len(tt.responses) {
					_, _ = w.Write([]byte(tt.responses[attempt]))
				}
			}))
			defer server.Close()

			discord, err := New(&Discord{URL: server.URL})
			if err != nil {
				t.Fatalf("create Discord notifier: %v", err)
			}
			discord.hc.SetRetryWaitTime(time.Millisecond)
			err = discord.Notify(context.Background(), notifier.Notification{Message: "test"})
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

	discord := &Discord{
		Token: "token",
		ID:    "123456",
		hc:    resty.New().SetBaseURL(server.URL),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- discord.Notify(ctx, notifier.Notification{Message: "test"})
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("Discord request did not start")
	}
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected canceled Discord request to return an error")
		}
	case <-time.After(time.Second):
		t.Fatal("Discord request did not return after context cancellation")
	}
}
