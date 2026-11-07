package main

import (
	"context"
	"crypto/tls"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/transport"
)

func TestHostLocatorFindsPairedHostAtNewAddress(t *testing.T) {
	hostCert, err := transport.GenerateSelfSignedCert("host")
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := transport.GenerateSelfSignedCert("client")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", transport.NewServerTLSConfig(hostCert, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() { _ = conn.(*tls.Conn).Handshake(); _ = conn.Close() }()
		}
	}()
	hostAddr := listener.Addr().(*net.TCPAddr)

	// The last known address is a different 127.0.0.x address on the same port:
	// the host "moved" within the subnet.
	lastKnown := net.JoinHostPort("127.0.0.9", strconv.Itoa(hostAddr.Port))
	if hostAddr.IP.String() != "127.0.0.1" {
		t.Skip("loopback listener not on 127.0.0.1")
	}

	var searching atomic.Bool
	var clientMu sync.Mutex
	type connection struct {
		address       string
		searchingWhen bool
	}
	connected := make(chan connection, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runHostLocator(hostLocatorDeps{
		ctx:          ctx,
		clientMu:     &clientMu,
		activeClient: func() *transport.Client { return nil },
		activeTarget: func() string { return lastKnown },
		fingerprint:  func() string { return hostCert.Fingerprint },
		certPair:     clientCert,
		searching:    &searching,
		connect: func(address string) {
			connected <- connection{address: address, searchingWhen: searching.Load()}
		},
		probeInterval:   10 * time.Millisecond,
		probeTimeout:    time.Second,
		probeConcurrent: 16,
	})

	select {
	case got := <-connected:
		if got.address != hostAddr.String() {
			t.Fatalf("connected to %s, want %s", got.address, hostAddr.String())
		}
		// Checked inside connect, so a later scan cannot change the answer.
		if got.searchingWhen {
			t.Fatal("searching flag must be cleared before connecting")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("locator did not reconnect to the moved host")
	}
}

func TestSplitHostPortAcceptsBareIPAndRejectsIPv6(t *testing.T) {
	ip, port, err := splitHostPort("192.168.1.249")
	if err != nil || ip.String() != "192.168.1.249" || port != DefaultPort {
		t.Fatalf("bare IP parse = %v %d %v", ip, port, err)
	}
	if _, _, err := splitHostPort("[::1]:54823"); err == nil {
		t.Fatal("IPv6 must be rejected by the locator")
	}
}
