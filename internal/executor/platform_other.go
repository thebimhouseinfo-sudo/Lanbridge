//go:build !windows

package executor

import "os/exec"

// applyPlatformAttrs is a no-op on non-Windows platforms (macOS build).
func applyPlatformAttrs(_ *exec.Cmd) {}

// AttachJobObject is a no-op on non-Windows platforms.
func AttachJobObject(_ int) (func(), error) {
	return func() {}, nil
}
