package permissions

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

func TestResolveValidPath(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "lanbridge-resolve-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	subDir := filepath.Join(tempDir, "src")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subDir: %v", err)
	}

	testFile := filepath.Join(subDir, "Main.cs")
	if err := os.WriteFile(testFile, []byte("// code"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	resolved, err := Resolve(tempDir, "src/Main.cs")
	if err != nil {
		t.Fatalf("Expected Resolve to succeed, got: %v", err)
	}

	if resolved != testFile {
		t.Errorf("Expected %s, got %s", testFile, resolved)
	}
}

func TestResolvePathTraversalEscape(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "lanbridge-resolve-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	_, err = Resolve(tempDir, "../../../etc/passwd")
	if err == nil {
		t.Errorf("Expected path traversal to be rejected, but it succeeded")
	}
	if _, err := Resolve(tempDir, `subdir\..\file.txt`); !errors.Is(err, ErrPathEscape) {
		t.Errorf("Expected Windows-style parent traversal to be rejected, got: %v", err)
	}
	if _, err := Resolve(tempDir, tempDir); !errors.Is(err, ErrPathEscape) {
		t.Errorf("Expected absolute path to be rejected, got: %v", err)
	}
}

func TestResolveRejectsSymlinkAncestorForMissingTarget(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "junction")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	_, err := Resolve(base, filepath.Join("junction", "new", "nested.txt"))
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("Expected symlink escape to be denied, got: %v", err)
	}
}

func TestResolveRejectsSymlinkFileOutsideBase(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "link.txt")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	_, err := Resolve(base, "link.txt")
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("Expected symlink escape to be denied, got: %v", err)
	}
}

func TestResolveRejectsWindowsJunctionAncestor(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows junction test")
	}
	base := t.TempDir()
	outside := t.TempDir()
	junction := filepath.Join(base, "junction")
	if output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", junction, outside).CombinedOutput(); err != nil {
		t.Skipf("could not create test junction: %v: %s", err, output)
	}

	_, err := Resolve(base, filepath.Join("junction", "new", "nested.txt"))
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("Expected junction escape to be denied, got: %v", err)
	}
}

