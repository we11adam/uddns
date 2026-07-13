package proxyurl

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{name: "HTTP", rawURL: "http://proxy.example:8080"},
		{name: "HTTPS with path slash", rawURL: "https://proxy.example/"},
		{name: "userinfo", rawURL: "http://user:secret@proxy.example:8080"},
		{name: "escaped userinfo", rawURL: "https://user:p%40ss@proxy.example"},
		{name: "IPv6 host", rawURL: "http://[2001:db8::1]:8080"},
		{name: "empty", rawURL: "", wantErr: true},
		{name: "relative", rawURL: "proxy.example:8080", wantErr: true},
		{name: "scheme relative", rawURL: "//proxy.example:8080", wantErr: true},
		{name: "unsupported scheme", rawURL: "socks5://proxy.example:1080", wantErr: true},
		{name: "missing host", rawURL: "http:///proxy", wantErr: true},
		{name: "query", rawURL: "http://proxy.example?token=secret", wantErr: true},
		{name: "empty query", rawURL: "http://proxy.example?", wantErr: true},
		{name: "fragment", rawURL: "http://proxy.example#secret", wantErr: true},
		{name: "empty fragment", rawURL: "http://proxy.example#", wantErr: true},
		{name: "path", rawURL: "http://proxy.example/tunnel", wantErr: true},
		{name: "encoded path", rawURL: "http://proxy.example/%2F", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.rawURL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse returned err=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestParseErrorDoesNotExposeCredentials(t *testing.T) {
	rawURL := "http://user:super-secret@proxy.example:%zz"
	_, err := Parse(rawURL)
	if err == nil {
		t.Fatal("expected invalid proxy URL to fail")
	}
	for _, sensitive := range []string{rawURL, "user", "super-secret"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("error exposes proxy credentials %q: %v", sensitive, err)
		}
	}
}
