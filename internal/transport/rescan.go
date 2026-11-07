package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"
)

// ErrHostNotFound means no candidate address presented the pinned host certificate.
var ErrHostNotFound = errors.New("không tìm thấy Windows host trong mạng LAN (đã thử các địa chỉ trong cùng dải IP)")

// SubnetCandidates lists every host address in the /24 that contains ip,
// excluding the network and broadcast addresses. The address ip itself is first
// so a host that did not move is found immediately.
func SubnetCandidates(ip net.IP, port int) []string {
	v4 := ip.To4()
	if v4 == nil {
		return nil
	}
	portText := strconv.Itoa(port)
	candidates := []string{net.JoinHostPort(v4.String(), portText)}
	for last := 1; last < 255; last++ {
		candidate := net.IPv4(v4[0], v4[1], v4[2], byte(last))
		if candidate.Equal(v4) {
			continue
		}
		candidates = append(candidates, net.JoinHostPort(candidate.String(), portText))
	}
	return candidates
}

// FindHostByFingerprint probes candidate addresses in parallel with a TLS
// handshake pinned to expectedFingerprint. The first address that presents that
// certificate wins. Addresses that do not answer, or answer with another
// certificate, are ignored. The probe never sends bridge data.
func FindHostByFingerprint(ctx context.Context, candidates []string, certPair *CertificatePair, expectedFingerprint string, perProbe time.Duration, concurrency int) (string, error) {
	if expectedFingerprint == "" {
		return "", errors.New("pinned host fingerprint is required to verify a rediscovered host")
	}
	if len(candidates) == 0 {
		return "", ErrHostNotFound
	}
	if concurrency < 1 {
		concurrency = 1
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	found := make(chan string, 1)
	work := make(chan string)
	var wg sync.WaitGroup
	tlsConfig := NewClientTLSConfig(certPair, expectedFingerprint)
	dialer := &net.Dialer{Timeout: perProbe}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for address := range work {
				if ctx.Err() != nil {
					return
				}
				if probeHost(ctx, dialer, address, tlsConfig) {
					select {
					case found <- address:
					default:
					}
					cancel()
					return
				}
			}
		}()
	}

	go func() {
		defer close(work)
		for _, candidate := range candidates {
			select {
			case work <- candidate:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(found)
	}()

	address, ok := <-found
	if !ok || address == "" {
		return "", ErrHostNotFound
	}
	return address, nil
}

func probeHost(ctx context.Context, dialer *net.Dialer, address string, tlsConfig *tls.Config) bool {
	conn, err := tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
