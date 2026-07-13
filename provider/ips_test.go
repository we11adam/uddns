package provider

import "testing"

func TestIpResultValidate(t *testing.T) {
	tests := []struct {
		name     string
		result   *IpResult
		wantIPv4 string
		wantIPv6 string
		wantErr  bool
	}{
		{name: "ipv4", result: &IpResult{IPv4: "192.0.2.10"}, wantIPv4: "192.0.2.10"},
		{name: "ipv6", result: &IpResult{IPv6: "2001:db8::1"}, wantIPv6: "2001:db8::1"},
		{name: "both", result: &IpResult{IPv4: "192.0.2.10", IPv6: "2001:db8::1"}, wantIPv4: "192.0.2.10", wantIPv6: "2001:db8::1"},
		{
			name:     "trims whitespace",
			result:   &IpResult{IPv4: " \t192.0.2.10\n", IPv6: " 2001:db8::1 \r\n"},
			wantIPv4: "192.0.2.10",
			wantIPv6: "2001:db8::1",
		},
		{
			name:     "canonicalizes equivalent ipv6",
			result:   &IpResult{IPv6: "2001:0DB8:0000:0000:0000:0000:0000:0001"},
			wantIPv6: "2001:db8::1",
		},
		{name: "nil", result: nil, wantErr: true},
		{name: "empty", result: &IpResult{}, wantErr: true},
		{name: "whitespace only", result: &IpResult{IPv4: " \t\n", IPv6: " \r\n"}, wantErr: true},
		{name: "invalid ipv4", result: &IpResult{IPv4: "not-an-ip"}, wantErr: true},
		{name: "ipv6 in ipv4 field", result: &IpResult{IPv4: "2001:db8::1"}, wantErr: true},
		{name: "mapped ipv6 in ipv4 field", result: &IpResult{IPv4: "::ffff:192.0.2.10"}, wantErr: true},
		{name: "ipv4 in ipv6 field", result: &IpResult{IPv6: "192.0.2.10"}, wantErr: true},
		{name: "mapped ipv6 in ipv6 field", result: &IpResult{IPv6: "::ffff:192.0.2.10"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.result.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if tt.wantErr {
				return
			}
			if tt.result.IPv4 != tt.wantIPv4 || tt.result.IPv6 != tt.wantIPv6 {
				t.Fatalf("expected IPv4=%q IPv6=%q, got IPv4=%q IPv6=%q", tt.wantIPv4, tt.wantIPv6, tt.result.IPv4, tt.result.IPv6)
			}
		})
	}
}

func TestIPFamilyValidationRejectsMappedIPv6(t *testing.T) {
	mapped := "::ffff:192.0.2.10"
	if IsValidIPv4(mapped) {
		t.Fatalf("expected IPv4-mapped IPv6 %q to be rejected as IPv4", mapped)
	}
	if IsValidIPv6(mapped) {
		t.Fatalf("expected IPv4-mapped IPv6 %q to be rejected as IPv6", mapped)
	}
}
