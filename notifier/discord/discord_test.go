package discord

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"

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
			client, err := New(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				baseURL := client.hc.BaseURL
				if baseURL != tt.wantBase {
					t.Errorf("New() BaseURL = %v, want %v", baseURL, tt.wantBase)
				}
			}
		})
	}
}

func TestNotifyRedactTokenFromTransportError(t *testing.T) {
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

func TestNotifyDiscordAPIResponse(t *testing.T) {
	token := "discord+/token =secret"
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{
			name:       "http 400",
			statusCode: http.StatusBadRequest,
			body:       `{"code":0,"message":""}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

func TestNotifyCancelsInFlightRequest(t *testing.T) {
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
