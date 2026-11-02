//go:build !windows

package mcpfwd

import "os/exec"

func applyNoWindow(_ *exec.Cmd) {}
