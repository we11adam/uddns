package provider

import "testing"

func TestIpResultValidate(t *testing.T) {
	tests := []struct {
		name    string
		result  *IpResult
		wantErr bool
	}{
		{name: "ipv4", result: &IpResult{IPv4: "192.0.2.10"}},
		{name: "ipv6", result: &IpResult{IPv6: "2001:db8::1"}},
		{name: "both", result: &IpResult{IPv4: "192.0.2.10", IPv6: "2001:db8::1"}},
		{name: "nil", result: nil, wantErr: true},
		{name: "empty", result: &IpResult{}, wantErr: true},
		{name: "invalid ipv4", result: &IpResult{IPv4: "not-an-ip"}, wantErr: true},
		{name: "ipv6 in ipv4 field", result: &IpResult{IPv4: "2001:db8::1"}, wantErr: true},
		{name: "ipv4 in ipv6 field", result: &IpResult{IPv6: "192.0.2.10"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.result.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected wantErr=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}
