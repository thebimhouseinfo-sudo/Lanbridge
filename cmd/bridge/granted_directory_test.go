package main

import (
	"encoding/json"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/fileops"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/permissions"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/protocol"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultGrantedDirectoryRequiresOneMatchingGrant(t *testing.T) {
	grants := []store.PermissionGrant{{Path: "first", Capabilities: []string{"sync", "execute"}}, {Path: "second", Capabilities: []string{"read"}}}
	if got, err := defaultGrantedDirectory(grants, "sync"); err != nil || got != "first" {
		t.Fatalf("single sync grant: %q %v", got, err)
	}
	if _, err := defaultGrantedDirectory(grants, "mcp"); err == nil {
		t.Fatal("missing grant must require explicit selection")
	}
	grants[1].Capabilities = append(grants[1].Capabilities, "sync")
	if _, err := defaultGrantedDirectory(grants, "sync"); err == nil {
		t.Fatal("multiple grants must require explicit selection")
	}
}

func TestSyncRootUsesGrantedFoldersWithoutWorkspace(t *testing.T) {
	root := t.TempDir()
	cfg := &store.HostConfig{MaxSyncMB: 1}
	state, err := store.NewStateStoreAt(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	gate, err := permissions.NewGatekeeper(state, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = gate.AddGrant(root, []string{permissions.CapSync}); err != nil {
		t.Fatal(err)
	}
	handler := syncFilesHandlerWithProject(gate, cfg, func(id string) *store.ProjectConfig { return &store.ProjectConfig{Name: id} })
	payload, _ := json.Marshal(map[string]any{"project": "demo", "target": "root", "files": []fileops.SyncItem{{RelPath: "sample.txt", Data: []byte("ok")}}})
	response, err := handler(&protocol.Message{ID: "single", Payload: payload})
	if err != nil || response.Error != nil {
		t.Fatalf("sync: %+v %v", response, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "sample.txt"))
	if err != nil || string(data) != "ok" {
		t.Fatalf("content: %q %v", data, err)
	}
	if err = gate.AddGrant(t.TempDir(), []string{permissions.CapSync}); err != nil {
		t.Fatal(err)
	}
	response, err = handler(&protocol.Message{ID: "multiple", Payload: payload})
	if err != nil || response.Error == nil || response.Error.Code != "ERR_PARAMS" {
		t.Fatalf("ambiguous root: %+v %v", response, err)
	}
}

func TestMCPDefaultDirectorySelectsGrantContainingExecutable(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	grants := []store.PermissionGrant{{Path: a, Capabilities: []string{"mcp"}}, {Path: b, Capabilities: []string{"mcp"}}}
	dir, err := mcpGrantedDirectory(grants, []string{filepath.Join(b, "bin", "server.exe")})
	if err != nil || dir != b {
		t.Fatalf("MCP directory = %q, %v", dir, err)
	}
	if _, err = mcpGrantedDirectory(grants, []string{"server.exe"}); err == nil {
		t.Fatal("ambiguous executable must not guess")
	}
}
