//go:build windows

package executor

import (
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobHandleStore keeps Job Object handles alive until the process finishes.
var jobHandleStore struct {
	mu      sync.Mutex
	handles []windows.Handle
}

// applyPlatformAttrs sets CREATE_NO_WINDOW so no console window appears.
// The Job Object is created separately and attached after cmd.Start() via AttachJobObject.
func applyPlatformAttrs(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | createNoWindow,
		HideWindow:    true,
	}
}

// AttachJobObject creates a Windows Job Object with KILL_ON_JOB_CLOSE and assigns
// the process (identified by its PID) to it. Call this immediately after cmd.Start().
// The returned cleanup function must be called (defer it) when the process exits so
// the handle is closed and child processes are terminated.
func AttachJobObject(pid int) (cleanup func(), err error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return func() {}, err
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	size := uint32(unsafe.Sizeof(info))
	if _, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		size,
	); err != nil {
		windows.CloseHandle(job)
		return func() {}, err
	}

	proc, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(pid),
	)
	if err != nil {
		windows.CloseHandle(job)
		return func() {}, err
	}
	defer windows.CloseHandle(proc)

	if err = windows.AssignProcessToJobObject(job, proc); err != nil {
		windows.CloseHandle(job)
		return func() {}, err
	}

	// Keep the handle alive so KILL_ON_JOB_CLOSE fires only when we want it.
	jobHandleStore.mu.Lock()
	jobHandleStore.handles = append(jobHandleStore.handles, job)
	jobHandleStore.mu.Unlock()

	cleanup = func() {
		windows.CloseHandle(job)
		jobHandleStore.mu.Lock()
		hs := jobHandleStore.handles
		for i, h := range hs {
			if h == job {
				jobHandleStore.handles = append(hs[:i], hs[i+1:]...)
				break
			}
		}
		jobHandleStore.mu.Unlock()
	}
	return cleanup, nil
}
