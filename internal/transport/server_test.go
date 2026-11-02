package transport

import (
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/protocol"
)

func getFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func TestServerClientIntegration(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}

	hostCert, err := GenerateSelfSignedCert("test-host")
	if err != nil {
		t.Fatalf("Failed to generate host cert: %v", err)
	}

	clientCert, err := GenerateSelfSignedCert("test-client")
	if err != nil {
		t.Fatalf("Failed to generate client cert: %v", err)
	}

	// Server config with pinned client fingerprint
	srvTLS := NewServerTLSConfig(hostCert, clientCert.Fingerprint)
	server := NewServer(port, srvTLS)

	// Register an echo handler
	server.RegisterHandler("echo", func(msg *protocol.Message) (*protocol.Message, error) {
		var content string
		_ = json.Unmarshal(msg.Payload, &content)
		return protocol.NewResponse(msg.ID, "echo_resp", "echoed: "+content)
	})
	startedSlow := make(chan struct{})
	server.RegisterHandler("slow", func(msg *protocol.Message) (*protocol.Message, error) {
		close(startedSlow)
		time.Sleep(300 * time.Millisecond)
		return protocol.NewResponse(msg.ID, "slow_resp", nil)
	})
	server.RegisterHandler("fast", func(msg *protocol.Message) (*protocol.Message, error) {
		return protocol.NewResponse(msg.ID, "fast_resp", nil)
	})

	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Close()

	// Client config with pinned host fingerprint
	clientTLS := NewClientTLSConfig(clientCert, hostCert.Fingerprint)
	client := NewClient(fmt.Sprintf("127.0.0.1:%d", port), clientTLS)

	if err := client.Connect(); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}
	defer client.Close()

	// Perform RoundTrip
	payloadBytes, _ := json.Marshal("hello bridge")
	req := &protocol.Message{
		ID:      "req-123",
		Type:    "echo",
		Payload: payloadBytes,
	}

	resp, err := client.RoundTrip(req, 2*time.Second)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}

	if resp.ID != "req-123" {
		t.Errorf("Expected resp ID req-123, got %s", resp.ID)
	}

	var respContent string
	if err := json.Unmarshal(resp.Payload, &respContent); err != nil {
		t.Fatalf("Failed to unmarshal response payload: %v", err)
	}
	if respContent != "echoed: hello bridge" {
		t.Errorf("Expected 'echoed: hello bridge', got '%s'", respContent)
	}

	slowDone := make(chan error, 1)
	go func() {
		_, err := client.RoundTrip(&protocol.Message{ID: "req-slow", Type: "slow"}, time.Second)
		slowDone <- err
	}()
	<-startedSlow
	fastStart := time.Now()
	if _, err := client.RoundTrip(&protocol.Message{ID: "req-fast", Type: "fast"}, time.Second); err != nil {
		t.Fatalf("Fast request failed while another request was pending: %v", err)
	}
	if elapsed := time.Since(fastStart); elapsed >= 250*time.Millisecond {
		t.Errorf("Fast request was blocked behind slow handler: %s", elapsed)
	}
	if err := <-slowDone; err != nil {
		t.Fatalf("Slow request failed: %v", err)
	}
}

