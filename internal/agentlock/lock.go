package agentlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

var ErrBusy = errors.New("MCP server is already running")

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
		_ = file.Close()
		return nil, err
	}
	return &Lock{file: file, unlock: unlock}, nil
}

func (l *Lock) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
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
