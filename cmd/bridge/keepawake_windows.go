//go:build windows

package main

import (
	"log/slog"
	"runtime"
	"time"

	"golang.org/x/sys/windows"
)

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

var setThreadExecutionState = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")

// runKeepAwake holds one OS thread for its whole life, because the execution
// state applies to the calling thread.
func runKeepAwake(done <-chan struct{}, stopped chan<- struct{}, started chan<- bool) {
	runtime.LockOSThread()
	defer close(stopped)
	defer runtime.UnlockOSThread()

	set := func(flags uintptr) bool {
		ret, _, _ := setThreadExecutionState.Call(flags)
		return ret != 0
	}
	first := set(esContinuous | esSystemRequired)
	started <- first
	if !first {
		slog.Warn("SetThreadExecutionState failed")
	}
	ticker := time.NewTicker(hostKeepAwakeRefresh)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			set(esContinuous)
			return
		case <-ticker.C:
			if !set(esContinuous | esSystemRequired) {
				slog.Warn("Renewing the keep-awake request failed")
			}
		}
	}
}
