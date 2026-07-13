package telegram

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/we11adam/uddns/notifier"
)

type failingTransport struct {
	message string
}

func (f failingTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, errors.New(f.message)
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

	err := telegram.Notify(notifier.Notification{Message: "test"})
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
			err := telegram.Notify(notifier.Notification{Message: "test"})
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if err != nil {
				assertTokenRedacted(t, err.Error(), token)
			}
		})
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
