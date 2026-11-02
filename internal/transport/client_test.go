package transport

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/protocol"
)

func TestClientCloseIsIdempotent(t *testing.T) {
	client := NewClient("127.0.0.1:1", nil)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestClientAutoReconnectsAfterConnectionDrop(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatal(err)
	}
	hostCert, err := GenerateSelfSignedCert("reconnect-host")
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := GenerateSelfSignedCert("reconnect-client")
	if err != nil {
		t.Fatal(err)
	}

	server := NewServer(port, NewServerTLSConfig(hostCert, clientCert.Fingerprint))
	server.RegisterHandler("echo", func(msg *protocol.Message) (*protocol.Message, error) {
		return protocol.NewResponse(msg.ID, "echo_resp", nil)
	})
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}

	client := NewClient(
		fmt.Sprintf("127.0.0.1:%d", port),
		NewClientTLSConfig(clientCert, hostCert.Fingerprint),
	)
	var connectCount atomic.Int32
	client.SetCallbacks(func() {
		connectCount.Add(1)
	}, nil, nil)
	if err := client.Connect(); err != nil {
		_ = server.Close()
		t.Fatalf("initial connection failed: %v", err)
	}
	client.StartAutoReconnect()
	defer func() {
		_ = client.Close()
		_ = server.Close()
	}()

	client.connMu.Lock()
	connection := client.rawConn
	client.connMu.Unlock()
	if connection == nil {
		t.Fatal("client has no live connection to interrupt")
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("failed to interrupt client connection: %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if connectCount.Load() >= 2 && client.IsConnected() {
			response, err := client.RoundTrip(&protocol.Message{ID: "reconnected", Type: "echo"}, 200*time.Millisecond)
			if err == nil && response.Type == "echo_resp" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("client did not reconnect and complete a request; connect attempts=%d", connectCount.Load())
}
