package routeros

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/we11adam/uddns/provider"
)

func TestRouterOSRestURL(t *testing.T) {
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

func TestGetIPsRequestsOnlySelectedFamilies(t *testing.T) {
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
