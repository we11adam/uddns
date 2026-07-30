package routeros

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/we11adam/uddns/internal/testutil"
	"github.com/we11adam/uddns/provider"
)

func TestRouterOSRestURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		endpoint string
		want     string
		wantErr  bool
	}{
		{name: "https", endpoint: "https://192.0.2.1", want: "https://192.0.2.1/rest"},
		{name: "trim slash", endpoint: "https://192.0.2.1/", want: "https://192.0.2.1/rest"},
		{name: "http", endpoint: "http://router.example.com", want: "http://router.example.com/rest"},
		{name: "missing scheme", endpoint: "router.example.com", wantErr: true},
		{name: "unsupported scheme", endpoint: "ftp://router.example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := routerOSRestURL(tt.endpoint)
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestNewVerifiesTLSByDefault(t *testing.T) {
	t.Parallel()
	router, err := New(&Config{
		Username: "admin",
		Password: "secret",
		Endpoint: "https://router.example.com",
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if router.config.Insecure == nil || *router.config.Insecure {
		t.Fatalf("normalized insecure setting = %v, want false", router.config.Insecure)
	}
	transport, ok := router.httpClient.GetClient().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("HTTP transport type = %T, want *http.Transport", router.httpClient.GetClient().Transport)
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS certificate verification is not enabled by default")
	}
}

func TestNewAllowsExplicitInsecureTLS(t *testing.T) {
	t.Parallel()
	insecure := true
	router, err := New(&Config{
		Username: "admin",
		Password: "secret",
		Endpoint: "https://router.example.com",
		Insecure: &insecure,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	transport, ok := router.httpClient.GetClient().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("HTTP transport type = %T, want *http.Transport", router.httpClient.GetClient().Transport)
	}
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("explicit insecure setting was not applied")
	}
}

func TestGetIPsRequestsOnlySelectedFamilies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		families  provider.FamilyRequest
		wantIPv4  string
		wantIPv6  string
		wantPaths []string
	}{
		{
			name:      "IPv4 only",
			families:  provider.FamilyRequest{IPv4: true},
			wantIPv4:  "192.0.2.10",
			wantPaths: []string{"/interface", "/ip/address"},
		},
		{
			name:      "IPv6 only",
			families:  provider.FamilyRequest{IPv6: true},
			wantIPv6:  "2001:db8::10",
			wantPaths: []string{"/interface", "/ipv6/address"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				paths = append(paths, request.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				switch request.URL.Path {
				case "/interface":
					_, _ = w.Write([]byte(`[{"name":"pppoe-out1","type":"pppoe-out"}]`))
				case "/ip/address":
					_, _ = w.Write([]byte(`[{"interface":"pppoe-out1","address":"192.0.2.10/32"}]`))
				case "/ipv6/address":
					_, _ = w.Write([]byte(`[{"interface":"pppoe-out1","address":"2001:db8::10/64"}]`))
				default:
					http.NotFound(w, request)
				}
			}))
			defer server.Close()

			router := &RouterOS{httpClient: resty.New().SetBaseURL(server.URL)}
			result, err := router.GetIPs(context.Background(), tt.families)
			if err != nil {
				t.Fatalf("get IPs: %v", err)
			}
			if result.IPv4 != tt.wantIPv4 || result.IPv6 != tt.wantIPv6 {
				t.Fatalf("result = %+v, want IPv4=%q IPv6=%q", result, tt.wantIPv4, tt.wantIPv6)
			}
			if !slices.Equal(paths, tt.wantPaths) {
				t.Fatalf("request paths = %v, want %v", paths, tt.wantPaths)
			}
		})
	}
}

