package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

func newTestJob(id string, status string) *Job {
	job := &Job{ID: id, ProjectID: "demo", CommandID: "build", StartedAt: time.Now(), status: status, exitCode: 3}
	job.stdout.Write([]byte("compiled ok\n"))
	job.stderr.Write([]byte("warning: x\n"))
	return job
}

func TestJobStoreRoundTripKeepsStatusAndOutput(t *testing.T) {
	jobs, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.Save(newTestJob("demo-1", StatusDone)); err != nil {
		t.Fatal(err)
	}
	loaded, err := jobs.Load("demo-1")
	if err != nil {
		t.Fatal(err)
	}
	status, exitCode := loaded.State()
	if status != StatusDone || exitCode != 3 {
		t.Fatalf("loaded status=%s exit=%d", status, exitCode)
	}
	stdout, stderr := loaded.FullOutput()
	if string(stdout) != "compiled ok\n" || string(stderr) != "warning: x\n" {
		t.Fatalf("loaded output stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestMarkInterruptedTurnsRunningJobsIntoInterrupted(t *testing.T) {
	jobs, _ := NewJobStore(t.TempDir())
	if err := jobs.Save(newTestJob("demo-running", StatusRunning)); err != nil {
		t.Fatal(err)
	}
	if err := jobs.Save(newTestJob("demo-finished", StatusDone)); err != nil {
		t.Fatal(err)
	}
	changed, err := jobs.MarkInterrupted()
	if err != nil || changed != 1 {
		t.Fatalf("MarkInterrupted = %d, %v; want 1 changed", changed, err)
	}
	running, _ := jobs.Load("demo-running")
	if status, _ := running.State(); status != StatusInterrupted {
		t.Fatalf("running job after restart = %s, want interrupted", status)
	}
	finished, _ := jobs.Load("demo-finished")
	if status, _ := finished.State(); status != StatusDone {
		t.Fatalf("finished job must not change, got %s", status)
	}
}

func TestManagerReadsSavedJobAfterRestart(t *testing.T) {
	dir := t.TempDir()
	jobs, _ := NewJobStore(dir)
	if err := jobs.Save(newTestJob("demo-42", StatusDone)); err != nil {
		t.Fatal(err)
	}
	// A new manager has no in-memory jobs: this is the host restart case.
	restarted := NewManagerWithStore(mustStore(t, dir))
	job := restarted.GetJob("demo-42")
	if job == nil {
		t.Fatal("GetJob after restart returned nil; saved job must be readable")
	}
	if status, _ := job.State(); status != StatusDone {
		t.Fatalf("status = %s", status)
	}
	if restarted.GetJob("demo-missing") != nil {
		t.Fatal("unknown job must return nil")
	}
}

func TestJobStoreRejectsPathTraversalIDs(t *testing.T) {
	jobs, _ := NewJobStore(t.TempDir())
	for _, id := range []string{"", "../x", `..\x`, "a/b", "."} {
		if _, err := jobs.Load(id); err == nil {
			t.Fatalf("Load(%q) must be rejected", id)
		}
	}
}

func TestJobStorePrunesOldestBeyondLimit(t *testing.T) {
	dir := t.TempDir()
	jobs, _ := NewJobStore(dir)
	for i := 0; i < maxStoredJobs+5; i++ {
		job := newTestJob(fmt.Sprintf("demo-%03d", i), StatusDone)
		job.StartedAt = time.Unix(int64(1000+i), 0)
		if err := jobs.Save(job); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != maxStoredJobs {
		t.Fatalf("stored jobs = %d, want %d", len(entries), maxStoredJobs)
	}
	if _, err := os.Stat(filepath.Join(dir, "demo-000")); !os.IsNotExist(err) {
		t.Fatal("oldest job must be pruned")
	}
}

func TestRunShellPersistsFinishedJob(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	dir := t.TempDir()
	jobs := mustStore(t, filepath.Join(dir, "jobs"))
	manager := NewManagerWithStore(jobs)
	project := &store.ProjectConfig{Name: "demo-proj", Root: dir}
	job, err := manager.RunShell(project, []string{"/bin/sh", "-c", "echo hello-out; echo hello-err >&2"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if status, _ := job.State(); status != StatusRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Give the completion goroutine a moment to save the record after setResult.
	var saved *Job
	for i := 0; i < 100 && saved == nil; i++ {
		saved, _ = jobs.Load(job.ID)
		if saved == nil {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if saved == nil {
		t.Fatal("finished job was not saved")
	}
	stdout, stderr := saved.FullOutput()
	if !strings.Contains(string(stdout), "hello-out") || !strings.Contains(string(stderr), "hello-err") {
		t.Fatalf("saved output stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.HasPrefix(job.ID, "demo-proj-") {
		t.Fatalf("job id %q must keep the project prefix", job.ID)
	}
}

func mustStore(t *testing.T, dir string) *JobStore {
	t.Helper()
	s, err := NewJobStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
