package dnsname

import "testing"

func TestSplitRecord(t *testing.T) {
	tests := []struct {
		name       string
		recordName string
		zoneName   string
		zone       string
		rr         string
		wantErr    bool
	}{
		{name: "subdomain", recordName: "DDNS.Example.COM.", zone: "example.com", rr: "ddns"},
		{name: "root", recordName: "example.com", zone: "example.com", rr: "@"},
		{name: "multi-label public suffix", recordName: "www.example.co.uk", zone: "example.co.uk", rr: "www"},
		{name: "multi-label public suffix root", recordName: "example.co.uk", zone: "example.co.uk", rr: "@"},
		{name: "private public suffix", recordName: "www.example.github.io", zone: "example.github.io", rr: "www"},
		{name: "explicit zone", recordName: "ddns.example.co.uk", zoneName: "example.co.uk", zone: "example.co.uk", rr: "ddns"},
		{name: "explicit delegated zone", recordName: "host.dev.example.com", zoneName: "dev.example.com", zone: "dev.example.com", rr: "host"},
		{name: "explicit root", recordName: "example.co.uk", zoneName: "example.co.uk", zone: "example.co.uk", rr: "@"},
		{name: "not under explicit zone", recordName: "ddns.example.com", zoneName: "example.net", wantErr: true},
		{name: "bare public suffix", recordName: "co.uk", wantErr: true},
		{name: "single label", recordName: "localhost", wantErr: true},
		{name: "empty label", recordName: "ddns..example.com", wantErr: true},
		{name: "bad character", recordName: "ddns_1.example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zone, rr, err := SplitRecord(tt.recordName, tt.zoneName)
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if tt.wantErr {
				return
			}
			if zone != tt.zone || rr != tt.rr {
				t.Fatalf("expected %s/%s, got %s/%s", tt.zone, tt.rr, zone, rr)
			}
		})
	}
}