func TestGatekeeperConstraints(t *testing.T) {
	tempDir := t.TempDir()

	stateStore, err := store.NewStateStoreAt(filepath.Join(tempDir, "LanBridge", "state.json"))
	if err != nil {
		t.Fatal(err)
	}

	projRoot := filepath.Join(tempDir, "Projects", "CAI")
	addinDir := filepath.Join(tempDir, "Plugins", "CAI.bundle")
	otherDir := filepath.Join(tempDir, "Unrelated")
	for _, path := range []string{projRoot, addinDir, otherDir} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &store.HostConfig{
		Projects: []store.ProjectConfig{
			{
				Name: "CAI",
				Root: projRoot,
				RelatedDirs: map[string]string{
					"addin": addinDir,
				},
			},
		},
	}

	gate, err := NewGatekeeper(stateStore, cfg)
	if err != nil {
		t.Fatalf("Failed to create gatekeeper: %v", err)
	}

	// 1. Grant sync to project root -> should succeed
	if err := gate.AddGrant(projRoot, []string{CapSync, CapRead}); err != nil {
		t.Errorf("Expected AddGrant for projRoot to succeed, got: %v", err)
	}

	// 2. Grant sync to unrelated dir -> should fail
	if err := gate.AddGrant(otherDir, []string{CapSync}); err == nil {
		t.Errorf("Expected AddGrant sync on unrelated dir to fail")
	}

	// 3. Grant read on unrelated dir (e.g. AutoCAD logs) -> should succeed
	if err := gate.AddGrant(otherDir, []string{CapRead}); err != nil {
		t.Errorf("Expected AddGrant read on any dir to succeed, got: %v", err)
	}

	// 4. Verify access check
	if !gate.CheckAccess(filepath.Join(projRoot, "test.cs"), CapSync) {
		t.Errorf("Expected CheckAccess sync on projRoot file to be true")
	}
	if runtime.GOOS == "windows" && !gate.CheckAccess(filepath.Join(strings.ToUpper(projRoot), "TEST.CS"), CapSync) {
		t.Error("Expected Windows path authorization to ignore case")
	}
	if gate.CheckAccess(filepath.Join(otherDir, "hack.exe"), CapSync) {
		t.Errorf("Expected CheckAccess sync on otherDir to be false")
	}
	if !gate.CheckAccess(filepath.Join(otherDir, "acad.log"), CapRead) {
		t.Errorf("Expected CheckAccess read on otherDir to be true")
	}
	outsideFile := filepath.Join(tempDir, "private.log")
	if err := os.WriteFile(outsideFile, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(otherDir, "linked.log")); err == nil {
		if gate.CheckAccess(filepath.Join(otherDir, "linked.log"), CapRead) {
			t.Error("Expected read grant to reject a symlink outside the granted directory")
		}
	}

	addinSubdir := filepath.Join(addinDir, "subdir")
	if err := os.MkdirAll(addinSubdir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := gate.AddGrant(addinSubdir, []string{CapSync}); err != nil {
		t.Errorf("Expected sync grant under related_dirs to succeed, got: %v", err)
	}
	if err := gate.AddGrant(otherDir, []string{CapExecute}); err == nil {
		t.Error("Expected execute grant outside project root to fail")
	}
	if err := gate.AddGrant(filepath.Join(projRoot, "nested"), []string{CapMCP}); err == nil {
		t.Error("Expected MCP grant below (not at) project root to fail")
	}
	if err := gate.RevokeGrant(projRoot); err != nil {
		t.Fatal(err)
	}
	if gate.CheckAccess(filepath.Join(projRoot, "test.cs"), CapSync) {
		t.Fatal("Revoked project grant still authorizes access")
	}
}

func TestGatekeeperPermissionRequest(t *testing.T) {
	tempDir := t.TempDir()
	projectRoot := filepath.Join(tempDir, "project")
	requestPath := filepath.Join(projectRoot, "logs")
	if err := os.MkdirAll(requestPath, 0755); err != nil {
		t.Fatal(err)
	}
	stateStore, err := store.NewStateStoreAt(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewGatekeeper(stateStore, &store.HostConfig{
		Projects: []store.ProjectConfig{{Name: "project", Root: projectRoot}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := gate.BeginRequest("req-1", requestPath, []string{CapRead, CapRead}); err != nil {
		t.Fatal(err)
	}
	requests := gate.PendingRequests()
	if len(requests) != 1 || len(requests[0].Capabilities) != 1 {
		t.Fatalf("Expected one deduplicated pending request, got %+v", requests)
	}

	result := make(chan struct {
		caps []string
		ok   bool
		err  error
	}, 1)
	go func() {
		caps, ok, err := gate.WaitForRequest(context.Background(), "req-1")
		result <- struct {
			caps []string
			ok   bool
			err  error
		}{caps: caps, ok: ok, err: err}
	}()
	if err := gate.RespondRequest("req-1", true, []string{CapRead}); err != nil {
		t.Fatal(err)
	}
	got := <-result
	if got.err != nil || !got.ok || len(got.caps) != 1 || got.caps[0] != CapRead {
		t.Fatalf("Unexpected permission response: %+v", got)
	}
	if !gate.CheckAccess(filepath.Join(requestPath, "crash.log"), CapRead) {
		t.Fatal("Approved permission was not effective")
	}
	reloaded, err := NewGatekeeper(stateStore, &store.HostConfig{
		Projects: []store.ProjectConfig{{Name: "project", Root: projectRoot}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.CheckAccess(filepath.Join(requestPath, "crash.log"), CapRead) {
		t.Fatal("Approved permission was not persisted")
	}

	rejectedPath := filepath.Join(projectRoot, "rejected")
	if err := os.MkdirAll(rejectedPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := gate.BeginRequest("req-2", rejectedPath, []string{CapRead}); err != nil {
		t.Fatal(err)
	}
	if err := gate.RespondRequest("req-2", true, []string{CapSync}); err == nil {
		t.Fatal("Expected an unrequested capability to be rejected")
	}
	if len(gate.PendingRequests()) != 1 {
		t.Fatal("Invalid response should leave the request pending")
	}
	if err := gate.RespondRequest("req-2", false, nil); err != nil {
		t.Fatal(err)
	}
	if gate.CheckAccess(filepath.Join(rejectedPath, "private.log"), CapRead) {
		t.Fatal("Rejected permission request unexpectedly granted access")
	}

	if err := gate.BeginRequest("req-3", requestPath, []string{CapRead}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, _, err := gate.WaitForRequest(ctx, "req-3"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Expected timeout for pending request, got: %v", err)
	}
	if len(gate.PendingRequests()) != 0 {
		t.Fatal("Timed-out permission request should be removed")
	}
}
