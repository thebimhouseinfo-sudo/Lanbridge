package transport

import (
	"bytes"
	"crypto/tls"
	"testing"
)

func TestCertGenerationAndFingerprint(t *testing.T) {
	pair, err := GenerateSelfSignedCert("test-bridge-host")
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert failed: %v", err)
	}

	if len(pair.Fingerprint) != 64 {
		t.Errorf("Expected 64-char hex SHA-256 fingerprint, got %d chars: %s", len(pair.Fingerprint), pair.Fingerprint)
	}

	if len(pair.TLSKeyPair.Certificate) == 0 {
		t.Fatalf("No certificate in TLS key pair")
	}

	// Verify deterministic fingerprint from DER
	recalc := Fingerprint(pair.RawCertDER)
	if recalc != pair.Fingerprint {
		t.Errorf("Fingerprint mismatch: got %s, expected %s", recalc, pair.Fingerprint)
	}
}

func TestLoadCertificatePairPreservesIdentity(t *testing.T) {
	generated, err := GenerateSelfSignedCert("persisted-identity")
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadCertificatePair(generated.CertPEM, generated.KeyPEM)
	if err != nil {
		t.Fatalf("LoadCertificatePair failed: %v", err)
	}
	if loaded.Fingerprint != generated.Fingerprint {
		t.Fatalf("fingerprint changed after restore: got %s, want %s", loaded.Fingerprint, generated.Fingerprint)
	}
	if !bytes.Equal(loaded.RawCertDER, generated.RawCertDER) {
		t.Fatal("certificate DER changed after restore")
	}
}

func TestVerifyConnectionWithPinnedFingerprint(t *testing.T) {
	hostCert, err := GenerateSelfSignedCert("test-host")
	if err != nil {
		t.Fatalf("Failed to generate host cert: %v", err)
	}

	clientCert, err := GenerateSelfSignedCert("test-client")
	if err != nil {
		t.Fatalf("Failed to generate client cert: %v", err)
	}

	// Server expecting correct client fingerprint
	srvConfig := NewServerTLSConfig(hostCert, clientCert.Fingerprint)

	cs := tls.ConnectionState{}
	// Verify without cert should error
	if err := srvConfig.VerifyConnection(cs); err == nil {
		t.Errorf("Expected error when no peer certificate is presented")
	}
}
