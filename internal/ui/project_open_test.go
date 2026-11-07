package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/projectconfig"
)

func TestEnsureProjectOpenCreatesAndOpensMissingProject(t *testing.T) {
	var setupCalls []string
	var opened []string
	ws := &WebServer{}
	ws.onProjectSetup = func(id, _, _ string) error { setupCalls = append(setupCalls, id); return nil }
	ws.onProjectOpen = func(id string) error { opened = append(opened, id); return nil }

	created, err := ws.EnsureProjectOpen("cadgpt-wip-vision")
	if err != nil || !created {
		t.Fatalf("EnsureProjectOpen = %v, %v; want created=true", created, err)
	}
	if len(setupCalls) != 1 || len(opened) != 1 || opened[0] != "cadgpt-wip-vision" {
		t.Fatalf("setup=%v open=%v", setupCalls, opened)
	}
}

func TestEnsureProjectOpenIsIdempotentForExistingProject(t *testing.T) {
	ws := &WebServer{}
	ws.onProjectSetup = func(id, _, _ string) error {
		return fmt.Errorf("%w: %s", projectconfig.ErrAlreadyExists, id)
	}
	var opened []string
	ws.onProjectOpen = func(id string) error { opened = append(opened, id); return nil }

	created, err := ws.EnsureProjectOpen("CAI")
	if err != nil {
		t.Fatalf("existing project must not be an error, got %v", err)
	}
	if created {
		t.Fatal("existing project must report created=false")
	}
	if len(opened) != 1 || opened[0] != "CAI" {
		t.Fatalf("existing project must still be opened, got %v", opened)
	}
}

func TestEnsureProjectOpenReportsRealSetupFailure(t *testing.T) {
	ws := &WebServer{}
	ws.onProjectSetup = func(string, string, string) error { return errors.New("disk full") }
	ws.onProjectOpen = func(string) error { t.Fatal("open must not run after a real setup failure"); return nil }
	if _, err := ws.EnsureProjectOpen("X"); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("setup failure must surface, got %v", err)
	}
}

func TestRemoteSetupProjectIsIdempotent(t *testing.T) {
	ws := &WebServer{}
	ws.state.RemoteConfig, ws.state.RemoteScope = true, `C:\Dev\LanBridge`
	ws.onProjectSetup = func(id, _, _ string) error {
		return fmt.Errorf("%w: %s", projectconfig.ErrAlreadyExists, id)
	}
	out, err := ws.HandleRemoteConfig(RemoteConfigRequest{Action: "setup_project", ProjectID: "CAI"})
	if err != nil {
		t.Fatalf("setup_project on existing project must succeed, got %v", err)
	}
	result := out.(map[string]any)
	if result["already_exists"] != true || result["created"] != false {
		t.Fatalf("result = %v", result)
	}
}

func TestRemoteBootstrapToleratesExistingProject(t *testing.T) {
	ws := &WebServer{}
	ws.state.RemoteConfig, ws.state.RemoteScope = true, `C:\Dev\LanBridge`
	ws.onProjectSetup = func(id, _, _ string) error {
		return fmt.Errorf("%w: %s", projectconfig.ErrAlreadyExists, id)
	}
	var opened string
	ws.onProjectOpen = func(id string) error { opened = id; return nil }
	ws.onGrantUpdate = func(string, []string) error { return nil }
	if _, err := ws.HandleRemoteConfig(RemoteConfigRequest{Action: "bootstrap", ProjectID: "cadgpt-wip-vision"}); err != nil {
		t.Fatalf("bootstrap on existing project must succeed, got %v", err)
	}
	if opened != "cadgpt-wip-vision" {
		t.Fatalf("bootstrap opened %q", opened)
	}
}
