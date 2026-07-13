package netif

import (
	"net"
	"testing"
)

func TestSelectPublishableIPs(t *testing.T) {
	addrs := []net.Addr{
		ipNet("127.0.0.1/8"),
		ipNet("169.254.10.20/16"),
		ipNet("0.0.0.0/32"),
		ipNet("224.0.0.1/32"),
		ipNet("192.168.1.20/24"),
		ipNet("10.0.0.20/8"),
		ipNet("198.51.100.20/24"),
		ipNet("::/128"),
		ipNet("::1/128"),
		ipNet("fe80::20/64"),
		ipNet("ff02::1/128"),
		ipNet("fd00::20/64"),
		ipNet("2001:db8::20/64"),
	}

	result := selectPublishableIPs(addrs)
	if result.IPv4 != "198.51.100.20" {
		t.Fatalf("IPv4 = %q, want %q", result.IPv4, "198.51.100.20")
	}
	if result.IPv6 != "2001:db8::20" {
		t.Fatalf("IPv6 = %q, want %q", result.IPv6, "2001:db8::20")
	}
}

func TestSelectPublishableIPsIsIndependentOfAddressOrder(t *testing.T) {
	forward := []net.Addr{
		ipNet("192.168.1.20/24"),
		ipNet("10.0.0.20/8"),
		ipNet("198.51.100.20/24"),
		ipNet("fd00::20/64"),
		ipNet("2001:db8::20/64"),
	}
	reverse := []net.Addr{forward[4], forward[3], forward[2], forward[1], forward[0]}

	first := selectPublishableIPs(forward)
	second := selectPublishableIPs(reverse)
	if *first != *second {
		t.Fatalf("selection changed with address order: first = %+v, second = %+v", first, second)
	}
}

func TestSelectPublishableIPsFallsBackToPrivateAddresses(t *testing.T) {
	result := selectPublishableIPs([]net.Addr{
		ipNet("192.168.1.20/24"),
		ipNet("10.0.0.20/8"),
		ipNet("fd00::20/64"),
		ipNet("fd00::10/64"),
	})
	if result.IPv4 != "10.0.0.20" {
		t.Fatalf("IPv4 = %q, want %q", result.IPv4, "10.0.0.20")
	}
	if result.IPv6 != "fd00::10" {
		t.Fatalf("IPv6 = %q, want %q", result.IPv6, "fd00::10")
	}
}

func TestSelectPublishableIPsReturnsEmptyWithoutGlobalUnicast(t *testing.T) {
	result := selectPublishableIPs([]net.Addr{
		ipNet("127.0.0.1/8"),
		ipNet("169.254.10.20/16"),
		ipNet("::1/128"),
		ipNet("fe80::20/64"),
	})
	if result.IPv4 != "" || result.IPv6 != "" {
		t.Fatalf("result = %+v, want no addresses", result)
	}
}

func ipNet(cidr string) *net.IPNet {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	network.IP = ip
	return network
}
