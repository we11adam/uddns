package telegram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/we11adam/uddns/notifier"
)

type failingTransport struct {
	message string
}

func (f failingTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, errors.New(f.message)
}

func TestHTTPClientTimeout(t *testing.T) {
	client := newHTTPClient("token", "")
	if got := client.GetClient().Timeout; got != requestTimeout {
		t.Fatalf("expected Telegram request timeout %s, got %s", requestTimeout, got)
	}
	if client.ResponseBodyLimit != responseBodyLimit {
		t.Fatalf("expected Telegram response body limit %d, got %d", responseBodyLimit, client.ResponseBodyLimit)
	}
}

func TestNotifyRedactsTokenFromTransportError(t *testing.T) {
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
	assertTokenRedacted(t, err.Error(), token)
}

func TestNotifyChecksTelegramAPIResponse(t *testing.T) {
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
				assertTokenRedacted(t, err.Error(), token)
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

func assertTokenRedacted(t *testing.T, value, token string) {
	t.Helper()
	for _, sensitive := range []string{token, url.QueryEscape(token), url.PathEscape(token)} {
		if strings.Contains(value, sensitive) {
			t.Fatalf("error still contains token %q: %q", sensitive, value)
		}
	}
}
