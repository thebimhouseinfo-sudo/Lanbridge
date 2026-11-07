//go:build windows

package agentlock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// windowsLockOffset is far beyond the holder record. Windows locks are
// mandatory, so locking the record itself would stop other handles from
// reading which process holds the lock.
const windowsLockOffset = 1 << 30

func acquireFileLock(file *os.File) (func() error, error) {
	overlapped := windows.Overlapped{Offset: windowsLockOffset}
	err := windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&overlapped,
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return func() error {
		return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
	}, nil
}

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = process.Release()
	return true
}
