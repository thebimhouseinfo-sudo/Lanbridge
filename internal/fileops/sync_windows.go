//go:build windows

package fileops

import (
	"errors"

	"golang.org/x/sys/windows"
)

func isFileLockError(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
