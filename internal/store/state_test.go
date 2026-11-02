package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateStoreAtomicSaveLoad(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "lanbridge-store-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store := &StateStore{
		filePath: filepath.Join(tempDir, "state.json"),
	}

	initialState := &AppState{
		MyFingerprint:         "fp-host-123",
		PairedPeerFingerprint: "fp-client-456",
		PendingUnpair:         true,
		Permissions: []PermissionGrant{
			{Path: "D:\\Repos\\CAI", Capabilities: []string{"read", "sync"}},
		},
	}

	if err := store.Save(initialState); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.MyFingerprint != "fp-host-123" {
		t.Errorf("Expected MyFingerprint 'fp-host-123', got '%s'", loaded.MyFingerprint)
	}
	if loaded.PairedPeerFingerprint != "fp-client-456" {
		t.Errorf("Expected PairedPeerFingerprint 'fp-client-456', got '%s'", loaded.PairedPeerFingerprint)
	}
	if len(loaded.Permissions) != 1 || loaded.Permissions[0].Path != "D:\\Repos\\CAI" {
		t.Errorf("Permissions not preserved")
	}

	// Test Unpair
	if err := store.Unpair(); err != nil {
		t.Fatalf("Unpair failed: %v", err)
	}

	unpairedState, err := store.Load()
	if err != nil {
		t.Fatalf("Load after unpair failed: %v", err)
	}
	if unpairedState.PairedPeerFingerprint != "" {
		t.Errorf("Expected empty PairedPeerFingerprint after unpair")
	}
	if unpairedState.PendingUnpair {
		t.Error("Expected pending unpair marker to be cleared")
	}
	if len(unpairedState.Permissions) != 0 {
		t.Errorf("Expected cleared permissions after unpair")
	}
}

func TestStateStorePersistsPendingUnpair(t *testing.T) {
	stateStore, err := NewStateStoreAt(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := stateStore.Save(&AppState{PendingUnpair: true}); err != nil {
		t.Fatal(err)
	}
	state, err := stateStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !state.PendingUnpair {
		t.Fatal("pending unpair marker was not persisted")
	}
}