func TestServerAttachesPeerFingerprintFromTLS(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatal(err)
	}
	hostCert, err := GenerateSelfSignedCert("fingerprint-host")
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := GenerateSelfSignedCert("fingerprint-client")
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(port, NewServerTLSConfig(hostCert, clientCert.Fingerprint))
	server.RegisterHandler("peer-fingerprint", func(msg *protocol.Message) (*protocol.Message, error) {
		return protocol.NewResponse(msg.ID, "peer-fingerprint", msg.PeerFingerprint)
	})
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	client := NewClient(
		fmt.Sprintf("127.0.0.1:%d", port),
		NewClientTLSConfig(clientCert, hostCert.Fingerprint),
	)
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	response, err := client.RoundTrip(&protocol.Message{ID: "peer-fp", Type: "peer-fingerprint"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var fingerprint string
	if err := json.Unmarshal(response.Payload, &fingerprint); err != nil {
		t.Fatal(err)
	}
	if fingerprint != clientCert.Fingerprint {
		t.Fatalf("handler received peer fingerprint %q, want %q", fingerprint, clientCert.Fingerprint)
	}

	encoded, err := json.Marshal(&protocol.Message{Type: "test", PeerFingerprint: "must-not-leak"})
	if err != nil {
		t.Fatal(err)
	}
	var wireMessage map[string]any
	if err := json.Unmarshal(encoded, &wireMessage); err != nil {
		t.Fatal(err)
	}
	if _, exists := wireMessage["PeerFingerprint"]; exists {
		t.Fatal("TLS peer fingerprint was serialized into the protocol message")
	}
}

func TestServerRequestGuardAllowsOnlyPairingBeforeTrust(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatal(err)
	}
	hostCert, err := GenerateSelfSignedCert("guard-host")
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := GenerateSelfSignedCert("guard-client")
	if err != nil {
		t.Fatal(err)
	}

	var paired atomic.Bool
	server := NewServer(port, NewServerTLSConfig(hostCert, clientCert.Fingerprint))
	server.SetRequestGuard(func(msg *protocol.Message) *protocol.Message {
		if !paired.Load() && msg.Type != protocol.TypePairInit && msg.Type != protocol.TypePairVerify {
			return protocol.NewErrorResponse(msg.ID, "ERR_UNPAIRED", "pair before using the bridge")
		}
		return nil
	})
	server.RegisterHandler(protocol.TypePairInit, func(msg *protocol.Message) (*protocol.Message, error) {
		return protocol.NewResponse(msg.ID, protocol.TypePairInit, map[string]string{"status": "ready"})
	})
	server.RegisterHandler("echo", func(msg *protocol.Message) (*protocol.Message, error) {
		return protocol.NewResponse(msg.ID, "echo_resp", nil)
	})
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	client := NewClient(fmt.Sprintf("127.0.0.1:%d", port), NewClientTLSConfig(clientCert, hostCert.Fingerprint))
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	blocked, err := client.RoundTrip(&protocol.Message{ID: "blocked", Type: "echo"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Error == nil || blocked.Error.Code != "ERR_UNPAIRED" {
		t.Fatalf("expected unpaired guard error, got %+v", blocked)
	}

	allowed, err := client.RoundTrip(&protocol.Message{ID: "pair", Type: protocol.TypePairInit}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if allowed.Type != protocol.TypePairInit {
		t.Fatalf("pairing request was not allowed: %+v", allowed)
	}

	paired.Store(true)
	connected, err := client.RoundTrip(&protocol.Message{ID: "connected", Type: "echo"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if connected.Type != "echo_resp" {
		t.Fatalf("paired request was not allowed: %+v", connected)
	}
}

func TestServerTLSConfigUsesUpdatedPeerFingerprint(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatal(err)
	}
	hostCert, err := GenerateSelfSignedCert("dynamic-host")
	if err != nil {
		t.Fatal(err)
	}
	trustedClientCert, err := GenerateSelfSignedCert("trusted-client")
	if err != nil {
		t.Fatal(err)
	}
	untrustedClientCert, err := GenerateSelfSignedCert("untrusted-client")
	if err != nil {
		t.Fatal(err)
	}

	var expected atomic.Value
	var appConnections atomic.Int32
	var acceptedPeer atomic.Value
	expected.Store("")
	server := NewServer(port, NewServerTLSConfigWithFingerprint(hostCert, func() string {
		return expected.Load().(string)
	}))
	server.SetCallbacks(func(peerFingerprint string) {
		acceptedPeer.Store(peerFingerprint)
		appConnections.Add(1)
	}, nil, nil)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	expected.Store(trustedClientCert.Fingerprint)

	untrustedClient := NewClient(
		fmt.Sprintf("127.0.0.1:%d", port),
		NewClientTLSConfig(untrustedClientCert, hostCert.Fingerprint),
	)
	defer untrustedClient.Close()
	if err := untrustedClient.Connect(); err != nil {
		t.Logf("untrusted TLS client was rejected during handshake: %v", err)
	}
	trustedClient := NewClient(
		fmt.Sprintf("127.0.0.1:%d", port),
		NewClientTLSConfig(trustedClientCert, hostCert.Fingerprint),
	)
	if err := trustedClient.Connect(); err != nil {
		t.Fatalf("trusted client failed to connect: %v", err)
	}
	defer trustedClient.Close()

	waitForAcceptedConnection := func() bool {
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if appConnections.Load() > 0 {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}
	if !waitForAcceptedConnection() {
		t.Fatal("server did not establish an application connection for the pinned client")
	}
	if got := acceptedPeer.Load().(string); got != trustedClientCert.Fingerprint {
		t.Fatalf("server accepted fingerprint %q, want pinned fingerprint %q", got, trustedClientCert.Fingerprint)
	}
}
