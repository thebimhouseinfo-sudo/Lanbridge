package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// StatusInterrupted marks a job that was still running when the host stopped.
// Its process was killed with the host, so its output is whatever was saved.
const StatusInterrupted = "interrupted"

const (
	jobStatusFile = "status.json"
	jobStdoutFile = "stdout.log"
	jobStderrFile = "stderr.log"
	// maxStoredJobs bounds disk use: the oldest job directories are removed first.
	maxStoredJobs = 50
)

// JobStore keeps finished job status and output on disk so a command result can
// be read after the MCP session or the host restarts. Each job lives in
// <dir>/<job-id>/ with status.json, stdout.log and stderr.log.
type JobStore struct {
	dir string
}

type jobRecord struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	CommandID   string    `json:"command_id"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	Status      string    `json:"status"`
	ExitCode    int       `json:"exit_code"`
	StdoutBytes int64     `json:"stdout_bytes"`
	StderrBytes int64     `json:"stderr_bytes"`
}

// NewJobStore creates the job directory if needed.
func NewJobStore(dir string) (*JobStore, error) {
	if dir == "" {
		return nil, errors.New("job store directory is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create job store: %w", err)
	}
	return &JobStore{dir: dir}, nil
}

func (s *JobStore) jobDir(jobID string) (string, error) {
	if jobID == "" || strings.ContainsAny(jobID, `/\`) || jobID == "." || jobID == ".." {
		return "", fmt.Errorf("invalid job id %q", jobID)
	}
	return filepath.Join(s.dir, jobID), nil
}

// Save writes a finished job. Status is written last, so a partially written
// job is never reported as complete.
func (s *JobStore) Save(job *Job) error {
	status, exitCode, stdout, stderr := job.finalSnapshot()
	dir, err := s.jobDir(job.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create job directory: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(dir, jobStdoutFile), stdout); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, jobStderrFile), stderr); err != nil {
		return err
	}
	record := jobRecord{
		ID:          job.ID,
		ProjectID:   job.ProjectID,
		CommandID:   job.CommandID,
		StartedAt:   job.StartedAt,
		FinishedAt:  time.Now(),
		Status:      status,
		ExitCode:    exitCode,
		StdoutBytes: int64(len(stdout)),
		StderrBytes: int64(len(stderr)),
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode job status: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(dir, jobStatusFile), data); err != nil {
		return err
	}
	s.prune()
	return nil
}

// Load rebuilds a finished job, including its output, from disk.
func (s *JobStore) Load(jobID string) (*Job, error) {
	dir, err := s.jobDir(jobID)
	if err != nil {
		return nil, err
	}
	record, err := readJobRecord(dir)
	if err != nil {
		return nil, err
	}
	stdout, err := os.ReadFile(filepath.Join(dir, jobStdoutFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read job stdout: %w", err)
	}
	stderr, err := os.ReadFile(filepath.Join(dir, jobStderrFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read job stderr: %w", err)
	}
	job := &Job{
		ID:        record.ID,
		ProjectID: record.ProjectID,
		CommandID: record.CommandID,
		StartedAt: record.StartedAt,
		status:    record.Status,
		exitCode:  record.ExitCode,
	}
	job.stdout.Write(stdout)
	job.stderr.Write(stderr)
	return job, nil
}

// MarkInterrupted turns every job that was still "running" into "interrupted".
// Call it once at host start: a job cannot survive the host process.
func (s *JobStore) MarkInterrupted() (int, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0, fmt.Errorf("read job store: %w", err)
	}
	changed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(s.dir, entry.Name())
		record, err := readJobRecord(dir)
		if err != nil || record.Status != StatusRunning {
			continue
		}
		record.Status = StatusInterrupted
		record.FinishedAt = time.Now()
		data, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			return changed, err
		}
		if err := writeFileAtomic(filepath.Join(dir, jobStatusFile), data); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// prune removes the oldest job directories beyond maxStoredJobs.
func (s *JobStore) prune() {
	entries, err := os.ReadDir(s.dir)
	if err != nil || len(entries) <= maxStoredJobs {
		return
	}
	type stored struct {
		dir     string
		started time.Time
	}
	var jobs []stored
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(s.dir, entry.Name())
		record, err := readJobRecord(dir)
		if err != nil {
			continue
		}
		jobs = append(jobs, stored{dir: dir, started: record.StartedAt})
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].started.Before(jobs[j].started) })
	for i := 0; i < len(jobs)-maxStoredJobs; i++ {
		_ = os.RemoveAll(jobs[i].dir)
	}
}

func readJobRecord(dir string) (jobRecord, error) {
	var record jobRecord
	data, err := os.ReadFile(filepath.Join(dir, jobStatusFile))
	if err != nil {
		return record, err
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, fmt.Errorf("decode job status: %w", err)
	}
	return record, nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}

// finalSnapshot returns status and full output under one lock.
func (j *Job) finalSnapshot() (string, int, []byte, []byte) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status, j.exitCode, j.stdout.Bytes(), j.stderr.Bytes()
}
