package ui

import (
	"strings"
	"testing"
)

func TestInsideScope(t *testing.T) {
	scope := `C:\Dev\LanBridge`
	for p, want := range map[string]bool{
		`C:\Dev\LanBridge`:           true,
		`c:/dev/lanbridge/cmd/x.exe`: true,
		`C:\Dev\LanBridge2`:          false,
		`C:\Dev`:                     false,
		`D:\Dev\LanBridge`:           false,
		`C:\Program Files\Autodesk`:  false,
	} {
		got, err := insideScope(scope, p)
		if err != nil || got != want {
			t.Fatalf("%q: got %v err %v want %v", p, got, err, want)
		}
	}
	if _, err := insideScope(scope, `C:\Dev\LanBridge\..\Other`); err == nil {
		t.Fatal("expected .. rejection")
	}
}

func TestRemoteConfigGuards(t *testing.T) {
	ws := &WebServer{}
	var granted []string
	ws.onGrantUpdate = func(p string, _ []string) error { granted = append(granted, p); return nil }
	if _, err := ws.HandleRemoteConfig(RemoteConfigRequest{Action: "grant_folder", Path: `C:\Dev\LanBridge`}); err == nil || !strings.Contains(err.Error(), "REMOTE_CONFIG_DISABLED") {
		t.Fatalf("disabled must refuse, got %v", err)
	}
	ws.state.RemoteConfig, ws.state.RemoteScope = true, `C:\Dev\LanBridge`
	if _, err := ws.HandleRemoteConfig(RemoteConfigRequest{Action: "grant_folder", Path: `C:\CAD`}); err == nil || !strings.Contains(err.Error(), "OUTSIDE_SCOPE") {
		t.Fatalf("outside scope must refuse, got %v", err)
	}
	if _, err := ws.HandleRemoteConfig(RemoteConfigRequest{Action: "grant_folder", Path: `C:\Dev\LanBridge\sub`}); err != nil || len(granted) != 1 {
		t.Fatalf("inside scope should grant: %v %v", err, granted)
	}
	ws.DisableRemoteConfig()
	if _, err := ws.HandleRemoteConfig(RemoteConfigRequest{Action: "save"}); err == nil {
		t.Fatal("disabled again must refuse")
	}
}
