package transport

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// CertificatePair holds TLS certificate and its SHA-256 fingerprint
type CertificatePair struct {
	TLSKeyPair  tls.Certificate
	Fingerprint string
	RawCertDER  []byte
	CertPEM     string
	KeyPEM      string
}

// GenerateSelfSignedCert creates an ECDSA P-256 private key and self-signed x509 cert valid for 10 years
func GenerateSelfSignedCert(commonName string) (*CertificatePair, error) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ecdsa key: %w", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"LAN Test Bridge"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create x509 certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	privDER, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to build tls certificate: %w", err)
	}

	fp := Fingerprint(certDER)

	return &CertificatePair{
		TLSKeyPair:  tlsCert,
		Fingerprint: fp,
		RawCertDER:  certDER,
		CertPEM:     string(certPEM),
		KeyPEM:      string(keyPEM),
	}, nil
}

// LoadCertificatePair restores a previously persisted certificate identity.
func LoadCertificatePair(certPEM, keyPEM string) (*CertificatePair, error) {
	tlsCert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate identity: %w", err)
	}
	if len(tlsCert.Certificate) == 0 {
		return nil, errors.New("certificate identity contains no certificate")
	}

	rawCertDER := tlsCert.Certificate[0]
	return &CertificatePair{
		TLSKeyPair:  tlsCert,
		Fingerprint: Fingerprint(rawCertDER),
		RawCertDER:  rawCertDER,
		CertPEM:     certPEM,
		KeyPEM:      keyPEM,
	}, nil
}

// Fingerprint calculates the lowercase hex SHA-256 fingerprint of a DER certificate
func Fingerprint(certDER []byte) string {
	sum := sha256.Sum256(certDER)
	return hex.EncodeToString(sum[:])
}

// ExtractPeerFingerprint retrieves the peer's certificate fingerprint directly from TLS connection state
func ExtractPeerFingerprint(cs tls.ConnectionState) (string, error) {
	if len(cs.PeerCertificates) == 0 {
		return "", errors.New("no peer certificates presented")
	}
	peerDER := cs.PeerCertificates[0].Raw
	return Fingerprint(peerDER), nil
}

// NewServerTLSConfig creates TLS config for Host with optional pinned peer fingerprint
func NewServerTLSConfig(certPair *CertificatePair, expectedClientFingerprint string) *tls.Config {
	return NewServerTLSConfigWithFingerprint(certPair, func() string {
		return expectedClientFingerprint
	})
}

// NewServerTLSConfigWithFingerprint reads the expected peer identity at each handshake.
func NewServerTLSConfigWithFingerprint(certPair *CertificatePair, expectedClientFingerprint func() string) *tls.Config {
	if expectedClientFingerprint == nil {
		expectedClientFingerprint = func() string { return "" }
	}
	return &tls.Config{
		Certificates: []tls.Certificate{certPair.TLSKeyPair},
		ClientAuth:   tls.RequireAnyClientCert,
		MinVersion:   tls.VersionTLS13,
		VerifyConnection: func(cs tls.ConnectionState) error {
			expectedFingerprint := expectedClientFingerprint()
			if expectedFingerprint == "" {
				// During initial pairing, accept any cert; CPace will verify
				return nil
			}
			peerFP, err := ExtractPeerFingerprint(cs)
			if err != nil {
				return err
			}
			if peerFP != expectedFingerprint {
				return fmt.Errorf("untrusted peer certificate fingerprint: %s (expected %s)", peerFP, expectedFingerprint)
			}
			return nil
		},
	}
}

// NewClientTLSConfig creates TLS config for Client with optional pinned host fingerprint
func NewClientTLSConfig(certPair *CertificatePair, expectedHostFingerprint string) *tls.Config {
	return &tls.Config{
		Certificates:       []tls.Certificate{certPair.TLSKeyPair},
		InsecureSkipVerify: true, // We verify certificate fingerprint directly in VerifyConnection
		MinVersion:         tls.VersionTLS13,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if expectedHostFingerprint == "" {
				// During initial pairing, accept any cert; CPace will verify
				return nil
			}
			peerFP, err := ExtractPeerFingerprint(cs)
			if err != nil {
				return err
			}
			if peerFP != expectedHostFingerprint {
				return fmt.Errorf("untrusted host certificate fingerprint: %s (expected %s)", peerFP, expectedHostFingerprint)
			}
			return nil
		},
	}
}
