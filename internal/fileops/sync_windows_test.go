//go:build windows

package fileops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestSyncFilesReportsLockedWindowsFile(t *testing.T) {
	tempDir := t.TempDir()
	gate := setupTestGate(t, tempDir)
	target := filepath.Join(tempDir, "locked.dll")
	original := []byte("loaded DLL")
	if err := os.WriteFile(target, original, 0600); err != nil {
		t.Fatal(err)
	}

	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(target),
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatalf("failed to open lock fixture: %v", err)
	}
	defer windows.CloseHandle(handle)

	_, err = SyncFiles(gate, tempDir, []SyncItem{{RelPath: "locked.dll", Data: []byte("replacement")}}, 5)
	if err == nil || !strings.Contains(err.Error(), "ERR_FILE_LOCKED") {
		t.Fatalf("expected clear locked-file error, got %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("locked file changed: got %q", got)
	}
}
