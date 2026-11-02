// Package executor runs project-scoped commands on Windows.
//
// Exactly one command runs per project at a time. Output is kept in memory
// (capped at ~20 MB per job; older data is discarded with a truncation note).
// Completed jobs are retained until a new job starts for the same project.
package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

const (
	maxOutputBytes   = 10 * 1024 * 1024 // 10 MB per output stream; 20 MB per job
	OutputChunkLimit = 200 * 1024       // 200 KB returned per poll
	waitDelay        = 3 * time.Second  // drain output after the main process exits
)

// Status values for a Job.
const (
	StatusRunning  = "running"
	StatusDone     = "done"
	StatusTimeout  = "timeout"
	StatusCanceled = "canceled"
)

// Job holds runtime state for one command invocation.
type Job struct {
	ID        string
	ProjectID string
	CommandID string
	StartedAt time.Time

	mu       sync.Mutex
	status   string // running | done | timeout | canceled
	exitCode int
	stdout   circBuf
	stderr   circBuf
	cancel   context.CancelFunc
}

// appendOut is safe to call from multiple goroutines.
func (j *Job) appendOut(b []byte, isSterr bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if isSterr {
		j.stderr.Write(b)
	} else {
		j.stdout.Write(b)
	}
}

// Snapshot returns up to returnOutputCap bytes from the tail of each stream.
func (j *Job) Snapshot() (stdout, stderr []byte) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.stdout.Tail(OutputChunkLimit), j.stderr.Tail(OutputChunkLimit)
}

// SnapshotResult returns the status, exit code, and output under one lock.
func (j *Job) SnapshotResult() (status string, exitCode int, stdout, stderr []byte) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status, j.exitCode, j.stdout.Tail(OutputChunkLimit), j.stderr.Tail(OutputChunkLimit)
}

type OutputChunk struct {
	Data       []byte
	NextOffset int64
	TotalBytes int64
	Truncated  bool
}

func (j *Job) ReadOutput(stdoutOffset, stderrOffset int64, limit int) (string, int, OutputChunk, OutputChunk) {
	j.mu.Lock()
	defer j.mu.Unlock()
	read := func(output *circBuf, offset int64) OutputChunk {
		if offset == -1 {
			return output.ReadTail(limit)
		}
		return output.ReadSince(offset, limit)
	}
	return j.status, j.exitCode,
		read(&j.stdout, stdoutOffset),
		read(&j.stderr, stderrOffset)
}

func (j *Job) State() (status string, exitCode int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status, j.exitCode
}

func (j *Job) setResult(status string, exitCode int) {
	j.mu.Lock()
	j.status = status
	j.exitCode = exitCode
	j.mu.Unlock()
}

// FullOutput returns all buffered output (up to maxOutputBytes).
func (j *Job) FullOutput() (stdout, stderr []byte) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.stdout.Bytes(), j.stderr.Bytes()
}

// Cancel terminates the job via context cancellation.
func (j *Job) Cancel() {
	if j.cancel != nil {
		j.cancel()
	}
}

// circBuf is a circular buffer that caps at maxOutputBytes.
// When full it discards the oldest data and prepends a "[truncated]\n" note.
type circBuf struct {
	buf       bytes.Buffer
	discarded int64
	total     int64
}

func (c *circBuf) Write(b []byte) {
	c.total += int64(len(b))
	if len(b) >= maxOutputBytes {
		c.discarded += int64(c.buf.Len() + len(b) - maxOutputBytes)
		c.buf.Reset()
		c.buf.Write(b[len(b)-maxOutputBytes:])
		return
	}
	overflow := c.buf.Len() + len(b) - maxOutputBytes
	if overflow > 0 {
		data := c.buf.Bytes()
		tail := append([]byte(nil), data[overflow:]...)
		c.discarded += int64(overflow)
		c.buf.Reset()
		c.buf.Write(tail)
	}
	c.buf.Write(b)
}

func (c *circBuf) Bytes() []byte {
	if c.discarded == 0 {
		return c.buf.Bytes()
	}
	return append([]byte("[output truncated — giữ phần cuối]\n"), c.buf.Bytes()...)
}

func (c *circBuf) Tail(n int) []byte {
	b := c.buf.Bytes()
	if len(b) <= n {
		return c.Bytes()
	}
	return b[len(b)-n:]
}

func (c *circBuf) ReadSince(offset int64, limit int) OutputChunk {
	if limit <= 0 {
		limit = OutputChunkLimit
	}
	truncated := offset < c.discarded
	if truncated {
		offset = c.discarded
	}
	if offset > c.total {
		offset = c.total
	}
	start := offset - c.discarded
	if start < 0 {
		start = 0
	}
	data := c.buf.Bytes()
	end := start + int64(limit)
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	chunk := append([]byte(nil), data[start:end]...)
	return OutputChunk{
		Data:       chunk,
		NextOffset: c.discarded + end,
		TotalBytes: c.total,
		Truncated:  truncated,
	}
}

func (c *circBuf) ReadTail(limit int) OutputChunk {
	if limit <= 0 {
		limit = OutputChunkLimit
	}
	offset := c.total - int64(limit)
	if offset < c.discarded {
		offset = c.discarded
	}
	chunk := c.ReadSince(offset, limit)
	chunk.Truncated = c.discarded > 0
	return chunk
}

