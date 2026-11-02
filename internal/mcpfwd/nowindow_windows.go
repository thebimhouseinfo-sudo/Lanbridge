//go:build windows

package mcpfwd

import (
	"os/exec"
	"syscall"
)

func applyNoWindow(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
		HideWindow:    true,
	}
}
