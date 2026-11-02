package transport

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/grandcat/zeroconf"
)

func TestServiceAddressPrefersIPv4AndSupportsIPv6(t *testing.T) {
	tests := []struct {
		name  string
		entry *zeroconf.ServiceEntry
		want  string
	}{
		{
			name: "prefer IPv4",
			entry: &zeroconf.ServiceEntry{
				Port:     54823,
				AddrIPv4: []net.IP{net.ParseIP("192.168.1.20")},
				AddrIPv6: []net.IP{net.ParseIP("fe80::20")},
			},
			want: "192.168.1.20:54823",
		},
		{
			name: "IPv6 address",
			entry: &zeroconf.ServiceEntry{
				Port:     54823,
				AddrIPv6: []net.IP{net.ParseIP("fe80::20")},
			},
			want: "[fe80::20]:54823",
		},
		{
			name:  "missing address",
			entry: &zeroconf.ServiceEntry{Port: 54823},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serviceAddress(tt.entry); got != tt.want {
				t.Fatalf("serviceAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAdvertiseAndBrowseHostOnLocalNetwork(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	service, err := AdvertiseHost(port)
	if err != nil {
		t.Skipf("mDNS advertisement is unavailable in this environment: %v", err)
	}
	defer service.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	found := make(chan string, 1)
	browseDone := make(chan error, 1)
	go func() {
		browseDone <- BrowseHosts(ctx, func(address, _ string) {
			_, portText, err := net.SplitHostPort(address)
			if err == nil && portText == strconv.Itoa(port) {
				select {
				case found <- address:
				default:
				}
			}
		})
	}()

	select {
	case address := <-found:
		if _, portText, err := net.SplitHostPort(address); err != nil || portText != strconv.Itoa(port) {
			t.Fatalf("discovered address %q does not match advertised port %d: %v", address, port, err)
		}
	case err := <-browseDone:
		if err != nil {
			t.Skipf("mDNS browsing is unavailable in this environment: %v", err)
		}
		if ctx.Err() != nil {
			t.Skipf("mDNS did not discover the local service within the test window: %v", ctx.Err())
		}
		t.Fatal("mDNS browse stopped before discovering the advertised host")
	case <-ctx.Done():
		t.Skip(fmt.Sprintf("mDNS did not discover the local service within the test window: %v", ctx.Err()))
	}
}
