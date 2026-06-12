package routeros

import "testing"

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
