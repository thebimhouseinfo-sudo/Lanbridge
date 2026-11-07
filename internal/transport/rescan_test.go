package transport

import (
	"context"
	"crypto/tls"
	"net"
	"strconv"
	"testing"
	"time"
)

func startPinnedHost(t *testing.T, certPair *CertificatePair, addr string) (net.Listener, string) {
	t.Helper()
	listener, err := tls.Listen("tcp", addr, NewServerTLSConfig(certPair, ""))
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				_ = conn.(*tls.Conn).Handshake()
				_ = conn.Close()
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return listener, listener.Addr().String()
}

func TestSubnetCandidatesCoverSlash24(t *testing.T) {
	candidates := SubnetCandidates(net.ParseIP("192.168.1.231"), 54823)
	if candidates[0] != "192.168.1.231:54823" {
		t.Fatalf("first candidate = %s, want the last known address", candidates[0])
	}
	if len(candidates) != 254 {
		t.Fatalf("candidates = %d, want 254 (.1-.254)", len(candidates))
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c] {
			t.Fatalf("duplicate candidate %s", c)
		}
		seen[c] = true
	}
	if seen["192.168.1.0:54823"] || seen["192.168.1.255:54823"] {
		t.Fatal("network and broadcast addresses must be excluded")
	}
	if SubnetCandidates(net.ParseIP("::1"), 1) != nil {
		t.Fatal("IPv6 input must not produce IPv4 candidates")
	}
}

func TestFindHostByFingerprintFindsMovedHost(t *testing.T) {
	hostCert, err := GenerateSelfSignedCert("host")
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := GenerateSelfSignedCert("client")
	if err != nil {
		t.Fatal(err)
	}
	// The host listens on 127.0.0.1 only; 127.0.0.2 stands in for the old address
	// of a host that moved, and must be skipped without matching.
	_, hostAddr := startPinnedHost(t, hostCert, "127.0.0.1:0")
	_, port, _ := net.SplitHostPort(hostAddr)
	portNum, _ := strconv.Atoi(port)

	candidates := []string{
		net.JoinHostPort("127.0.0.2", port),
		hostAddr,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := FindHostByFingerprint(ctx, candidates, clientCert, hostCert.Fingerprint, time.Second, 4)
	if err != nil {
		t.Fatalf("FindHostByFingerprint error = %v", err)
	}
	if got != hostAddr {
		t.Fatalf("found %s, want %s (port %d)", got, hostAddr, portNum)
	}
}

func TestFindHostByFingerprintRejectsWrongCertificate(t *testing.T) {
	realCert, _ := GenerateSelfSignedCert("real")
	impostor, _ := GenerateSelfSignedCert("impostor")
	clientCert, _ := GenerateSelfSignedCert("client")
	_, hostAddr := startPinnedHost(t, impostor, "127.0.0.1:0")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := FindHostByFingerprint(ctx, []string{hostAddr}, clientCert, realCert.Fingerprint, time.Second, 2)
	if err != ErrHostNotFound {
		t.Fatalf("error = %v, want ErrHostNotFound for a different certificate", err)
	}
}

func TestFindHostByFingerprintRequiresPin(t *testing.T) {
	clientCert, _ := GenerateSelfSignedCert("client")
	if _, err := FindHostByFingerprint(context.Background(), []string{"127.0.0.1:1"}, clientCert, "", time.Second, 1); err == nil {
		t.Fatal("empty pin must be rejected")
	}
}
