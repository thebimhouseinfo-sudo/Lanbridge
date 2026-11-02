package transport

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/grandcat/zeroconf"
)

const serviceType = "_lanbridge._tcp"

// AdvertiseHost publishes the LAN host listener over mDNS.
func AdvertiseHost(port int) (*zeroconf.Server, error) {
	if port <= 0 {
		return nil, fmt.Errorf("invalid mDNS service port %d", port)
	}
	return zeroconf.Register("LAN Test Bridge", serviceType, "local.", port, nil, nil)
}

// BrowseHosts reports discovered bridge hosts until the context is cancelled.
func BrowseHosts(ctx context.Context, onHost func(address, hostname string)) error {
	if onHost == nil {
		return fmt.Errorf("mDNS host callback cannot be nil")
	}
	resolver, err := zeroconf.NewResolver()
	if err != nil {
		return fmt.Errorf("create mDNS resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	browseErr := make(chan error, 1)
	go func() {
		browseErr <- resolver.Browse(ctx, serviceType, "local.", entries)
	}()

	for entry := range entries {
		address := serviceAddress(entry)
		if address != "" {
			onHost(address, entry.HostName)
		}
	}
	if err := <-browseErr; err != nil && ctx.Err() == nil {
		return fmt.Errorf("browse mDNS hosts: %w", err)
	}
	return nil
}

func serviceAddress(entry *zeroconf.ServiceEntry) string {
	if entry == nil || entry.Port <= 0 {
		return ""
	}
	var ip net.IP
	if len(entry.AddrIPv4) > 0 {
		ip = entry.AddrIPv4[0]
	} else if len(entry.AddrIPv6) > 0 {
		ip = entry.AddrIPv6[0]
	}
	if ip == nil {
		return ""
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(entry.Port))
}
