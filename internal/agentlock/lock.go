package agentlock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

var ErrBusy = errors.New("MCP server is already running")

// Holder describes the process that owns the MCP lock. It is written after the
// lock is acquired and is informational only: the OS lock is the source of truth.
type Holder struct {
	PID       int       `json:"pid"`
	PPID      int       `json:"ppid"`
	StartedAt time.Time `json:"started_at"`
}

// BusyError is returned when another live or not-yet-released owner holds the lock.
type BusyError struct {
	Holder Holder
}

func (e *BusyError) Error() string {
	if e.Holder.PID == 0 {
		return ErrBusy.Error() + " (không xác định được tiến trình giữ khóa)"
	}
	return fmt.Sprintf("%s (pid %d, bắt đầu %s). Dừng tiến trình đó bằng: kill %d",
		ErrBusy.Error(), e.Holder.PID, e.Holder.StartedAt.Format(time.RFC3339), e.Holder.PID)
}

func (e *BusyError) Unwrap() error { return ErrBusy }

type Lock struct {
	file   *os.File
	unlock func() error
	once   sync.Once
	err    error
}

func Acquire() (*Lock, error) {
	dir, err := store.GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve bridge state directory: %w", err)
	}
	return AcquireAt(filepath.Join(dir, "mcp.lock"))
}

func AcquireAt(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create MCP lock directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open MCP lock file: %w", err)
	}
	unlock, err := acquireFileLock(file)
	if err != nil {
		if errors.Is(err, ErrBusy) {
			holder, _ := readHolder(file)
			_ = file.Close()
			return nil, &BusyError{Holder: holder}
		}
		_ = file.Close()
		return nil, err
	}
	if err := writeHolder(file, Holder{PID: os.Getpid(), PPID: os.Getppid(), StartedAt: time.Now().UTC()}); err != nil {
		_ = unlock()
		_ = file.Close()
		return nil, err
	}
	return &Lock{file: file, unlock: unlock}, nil
}

// Status reports whether the MCP lock is currently held and by whom, without
// taking the lock when it is busy and without changing the holder record.
type Status struct {
	Busy   bool
	Holder Holder
}

// StatusAt probes the lock file. If the lock is free, it is taken and released
// immediately so the probe is race-free with a starting MCP process.
func StatusAt(path string) (Status, error) {
	lock, err := AcquireAt(path)
	if err == nil {
		closeErr := lock.Close()
		return Status{}, closeErr
	}
	var busy *BusyError
	if errors.As(err, &busy) {
		return Status{Busy: true, Holder: busy.Holder}, nil
	}
	return Status{}, err
}

// StatusDefault probes the standard MCP lock location.
func StatusDefault() (Status, error) {
	dir, err := store.GetConfigDir()
	if err != nil {
		return Status{}, fmt.Errorf("resolve bridge state directory: %w", err)
	}
	return StatusAt(filepath.Join(dir, "mcp.lock"))
}

func readHolder(file *os.File) (Holder, error) {
	var holder Holder
	data, err := io.ReadAll(io.NewSectionReader(file, 0, 4096))
	if err != nil || len(data) == 0 {
		return holder, err
	}
	if err := json.Unmarshal(data, &holder); err != nil {
		return Holder{}, err
	}
	return holder, nil
}

func writeHolder(file *os.File, holder Holder) error {
	data, err := json.Marshal(holder)
	if err != nil {
		return fmt.Errorf("encode MCP lock holder: %w", err)
	}
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("reset MCP lock holder: %w", err)
	}
	if _, err := file.WriteAt(data, 0); err != nil {
		return fmt.Errorf("write MCP lock holder: %w", err)
	}
	return file.Sync()
}

// ProcessAlive reports whether a process with the given pid currently exists.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return processAlive(pid)
}

func (l *Lock) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		if l.file != nil {
			// Clear the holder record while the lock is still held, so a free
			// lock never reports a stale owner.
			_ = l.file.Truncate(0)
		}
		if l.unlock != nil {
			l.err = l.unlock()
		}
		if l.file != nil {
			if err := l.file.Close(); l.err == nil {
				l.err = err
			}
		}
	})
	return l.err
}