func TestGetIPsSkipsDisabledAddresses(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/interface":
			_, _ = w.Write([]byte(`[{"name":"pppoe-out1","type":"pppoe-out"}]`))
		case "/ip/address":
			_, _ = w.Write([]byte(`[
				{"interface":"pppoe-out1","address":"192.0.2.10/32","disabled":"true"},
				{"interface":"pppoe-out1","address":"192.0.2.20/32","disabled":"false"}
			]`))
		case "/ipv6/address":
			_, _ = w.Write([]byte(`[
				{"interface":"pppoe-out1","address":"2001:db8::10/64","disabled":"true"},
				{"interface":"pppoe-out1","address":"2001:db8::20/64","disabled":"false"}
			]`))
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	router := &RouterOS{httpClient: resty.New().SetBaseURL(server.URL)}
	result, err := router.GetIPs(context.Background(), provider.FamilyRequest{IPv4: true, IPv6: true})
	if err != nil {
		t.Fatalf("get IPs: %v", err)
	}
	if result.IPv4 != "192.0.2.20" || result.IPv6 != "2001:db8::20" {
		t.Fatalf("result = %+v, want enabled IPv4 and IPv6 addresses", result)
	}
}

func TestGetIPsRejectsOnlyDisabledAddresses(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/interface":
			_, _ = w.Write([]byte(`[{"name":"pppoe-out1","type":"pppoe-out"}]`))
		case "/ip/address":
			_, _ = w.Write([]byte(`[{"interface":"pppoe-out1","address":"192.0.2.10/32","disabled":"true"}]`))
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	router := &RouterOS{httpClient: resty.New().SetBaseURL(server.URL)}
	if _, err := router.GetIPs(context.Background(), provider.FamilyRequest{IPv4: true}); err == nil {
		t.Fatal("expected disabled-only address list to return an error")
	}
}

func TestGetIPsReportsHTTPFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		failureAt  string
		statusCode int
		families   provider.FamilyRequest
	}{
		{
			name:       "interface redirect",
			failureAt:  "/interface",
			statusCode: http.StatusFound,
			families:   provider.FamilyRequest{IPv4: true},
		},
		{
			name:       "interface unauthorized",
			failureAt:  "/interface",
			statusCode: http.StatusUnauthorized,
			families:   provider.FamilyRequest{IPv4: true},
		},
		{
			name:       "IPv4 forbidden",
			failureAt:  "/ip/address",
			statusCode: http.StatusForbidden,
			families:   provider.FamilyRequest{IPv4: true},
		},
		{
			name:       "IPv6 service unavailable",
			failureAt:  "/ipv6/address",
			statusCode: http.StatusServiceUnavailable,
			families:   provider.FamilyRequest{IPv6: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			password := "router+/password =secret"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.URL.Path == tt.failureAt {
					w.WriteHeader(tt.statusCode)
					_, _ = fmt.Fprintf(
						w,
						"request failed for %s / %s / %s",
						password,
						url.QueryEscape(password),
						url.PathEscape(password),
					)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				switch request.URL.Path {
				case "/interface":
					_, _ = w.Write([]byte(`[{"name":"pppoe-out1","type":"pppoe-out"}]`))
				case "/ip/address":
					_, _ = w.Write([]byte(`[{"interface":"pppoe-out1","address":"192.0.2.10/32"}]`))
				case "/ipv6/address":
					_, _ = w.Write([]byte(`[{"interface":"pppoe-out1","address":"2001:db8::10/64"}]`))
				default:
					http.NotFound(w, request)
				}
			}))
			defer server.Close()

			router := &RouterOS{
				config:     Config{Password: password},
				httpClient: resty.New().SetBaseURL(server.URL),
			}
			_, err := router.GetIPs(context.Background(), tt.families)
			if err == nil {
				t.Fatal("GetIPs returned nil error")
			}
			for _, want := range []string{
				tt.failureAt,
				fmt.Sprintf("HTTP status %d", tt.statusCode),
			} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("GetIPs error = %q, want substring %q", err, want)
				}
			}
			if strings.Contains(err.Error(), "no IP address found") {
				t.Fatalf("HTTP failure was reported as an empty address result: %q", err)
			}
			testutil.AssertTokenRedacted(t, err.Error(), password)
		})
	}
}
