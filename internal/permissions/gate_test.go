package permissions

import (
	"path/filepath"
	"testing"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

func TestUnpairClearsPeerFingerprintAndActiveGrants(t *testing.T) {
	root := t.TempDir()
	stateStore, err := store.NewStateStoreAt(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := stateStore.Save(&store.AppState{PairedPeerFingerprint: "trusted-peer"}); err != nil {
		t.Fatal(err)
	}
	gate, err := NewGatekeeper(stateStore, &store.HostConfig{
		Projects: []store.ProjectConfig{{Name: "test", Root: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.AddGrant(root, []string{CapRead}); err != nil {
		t.Fatal(err)
	}
	if !gate.HasPermission(root, CapRead) {
		t.Fatal("test grant was not active before unpair")
	}

	if err := gate.Unpair(); err != nil {
		t.Fatal(err)
	}
	if gate.HasPermission(root, CapRead) || len(gate.ListGrants()) != 0 {
		t.Fatal("unpair left an in-memory permission grant active")
	}
	state, err := stateStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.PairedPeerFingerprint != "" || len(state.Permissions) != 0 {
		t.Fatalf("unpair did not clear persisted peer and grants: %+v", state)
	}
}
