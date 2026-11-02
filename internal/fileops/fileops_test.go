package fileops

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/permissions"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

func setupTestGate(t *testing.T, baseDir string) *permissions.Gatekeeper {
	stateStore, err := store.NewStateStoreAt(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &store.HostConfig{
		Projects: []store.ProjectConfig{
			{Name: "CAI", Root: baseDir},
		},
	}
	gate, err := permissions.NewGatekeeper(stateStore, cfg)
	if err != nil {
		t.Fatalf("Failed to create gatekeeper: %v", err)
	}
	if err := gate.AddGrant(baseDir, []string{permissions.CapRead, permissions.CapSync}); err != nil {
		t.Fatalf("Failed to grant test permissions: %v", err)
	}
	return gate
}

func TestReadOperations(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "lanbridge-read-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	gate := setupTestGate(t, tempDir)

	testFilePath := filepath.Join(tempDir, "test.log")
	content := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5\n"
	if err := os.WriteFile(testFilePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// 1. Test ListDir
	items, err := ListDir(gate, tempDir)
	if err != nil {
		t.Fatalf("ListDir failed: %v", err)
	}
	if len(items) != 1 || items[0].Name != "test.log" {
		t.Errorf("ListDir unexpected items: %+v", items)
	}

	// 2. Test ReadFileChunk
	chunk, isEOF, err := ReadFileChunk(gate, testFilePath, 0, 100)
	if err != nil {
		t.Fatalf("ReadFileChunk failed: %v", err)
	}
	if string(chunk) != content || !isEOF {
		t.Errorf("Unexpected chunk content: %s, isEOF: %v", string(chunk), isEOF)
	}

	// 3. Test ReadTailLines
	tail, err := ReadTailLines(gate, testFilePath, 2)
	if err != nil {
		t.Fatalf("ReadTailLines failed: %v", err)
	}
	if len(tail) != 2 || tail[0] != "Line 4" || tail[1] != "Line 5" {
		t.Errorf("Unexpected tail lines: %+v", tail)
	}
	if _, _, err := ReadFileChunk(gate, testFilePath, -1, 10); err == nil {
		t.Error("Expected negative read offset to fail")
	}
	if _, err := ReadTailLines(gate, testFilePath, -1); err == nil {
		t.Error("Expected negative tail line count to fail")
	}
	if _, _, err := ReadFileChunk(gate, filepath.Join(tempDir, "missing-link", "file.log"), 0, 10); err == nil {
		t.Error("Expected read outside grant to fail")
	}
	outsideFile := filepath.Join(t.TempDir(), "outside.log")
	if err := os.WriteFile(outsideFile, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	symlinkPath := filepath.Join(tempDir, "linked.log")
	if err := os.Symlink(outsideFile, symlinkPath); err == nil {
		if _, _, err := ReadFileChunk(gate, symlinkPath, 0, 10); err == nil {
			t.Error("Expected read through symlink outside grant to fail")
		}
	}
}

func TestReadLimitsAndTailErrors(t *testing.T) {
	tempDir := t.TempDir()
	gate := setupTestGate(t, tempDir)

	payload := filepath.Join(tempDir, "large.bin")
	content := make([]byte, MaxReadChunkSize+1)
	if err := os.WriteFile(payload, content, 0600); err != nil {
		t.Fatal(err)
	}

	chunk, eof, err := ReadFileChunk(gate, payload, 0, MaxReadChunkSize+1)
	if err != nil {
		t.Fatalf("ReadFileChunk failed: %v", err)
	}
	if len(chunk) != MaxReadChunkSize || eof {
		t.Fatalf("ReadFileChunk returned len=%d eof=%v; want max chunk and more data", len(chunk), eof)
	}
	defaultChunk, defaultEOF, err := ReadFileChunk(gate, payload, 0, 0)
	if err != nil {
		t.Fatalf("ReadFileChunk with omitted length failed: %v", err)
	}
	if len(defaultChunk) != MaxReadChunkSize || defaultEOF {
		t.Fatalf("default read returned len=%d eof=%v; want max chunk and more data", len(defaultChunk), defaultEOF)
	}
	if _, _, err := ReadFileChunk(gate, payload, 0, -1); err == nil {
		t.Fatal("expected negative read length to fail")
	}

	longLinePath := filepath.Join(tempDir, "long.log")
	longLine := strings.Repeat("x", 70*1024)
	if err := os.WriteFile(longLinePath, []byte(longLine+"\nlast\n"), 0600); err != nil {
		t.Fatal(err)
	}
	lines, err := ReadTailLines(gate, longLinePath, 2)
	if err != nil {
		t.Fatalf("ReadTailLines rejected a line below the 1 MB limit: %v", err)
	}
	if len(lines) != 2 || lines[0] != longLine || lines[1] != "last" {
		t.Fatalf("unexpected long-line tail: lines=%d", len(lines))
	}

	oversizedTailPath := filepath.Join(tempDir, "oversized-tail.log")
	oversizedTail := strings.Repeat("a", MaxReadChunkSize/2) + "\n" +
		strings.Repeat("b", MaxReadChunkSize/2) + "\n"
	if err := os.WriteFile(oversizedTailPath, []byte(oversizedTail), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTailLines(gate, oversizedTailPath, 2); err == nil || !strings.HasPrefix(err.Error(), "ERR_LIMIT:") {
		t.Fatalf("expected explicit tail size limit error, got %v", err)
	}

	if _, err := ReadTailLines(gate, filepath.Join(tempDir, "missing.log"), 1); err == nil || !strings.HasPrefix(err.Error(), "ERR_NOT_FOUND:") {
		t.Fatalf("expected missing file error, got %v", err)
	}
}

func TestSyncFilesAtomicAndLimit(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "lanbridge-sync-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	gate := setupTestGate(t, tempDir)

	fileData := []byte("public class Plugin { public void Init() {} }")
	hash := sha256.Sum256(fileData)
	expectedHash := hex.EncodeToString(hash[:])

	syncItems := []SyncItem{
		{RelPath: "src/Plugin.cs", Data: fileData},
	}

	// 1. Test successful sync
	results, err := SyncFiles(gate, tempDir, syncItems, 5)
	if err != nil {
		t.Fatalf("SyncFiles failed: %v", err)
	}

	if len(results) != 1 || results[0].SHA256 != expectedHash {
		t.Errorf("Sync result hash mismatch: expected %s, got %s", expectedHash, results[0].SHA256)
	}

	// Verify file was written to disk
	writtenContent, err := os.ReadFile(filepath.Join(tempDir, "src", "Plugin.cs"))
	if err != nil {
		t.Fatalf("Failed to read synced file from disk: %v", err)
	}
	if string(writtenContent) != string(fileData) {
		t.Errorf("File content on disk does not match synced data")
	}

	replacement := []byte("public class UpdatedPlugin {}")
	replacementHash := sha256.Sum256(replacement)
	replacementResults, err := SyncFiles(gate, tempDir, []SyncItem{{RelPath: "src/Plugin.cs", Data: replacement}}, 5)
	if err != nil {
		t.Fatalf("SyncFiles could not replace an existing target: %v", err)
	}
	writtenContent, err = os.ReadFile(filepath.Join(tempDir, "src", "Plugin.cs"))
	if err != nil {
		t.Fatalf("Failed to read replaced file: %v", err)
	}
	if string(writtenContent) != string(replacement) || replacementResults[0].SHA256 != hex.EncodeToString(replacementHash[:]) {
		t.Fatalf("Existing target was not replaced with the expected content and hash")
	}

	// 2. Test size limit exceeded (> 5 MB)
	largeData := make([]byte, 6*1024*1024) // 6 MB
	largeSync := []SyncItem{
		{RelPath: "valid-before-large.bin", Data: []byte("should not be written")},
		{RelPath: "large.bin", Data: largeData},
	}
	_, err = SyncFiles(gate, tempDir, largeSync, 5)
	if err == nil {
		t.Errorf("Expected SyncFiles to reject payload > 5 MB, but it succeeded")
	}
	if _, statErr := os.Stat(filepath.Join(tempDir, "valid-before-large.bin")); !os.IsNotExist(statErr) {
		t.Fatalf("oversized batch wrote a file before rejection: stat err=%v", statErr)
	}

	outsideDir := t.TempDir()
	if _, err := SyncFiles(gate, outsideDir, []SyncItem{{RelPath: "denied.txt", Data: []byte("blocked")}}, 5); err == nil {
		t.Fatal("expected sync outside the granted project root to fail")
	}
	if _, err := SyncFiles(gate, tempDir, []SyncItem{{RelPath: ".", Data: []byte("blocked")}}, 5); err == nil {
		t.Fatal("expected sync to reject a path that targets the directory")
	}

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tempDir, "escape")
	if err := os.Symlink(outside, link); err == nil {
		_, err := SyncFiles(gate, tempDir, []SyncItem{{RelPath: filepath.Join("escape", "new", "file.txt"), Data: []byte("blocked")}}, 5)
		if err == nil {
			t.Error("Expected sync through symlink ancestor to fail")
		}
		if data, readErr := os.ReadFile(outside); readErr != nil || string(data) != "outside" {
			t.Errorf("Symlink target changed after rejected sync: %q, %v", data, readErr)
		}
	}
}
