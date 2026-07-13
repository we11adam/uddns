package lightdns

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestUpdateIPRedactsKey(t *testing.T) {
	key := "light+/key =secret"

	t.Run("transport error", func(t *testing.T) {
		lightDNS := New(&Config{Domain: "home.example.com", Key: key})
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
		lightDNS := New(&Config{Domain: "home.example.com", Key: key})
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

func TestUpdateIPPropagatesContext(t *testing.T) {
	type contextKey struct{}
	key := contextKey{}
	ctx := context.WithValue(context.Background(), key, "request-value")
	lightDNS := New(&Config{Domain: "home.example.com", Key: "key"})
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

func assertKeyRedacted(t *testing.T, value, key string) {
	t.Helper()
	for _, sensitive := range []string{key, url.QueryEscape(key), url.PathEscape(key)} {
		if strings.Contains(value, sensitive) {
			t.Fatalf("error still contains key %q: %q", sensitive, value)
		}
	}
}
