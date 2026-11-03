package agentlock

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLockExcludesOtherProcessesAndReleasesOnExit(t *testing.T) {
	if os.Getenv("AGENTLOCK_CHILD_PATH") != "" {
		runLockChild(t)
		return
	}

	lockPath := filepath.Join(t.TempDir(), "mcp.lock")
	readyPath := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockExcludesOtherProcessesAndReleasesOnExit$")
	cmd.Env = append(os.Environ(),
		"AGENTLOCK_CHILD_PATH="+lockPath,
		"AGENTLOCK_READY_PATH="+readyPath,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		_ = stdin.Close()
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child process did not acquire the lock")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := AcquireAt(lockPath); !errors.Is(err, ErrBusy) {
		t.Fatalf("AcquireAt while child holds lock: got %v, want ErrBusy", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill lock-holding child process: %v", err)
	}
	_ = cmd.Wait()
	waited = true

	lock, err := AcquireAt(lockPath)
	if err != nil {
		t.Fatalf("AcquireAt after child exits: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("release lock after child exits: %v", err)
	}
}

func runLockChild(t *testing.T) {
	lock, err := AcquireAt(os.Getenv("AGENTLOCK_CHILD_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := os.WriteFile(os.Getenv("AGENTLOCK_READY_PATH"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stdin.Read(make([]byte, 1)); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

func TestLockCloseIsIdempotent(t *testing.T) {
	lock, err := AcquireAt(filepath.Join(t.TempDir(), "mcp.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}
