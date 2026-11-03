package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/agentlock"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/fileops"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/permissions"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/protocol"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/transport"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/transport/pairing"
)

func TestVersionAndDefaults(t *testing.T) {
	if Version != "1.0.0" {
		t.Errorf("Expected Version 1.0.0, got %s", Version)
	}
	if DefaultPort != 54823 {
		t.Errorf("Expected DefaultPort 54823, got %d", DefaultPort)
	}
}

func TestDefaultCommandLaunchesClientFromMacApp(t *testing.T) {
	if got := defaultCommand("darwin"); got != "client" {
		t.Fatalf("default command on macOS = %q, want client", got)
	}
	if got := defaultCommand("windows"); got != "" {
		t.Fatalf("default command on Windows = %q, want no default", got)
	}
}

func TestRunMCPWithBusyLockDoesNotWriteProtocolOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runMCPWithLock(
		nil,
		func() (*agentlock.Lock, error) {
			return nil, agentlock.ErrBusy
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if !errors.Is(err, agentlock.ErrBusy) {
		t.Fatalf("runMCPWithLock error = %v, want ErrBusy", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("busy MCP process wrote to stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "already running") {
		t.Fatalf("busy MCP error was not reported on stderr: %q", stderr.String())
	}
}

func TestNormalizeHostAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
		wantErr bool
	}{
		{name: "IPv4 with port", address: "192.168.1.20:54823", want: "192.168.1.20:54823"},
		{name: "IPv4 default port", address: "192.168.1.20", want: "192.168.1.20:54823"},
		{name: "IPv6 with port", address: "[fd00::20]:54823", want: "[fd00::20]:54823"},
		{name: "IPv6 bracketed default port", address: "[fd00::20]", want: "[fd00::20]:54823"},
		{name: "IPv6 default port", address: "fd00::20", want: "[fd00::20]:54823"},
		{name: "hostname", address: "bridge.local:54823", wantErr: true},
		{name: "invalid port", address: "192.168.1.20:70000", wantErr: true},
		{name: "zero port", address: "192.168.1.20:0", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeHostAddress(test.address)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeHostAddress(%q) error = %v, wantErr %v", test.address, err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("normalizeHostAddress(%q) = %q, want %q", test.address, got, test.want)
			}
		})
	}
}

func TestFormatHostAddresses(t *testing.T) {
	got := formatHostAddresses([]net.IP{
		net.ParseIP("192.168.1.20"),
		net.ParseIP("10.0.0.5"),
		net.ParseIP("fd00::20"),
		net.ParseIP("192.168.1.20"),
		net.ParseIP("8.8.8.8"),
		nil,
	}, 54823)
	want := "10.0.0.5:54823, 192.168.1.20:54823, [fd00::20]:54823"
	if got != want {
		t.Fatalf("formatHostAddresses() = %q, want %q", got, want)
	}

	if got := formatHostAddresses(nil, 54823); got != "Chưa tìm thấy IP LAN (cổng 54823)" {
		t.Fatalf("empty host addresses = %q", got)
	}
}