// Manager runs commands and tracks jobs, one active job per project.
type Manager struct {
	mu            sync.Mutex
	jobs          map[string]*Job // project name → latest job
	seq           uint64
	onStateChange func()
}

// NewManager creates an empty Manager.
func NewManager() *Manager {
	return &Manager{jobs: make(map[string]*Job)}
}

func (m *Manager) SetStateChangeHandler(handler func()) {
	m.mu.Lock()
	m.onStateChange = handler
	m.mu.Unlock()
}

func (m *Manager) notifyStateChange() {
	m.mu.Lock()
	handler := m.onStateChange
	m.mu.Unlock()
	if handler != nil {
		handler()
	}
}

func (m *Manager) HasRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, job := range m.jobs {
		if status, _ := job.State(); status == StatusRunning {
			return true
		}
	}
	return false
}

// RunCommand starts a command for the given project and returns the job.
// Returns an error if the project is already busy.
func (m *Manager) RunCommand(proj *store.ProjectConfig, commandID string) (*Job, error) {
	if !supportedCommandID(commandID) {
		return nil, fmt.Errorf("lệnh '%s' không được hỗ trợ", commandID)
	}
	argv, ok := proj.Commands[commandID]
	if !ok || len(argv) == 0 {
		return nil, fmt.Errorf("lệnh '%s' không được khai báo trong config cho project '%s'", commandID, proj.Name)
	}

	m.mu.Lock()
	existing := m.jobs[proj.Name]
	if existing != nil {
		status, _ := existing.State()
		if status == StatusRunning {
			m.mu.Unlock()
			return nil, fmt.Errorf("project '%s' đang chạy lệnh '%s', vui lòng đợi", proj.Name, existing.CommandID)
		}
	}

	m.seq++
	jobID := fmt.Sprintf("%s-%d", proj.Name, m.seq)
	timeoutMin := proj.CommandTimeoutMin
	if timeoutMin <= 0 {
		timeoutMin = 30
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMin)*time.Minute)
	job := &Job{
		ID:        jobID,
		ProjectID: proj.Name,
		CommandID: commandID,
		StartedAt: time.Now(),
		status:    StatusRunning,
		cancel:    cancel,
	}
	m.jobs[proj.Name] = job
	m.mu.Unlock()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = proj.Root
	cmd.Stdout = jobOutput{job: job}
	cmd.Stderr = jobOutput{job: job, isStderr: true}
	cmd.WaitDelay = waitDelay

	// Platform-specific setup: CREATE_NO_WINDOW + Job Object on Windows.
	applyPlatformAttrs(cmd)

	if err := cmd.Start(); err != nil {
		cancel()
		job.setResult(StatusCanceled, -1)
		return nil, fmt.Errorf("không khởi động được lệnh: %w", err)
	}

	// Attach Windows Job Object immediately so all child processes (MSBuild nodes, etc.)
	// are killed when the bridge exits or the job is canceled.
	jobCleanup, err := AttachJobObject(cmd.Process.Pid)
	if err != nil {
		cancel()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		job.setResult(StatusCanceled, -1)
		return nil, fmt.Errorf("không thể gắn lệnh vào Windows Job Object: %w", err)
	}
	m.notifyStateChange()

	// Wait for process in background; update job status when done.
	go func() {
		waitErr := cmd.Wait()
		ctxErr := ctx.Err()
		jobCleanup() // close Job Object handle — kills any lingering child processes
		cancel()     // release context resources

		status := StatusDone
		if ctxErr == context.DeadlineExceeded {
			status = StatusTimeout
		} else if errors.Is(ctxErr, context.Canceled) && waitErr != nil {
			status = StatusCanceled
		}

		exitCode := 0
		if waitErr != nil {
			if exitErr, ok := waitErr.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}
		job.setResult(status, exitCode)
		m.notifyStateChange()
	}()

	return job, nil
}

func supportedCommandID(commandID string) bool {
	switch commandID {
	case "build", "clean-build", "install", "restore", "doctor", "test", "status", "version":
		return true
	default:
		return false
	}
}

// GetJob retrieves the current job for a project by job ID.
func (m *Manager) GetJob(jobID string) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	// jobID encodes project name as prefix before the last dash+number
	parts := strings.LastIndex(jobID, "-")
	if parts < 0 {
		return nil
	}
	projName := jobID[:parts]
	j := m.jobs[projName]
	if j != nil && j.ID == jobID {
		return j
	}
	return nil
}

// CancelAll cancels all running jobs (called on graceful shutdown).
func (m *Manager) CancelAll() {
	m.mu.Lock()
	jobs := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		jobs = append(jobs, j)
	}
	m.mu.Unlock()
	for _, job := range jobs {
		if status, _ := job.State(); status == StatusRunning {
			job.Cancel()
		}
	}
}

type jobOutput struct {
	job      *Job
	isStderr bool
}

func (w jobOutput) Write(data []byte) (int, error) {
	w.job.appendOut(data, w.isStderr)
	return len(data), nil
}
