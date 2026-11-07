package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/transport"
)

// hostLocatorInterval is how often the client checks whether the paired host is
// reachable. A search starts on the first failed check and then at most once
// every hostLocatorSearchEvery checks, so an offline host does not flood the LAN.
var hostLocatorInterval = 10 * time.Second

const hostLocatorSearchEvery = 3

type hostLocatorDeps struct {
	ctx             context.Context
	clientMu        *sync.Mutex
	activeClient    func() *transport.Client
	activeTarget    func() string
	fingerprint     func() string
	certPair        *transport.CertificatePair
	searching       *atomic.Bool
	connect         func(address string)
	probeInterval   time.Duration
	probeTimeout    time.Duration
	probeConcurrent int
}

// runHostLocator keeps the client attached to the paired host when its IP changes.
// It searches only while a pinned fingerprint exists, and it connects only to an
// address that presents that exact certificate.
func runHostLocator(deps hostLocatorDeps) {
	ticker := time.NewTicker(deps.probeInterval)
	defer ticker.Stop()
	failures := 0
	for {
		select {
		case <-deps.ctx.Done():
			return
		case <-ticker.C:
		}
		if client := deps.activeClient(); client != nil && client.IsConnected() {
			failures = 0
			continue
		}
		fingerprint := deps.fingerprint()
		last := deps.activeTarget()
		if fingerprint == "" || last == "" {
			continue
		}
		failures++
		if (failures-1)%hostLocatorSearchEvery != 0 {
			continue
		}
		host, port, err := splitHostPort(last)
		if err != nil {
			slog.Warn("Cannot search for the paired host: last address is invalid", "address", last, "error", err)
			continue
		}
		deps.searching.Store(true)
		found, err := transport.FindHostByFingerprint(deps.ctx,
			transport.SubnetCandidates(host, port), deps.certPair, fingerprint,
			deps.probeTimeout, deps.probeConcurrent)
		deps.searching.Store(false)
		if err != nil {
			slog.Debug("Paired host not found on the LAN yet", "last_address", last, "error", err)
			continue
		}
		slog.Info("Paired host found at a new address", "previous", last, "address", found)
		failures = 0
		deps.connect(found)
	}
}

// splitHostPort parses an IPv4 host:port (or a bare IPv4 address, using the default port).
func splitHostPort(address string) (net.IP, int, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		host, portText = address, strconv.Itoa(DefaultPort)
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		return nil, 0, fmt.Errorf("địa chỉ không phải IPv4: %q", address)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 {
		return nil, 0, fmt.Errorf("cổng không hợp lệ: %q", address)
	}
	return ip, port, nil
}