func TestLoadOrCreateIdentityPersistsCertificateAndKey(t *testing.T) {
	stateStore, err := store.NewStateStoreAt(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := stateStore.Load()
	if err != nil {
		t.Fatal(err)
	}

	first, err := loadOrCreateIdentity(stateStore, state, "identity-test")
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := stateStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadOrCreateIdentity(stateStore, reloaded, "identity-test")
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint != second.Fingerprint || first.KeyPEM != second.KeyPEM {
		t.Fatal("identity changed after loading persisted state")
	}
}

func TestPersistLastConnectedAddressPreservesState(t *testing.T) {
	stateStore, err := store.NewStateStoreAt(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	state := &store.AppState{
		CertPEM:               "certificate",
		KeyPEM:                "private-key",
		MyFingerprint:         "local-fingerprint",
		PairedPeerFingerprint: "peer-fingerprint",
		Permissions: []store.PermissionGrant{{
			Path:         "C:\\Projects\\CAI",
			Capabilities: []string{"read"},
		}},
	}
	if err := stateStore.Save(state); err != nil {
		t.Fatal(err)
	}
	if err := persistLastConnectedAddress(stateStore, "192.168.1.20:54823"); err != nil {
		t.Fatal(err)
	}
	loaded, err := stateStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LastConnectedAddr != "192.168.1.20:54823" ||
		loaded.CertPEM != state.CertPEM ||
		loaded.KeyPEM != state.KeyPEM ||
		loaded.MyFingerprint != state.MyFingerprint ||
		loaded.PairedPeerFingerprint != state.PairedPeerFingerprint ||
		len(loaded.Permissions) != 1 || loaded.Permissions[0].Path != state.Permissions[0].Path {
		t.Fatalf("persisted address did not preserve existing state: %+v", loaded)
	}
}

func TestReadProtocolHandlers(t *testing.T) {
	baseDir := t.TempDir()
	stateStore, err := store.NewStateStoreAt(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	gate, err := permissions.NewGatekeeper(stateStore, &store.HostConfig{
		Projects: []store.ProjectConfig{{Name: "test", Root: baseDir}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.AddGrant(baseDir, []string{permissions.CapRead}); err != nil {
		t.Fatal(err)
	}

	filePath := filepath.Join(baseDir, "sample.log")
	if err := os.WriteFile(filePath, []byte("first\nsecond\nthird\n"), 0600); err != nil {
		t.Fatal(err)
	}

	listPayload, _ := json.Marshal(map[string]string{"path": baseDir})
	listResponse, err := fileListHandler(gate)(&protocol.Message{ID: "list-1", Payload: listPayload})
	if err != nil {
		t.Fatalf("file list handler failed: %v", err)
	}
	if listResponse.Type != protocol.TypeFileList || listResponse.Error != nil {
		t.Fatalf("unexpected file list response: %+v", listResponse)
	}
	var listResult struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(listResponse.Payload, &listResult); err != nil {
		t.Fatal(err)
	}
	if len(listResult.Items) != 1 || listResult.Items[0]["name"] != "sample.log" {
		t.Fatalf("unexpected list result: %+v", listResult.Items)
	}

	infoPayload, _ := json.Marshal(map[string]string{"path": filePath})
	infoResponse, err := fileInfoHandler(gate)(&protocol.Message{ID: "info-1", Payload: infoPayload})
	if err != nil {
		t.Fatalf("file info handler failed: %v", err)
	}
	if infoResponse.Type != protocol.TypeFileInfo || infoResponse.Error != nil {
		t.Fatalf("unexpected file info response: %+v", infoResponse)
	}

	readPayload, _ := json.Marshal(map[string]any{"path": filePath, "offset": 6, "length": 6})
	readResponse, err := fileReadHandler(gate)(&protocol.Message{ID: "read-1", Payload: readPayload})
	if err != nil {
		t.Fatalf("file read handler failed: %v", err)
	}
	var readResult struct {
		Data []byte `json:"data"`
		EOF  bool   `json:"eof"`
	}
	if err := json.Unmarshal(readResponse.Payload, &readResult); err != nil {
		t.Fatal(err)
	}
	if readResponse.Type != protocol.TypeFileRead || string(readResult.Data) != "second" || readResult.EOF {
		t.Fatalf("unexpected read response: %+v data=%q", readResponse, readResult.Data)
	}

	tailPayload, _ := json.Marshal(map[string]any{"path": filePath, "tail_lines": 2})
	tailResponse, err := fileReadHandler(gate)(&protocol.Message{ID: "tail-1", Payload: tailPayload})
	if err != nil {
		t.Fatalf("tail handler failed: %v", err)
	}
	var tailResult struct {
		Lines []string `json:"lines"`
		EOF   bool     `json:"eof"`
	}
	if err := json.Unmarshal(tailResponse.Payload, &tailResult); err != nil {
		t.Fatal(err)
	}
	if tailResponse.Type != protocol.TypeFileRead || len(tailResult.Lines) != 2 ||
		tailResult.Lines[0] != "second" || tailResult.Lines[1] != "third" || !tailResult.EOF {
		t.Fatalf("unexpected tail response: %+v", tailResult)
	}

	outsidePath := filepath.Join(t.TempDir(), "outside.log")
	deniedPayload, _ := json.Marshal(map[string]string{"path": outsidePath})
	deniedResponse, err := fileInfoHandler(gate)(&protocol.Message{ID: "denied-1", Payload: deniedPayload})
	if err != nil {
		t.Fatalf("file info denial handler failed: %v", err)
	}
	if deniedResponse.Error == nil || deniedResponse.Error.Code != "ERR_PERMISSION" {
		t.Fatalf("expected permission error response, got %+v", deniedResponse)
	}
}

func TestSyncProtocolHandler(t *testing.T) {
	rootDir := t.TempDir()
	relatedDir := t.TempDir()
	ungrantedDir := t.TempDir()
	cfg := &store.HostConfig{
		MaxSyncMB: 1,
		Projects: []store.ProjectConfig{
			{Name: "test", Root: rootDir, RelatedDirs: map[string]string{"addin": relatedDir}},
			{Name: "ungranted", Root: ungrantedDir},
		},
	}
	stateStore, err := store.NewStateStoreAt(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	gate, err := permissions.NewGatekeeper(stateStore, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.AddGrant(rootDir, []string{permissions.CapSync}); err != nil {
		t.Fatal(err)
	}
	if err := gate.AddGrant(relatedDir, []string{permissions.CapSync}); err != nil {
		t.Fatal(err)
	}

	handler := syncFilesHandler(gate, cfg)
	content := []byte("sync payload")
	payload, err := json.Marshal(map[string]any{
		"project": "test",
		"target":  "root",
		"files":   []fileops.SyncItem{{RelPath: "src/Plugin.cs", Data: content}},
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := handler(&protocol.Message{ID: "sync-root", Payload: payload})
	if err != nil {
		t.Fatalf("sync handler failed: %v", err)
	}
	var result struct {
		Results []fileops.SyncResult `json:"results"`
	}
	if err := json.Unmarshal(response.Payload, &result); err != nil {
		t.Fatal(err)
	}
	expectedHash := sha256.Sum256(content)
	if response.Type != protocol.TypeFileSync || response.Error != nil || len(result.Results) != 1 ||
		result.Results[0].SHA256 != hex.EncodeToString(expectedHash[:]) {
		t.Fatalf("unexpected root sync response: %+v results=%+v", response, result.Results)
	}

	relatedPayload, err := json.Marshal(map[string]any{
		"project": "test",
		"target":  "addin",
		"files":   []fileops.SyncItem{{RelPath: "CAI.addin", Data: content}},
	})
	if err != nil {
		t.Fatal(err)
	}
	relatedResponse, err := handler(&protocol.Message{ID: "sync-related", Payload: relatedPayload})
	if err != nil {
		t.Fatalf("related-dir sync handler failed: %v", err)
	}
	if relatedResponse.Error != nil {
		t.Fatalf("related-dir sync rejected: %+v", relatedResponse.Error)
	}

	invalidTarget, _ := json.Marshal(map[string]any{
		"project": "test",
		"target":  "unknown",
		"files":   []fileops.SyncItem{{RelPath: "file.txt", Data: content}},
	})
	invalidResponse, err := handler(&protocol.Message{ID: "sync-invalid-target", Payload: invalidTarget})
	if err != nil {
		t.Fatalf("invalid target handler failed: %v", err)
	}
	if invalidResponse.Error == nil || invalidResponse.Error.Code != "ERR_PARAMS" {
		t.Fatalf("expected invalid target error, got %+v", invalidResponse)
	}

	ungrantedPayload, _ := json.Marshal(map[string]any{
		"project": "ungranted",
		"target":  "root",
		"files":   []fileops.SyncItem{{RelPath: "blocked.txt", Data: content}},
	})
	ungrantedResponse, err := handler(&protocol.Message{ID: "sync-ungranted", Payload: ungrantedPayload})
	if err != nil {
		t.Fatalf("ungranted sync handler failed: %v", err)
	}
	if ungrantedResponse.Error == nil || ungrantedResponse.Error.Code != "ERR_PERMISSION" {
		t.Fatalf("expected permission error, got %+v", ungrantedResponse)
	}
}

func TestPairingHandshakePinsBothPeersAndUnpairs(t *testing.T) {
	hostCert, err := transport.GenerateSelfSignedCert("pairing-host")
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := transport.GenerateSelfSignedCert("pairing-client")
	if err != nil {
		t.Fatal(err)
	}
	attackerCert, err := transport.GenerateSelfSignedCert("pairing-attacker")
	if err != nil {
		t.Fatal(err)
	}

	hostStateStore, err := store.NewStateStoreAt(filepath.Join(t.TempDir(), "host-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	clientStateStore, err := store.NewStateStoreAt(filepath.Join(t.TempDir(), "client-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := hostStateStore.Save(&store.AppState{MyFingerprint: hostCert.Fingerprint}); err != nil {
		t.Fatal(err)
	}
	pairManager := pairing.NewHostPairingManager(hostCert.Fingerprint)
	code, err := pairManager.GenerateNewCode()
	if err != nil {
		t.Fatal(err)
	}
	var pairedClientFingerprint atomic.Value
	pairedClientFingerprint.Store("")

	portListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := portListener.Addr().(*net.TCPAddr).Port
	if err := portListener.Close(); err != nil {
		t.Fatal(err)
	}

	server := transport.NewServer(port, transport.NewServerTLSConfigWithFingerprint(hostCert, func() string {
		return pairedClientFingerprint.Load().(string)
	}))
	server.SetRequestGuard(func(msg *protocol.Message) *protocol.Message {
		if pairedClientFingerprint.Load().(string) == "" &&
			msg.Type != protocol.TypePairInit && msg.Type != protocol.TypePairVerify && msg.Type != protocol.TypeUnpair {
			return protocol.NewErrorResponse(msg.ID, "ERR_UNPAIRED", "pair this device first")
		}
		return nil
	})
	registerHostPairingHandlers(server, pairManager, hostStateStore, &pairedClientFingerprint, nil, nil)
	server.RegisterHandler(protocol.TypeUnpair, func(msg *protocol.Message) (*protocol.Message, error) {
		if err := hostStateStore.Unpair(); err != nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_UNPAIR", err.Error()), nil
		}
		pairedClientFingerprint.Store("")
		pairManager.Invalidate()
		return protocol.NewResponse(msg.ID, protocol.TypeUnpair, nil)
	})
	server.RegisterHandler("pairing_probe", func(msg *protocol.Message) (*protocol.Message, error) {
		return protocol.NewResponse(msg.ID, "pairing_probe", nil)
	})
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	target := fmt.Sprintf("127.0.0.1:%d", port)
	client := transport.NewClient(target, transport.NewClientTLSConfig(clientCert, ""))
	if err := client.Connect(); err != nil {
		t.Fatalf("initial unpinned TLS connection failed: %v", err)
	}
	defer client.Close()

	wrongCode := "000000"
	if code == wrongCode {
		wrongCode = "999999"
	}
	if _, err := performClientPairing(client, wrongCode, clientCert.Fingerprint); err == nil {
		t.Fatal("pairing with a wrong code unexpectedly succeeded")
	}
	if state, err := hostStateStore.Load(); err != nil {
		t.Fatal(err)
	} else if state.PairedPeerFingerprint != "" {
		t.Fatal("host persisted a client fingerprint after a wrong code")
	}

	hostFingerprint, err := performClientPairing(client, code, clientCert.Fingerprint)
	if err != nil {
		t.Fatalf("pairing with the active code failed: %v", err)
	}
	if hostFingerprint != hostCert.Fingerprint {
		t.Fatalf("paired host fingerprint = %q, want %q", hostFingerprint, hostCert.Fingerprint)
	}
	if got := pairedClientFingerprint.Load().(string); got != clientCert.Fingerprint {
		t.Fatalf("host TLS pin = %q, want %q", got, clientCert.Fingerprint)
	}
	hostState, err := hostStateStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if hostState.PairedPeerFingerprint != clientCert.Fingerprint {
		t.Fatalf("persisted host peer fingerprint = %q, want %q", hostState.PairedPeerFingerprint, clientCert.Fingerprint)
	}
	if err := saveClientPairingState(clientStateStore, hostFingerprint, false); err != nil {
		t.Fatal(err)
	}

	if err := client.SetTLSConfig(transport.NewClientTLSConfig(clientCert, hostFingerprint)); err != nil {
		t.Fatal(err)
	}
	client.Close()
	time.Sleep(100 * time.Millisecond)

	wrongPinClient := transport.NewClient(target, transport.NewClientTLSConfig(clientCert, "wrong-host-fingerprint"))
	if err := pairingProbe(wrongPinClient, time.Second); err == nil {
		wrongPinClient.Close()
		t.Fatal("client accepted a host with the wrong pinned fingerprint")
	}
	wrongPinClient.Close()

	wrongPeerClient := transport.NewClient(target, transport.NewClientTLSConfig(attackerCert, hostFingerprint))
	if err := pairingProbe(wrongPeerClient, time.Second); err == nil {
		wrongPeerClient.Close()
		t.Fatal("host accepted a client with the wrong pinned fingerprint")
	}
	wrongPeerClient.Close()

	var pinnedClient *transport.Client
	deadline := time.Now().Add(3 * time.Second)
	for {
		candidate := transport.NewClient(target, transport.NewClientTLSConfig(clientCert, hostFingerprint))
		if err := pairingProbe(candidate, time.Second); err == nil {
			pinnedClient = candidate
			break
		}
		candidate.Close()
		if time.Now().After(deadline) {
			t.Fatal("correctly pinned client could not reconnect after rejected peers")
		}
		time.Sleep(25 * time.Millisecond)
	}
	defer pinnedClient.Close()

	if err := saveClientPairingState(clientStateStore, "", true); err != nil {
		t.Fatal(err)
	}
	if err := syncPendingClientUnpair(pinnedClient, clientStateStore); err != nil {
		t.Fatal(err)
	}
	clientState, err := clientStateStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if clientState.PairedPeerFingerprint != "" || clientState.PendingUnpair {
		t.Fatalf("client unpair state did not converge: %+v", clientState)
	}
	hostState, err = hostStateStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if hostState.PairedPeerFingerprint != "" || pairedClientFingerprint.Load().(string) != "" {
		t.Fatal("host did not clear its persisted and active peer pins on unpair")
	}
}

func pairingProbe(client *transport.Client, timeout time.Duration) error {
	if err := client.Connect(); err != nil {
		return err
	}
	response, err := client.RoundTrip(&protocol.Message{
		ID:   fmt.Sprintf("probe-%d", time.Now().UnixNano()),
		Type: "pairing_probe",
	}, timeout)
	if err != nil {
		return err
	}
	if response.Type != "pairing_probe" {
		return fmt.Errorf("unexpected probe response type %q", response.Type)
	}
	return nil
}
