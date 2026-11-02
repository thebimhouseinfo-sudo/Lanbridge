package executor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

func TestCircBuf_BasicWrite(t *testing.T) {
	var cb circBuf
	cb.Write([]byte("hello"))
	cb.Write([]byte(" world"))
	if got := string(cb.Bytes()); got != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestCircBuf_Tail(t *testing.T) {
	var cb circBuf
	cb.Write([]byte("abcde"))
	tail := cb.Tail(3)
	if string(tail) != "cde" {
		t.Fatalf("expected 'cde', got %q", string(tail))
	}
}

func TestCircBuf_Truncation(t *testing.T) {
	// Override maxOutputBytes temporarily by writing a large buffer
	var cb circBuf
	big := strings.Repeat("X", maxOutputBytes/2)
	cb.Write([]byte(big))
	cb.Write([]byte(big))
	cb.Write([]byte("tail"))

	data := cb.Bytes()
	// Must contain truncation marker
	if !strings.Contains(string(data), "truncated") {
		t.Error("expected truncation note in output")
	}
	// Must end with "tail"
	if !strings.HasSuffix(string(data), "tail") {
		t.Errorf("expected output to end with 'tail', got last 20 bytes: %q", string(data[max(0, len(data)-20):]))
	}
}

func TestCircBuf_ReadSinceReportsOffsetsAndTruncation(t *testing.T) {
	var output circBuf
	output.Write([]byte(strings.Repeat("a", maxOutputBytes)))
	output.Write([]byte("tail"))

	chunk := output.ReadSince(0, 4)
	if !chunk.Truncated || string(chunk.Data) != "aaaa" {
		t.Fatalf("first chunk = %+v, want truncated retained output", chunk)
	}
	if chunk.NextOffset != 8 || chunk.TotalBytes != int64(maxOutputBytes+4) {
		t.Fatalf("chunk offsets = %+v", chunk)
	}
	tail := output.ReadSince(chunk.TotalBytes-4, 4)
	if tail.Truncated || string(tail.Data) != "tail" {
		t.Fatalf("last chunk = %+v, want tail", tail)
	}
}

func TestCircBuf_ReadTailReturnsLatestChunk(t *testing.T) {
	var output circBuf
	output.Write([]byte("first-"))
	output.Write([]byte("last"))

	chunk := output.ReadTail(4)
	if string(chunk.Data) != "last" || chunk.NextOffset != 10 || chunk.TotalBytes != 10 {
		t.Fatalf("tail chunk = %+v, want latest four bytes", chunk)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func TestManager_UnknownCommand(t *testing.T) {
	m := NewManager()
	proj := &store.ProjectConfig{
		Name: "TESTPROJ",
		Root: t.TempDir(),
		Commands: map[string][]string{
			"build":        {"echo", "hi"},
			"custom-shell": {"cmd.exe", "/c", "whoami"},
		},
	}
	_, err := m.RunCommand(proj, "custom-shell")
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}

func TestManager_BusyProject(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live process test in short mode")
	}
	m := NewManager()
	dir := t.TempDir()
	proj := &store.ProjectConfig{
		Name:              "TESTPROJ",
		Root:              dir,
		Commands:          map[string][]string{"test": {"sleep", "5"}},
		CommandTimeoutMin: 1,
	}
	job, err := m.RunCommand(proj, "test")
	if err != nil {
		t.Skipf("sleep not available: %v", err)
	}
	defer job.Cancel()

	// Second call must be rejected
	_, err = m.RunCommand(proj, "test")
	if err == nil {
		t.Fatal("expected busy error for second concurrent command")
	}
}

func TestManager_GetJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live process test in short mode")
	}
	m := NewManager()
	dir := t.TempDir()
	proj := &store.ProjectConfig{
		Name:              "GETJOB",
		Root:              dir,
		Commands:          map[string][]string{"build": {"echo", "done"}},
		CommandTimeoutMin: 1,
	}
	job, err := m.RunCommand(proj, "build")
	if err != nil {
		t.Skip("echo not available:", err)
	}

	// Wait for job to complete
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, _ := job.State()
		if status != StatusRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	got := m.GetJob(job.ID)
	if got == nil {
		t.Fatal("GetJob returned nil for known job ID")
	}
	if got.ID != job.ID {
		t.Fatalf("ID mismatch: %q vs %q", got.ID, job.ID)
	}
}

func TestManagerWaitDoesNotBlockOnDescendantHoldingOutput(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	t.Setenv("LANBRIDGE_EXECUTOR_HELPER", "parent")
	t.Setenv("LANBRIDGE_EXECUTOR_HELPER_PID_FILE", pidFile)

	manager := NewManager()
	project := &store.ProjectConfig{
		Name:              "PIPEWAIT",
		Root:              t.TempDir(),
		Commands:          map[string][]string{"test": {os.Args[0], "-test.run=^TestExecutorWaitDelayHelper$"}},
		CommandTimeoutMin: 1,
	}
	job, err := manager.RunCommand(project, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return
		}
		pid, err := strconv.Atoi(string(data))
		if err != nil {
			return
		}
		process, err := os.FindProcess(pid)
		if err == nil {
			_ = process.Kill()
		}
	})

	deadline := time.Now().Add(6 * time.Second)
	status, _ := job.State()
	for time.Now().Before(deadline) && status == StatusRunning {
		time.Sleep(25 * time.Millisecond)
		status, _ = job.State()
	}
	if status == StatusRunning {
		t.Fatal("command remained blocked by a descendant holding stdout/stderr")
	}
}

func TestManagerPreservesCommandOutputAndExitCode(t *testing.T) {
	t.Setenv("LANBRIDGE_EXECUTOR_HELPER", "output")
	manager := NewManager()
	project := &store.ProjectConfig{
		Name: "OUTPUT",
		Root: t.TempDir(),
		Commands: map[string][]string{
			"build": {os.Args[0], "-test.run=^TestExecutorWaitDelayHelper$"},
		},
	}
	job, err := manager.RunCommand(project, "build")
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	status, _ := job.State()
	for time.Now().Before(deadline) && status == StatusRunning {
		time.Sleep(25 * time.Millisecond)
		status, _ = job.State()
	}
	if status == StatusRunning {
		t.Fatal("command did not finish")
	}
	status, exitCode, stdout, stderr := job.SnapshotResult()
	if status != StatusDone || exitCode != 7 {
		t.Fatalf("command result = (%q, %d), want (%q, 7)", status, exitCode, StatusDone)
	}
	if string(stdout) != "stdout-marker\n" || string(stderr) != "stderr-marker\n" {
		t.Fatalf("command output = (%q, %q)", stdout, stderr)
	}
}

func TestExecutorWaitDelayHelper(t *testing.T) {
	switch os.Getenv("LANBRIDGE_EXECUTOR_HELPER") {
	case "parent":
		child := exec.Command(os.Args[0], "-test.run=^TestExecutorWaitDelayHelper$")
		child.Env = append(os.Environ(), "LANBRIDGE_EXECUTOR_HELPER=descendant")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
	case "output":
		_, _ = os.Stdout.Write([]byte("stdout-marker\n"))
		_, _ = os.Stderr.Write([]byte("stderr-marker\n"))
		os.Exit(7)
	case "descendant":
		if err := os.WriteFile(os.Getenv("LANBRIDGE_EXECUTOR_HELPER_PID_FILE"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Second)
	}
}
