//go:build windows

package appcontrol

import (
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	th32csSnapProcess   = 0x00000002
	gwOwner             = 4
	wmClose             = 0x0010
	smtoAbortIfHung     = 0x0002
	processStillActive  = 259
	processQueryLimited = windows.PROCESS_QUERY_LIMITED_INFORMATION
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindow                = user32.NewProc("GetWindow")
	procSendMessageTimeout       = user32.NewProc("SendMessageTimeoutW")
)

type platformController struct{}

func newPlatformController() Controller {
	return platformController{}
}

func (platformController) Start(path string) error {
	command := exec.Command(path)
	command.Dir = filepath.Dir(path)
	if err := command.Start(); err != nil {
		return err
	}
	go func() {
		if err := command.Wait(); err != nil {
			slog.Warn("Started application exited with an error", "path", path, "error", err)
		}
	}()
	return nil
}

func (platformController) Stop(path string) (StopResult, error) {
	pids, err := processIDsForPath(path)
	if err != nil {
		return StopResult{}, err
	}
	result := StopResult{ProcessesFound: len(pids)}
	if len(pids) == 0 {
		return result, nil
	}
	windowsByPID, err := mainWindowsByProcess()
	if err != nil {
		return result, err
	}
	for _, pid := range pids {
		for _, hwnd := range windowsByPID[pid] {
			var messageResult uintptr
			sent, _, callErr := procSendMessageTimeout.Call(
				hwnd, wmClose, 0, 0, smtoAbortIfHung, 1000, uintptr(unsafe.Pointer(&messageResult)),
			)
			if sent == 0 {
				if callErr != syscall.Errno(0) {
					slog.Warn("Could not send WM_CLOSE to application window", "pid", pid, "error", callErr)
				}
				continue
			}
			result.MessagesSent++
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		running := 0
		for _, pid := range pids {
			if processIsRunning(pid) {
				running++
			}
		}
		if running == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, pid := range pids {
		if processIsRunning(pid) {
			result.StillRunning++
		}
	}
	return result, nil
}

func processIDsForPath(target string) ([]uint32, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(th32csSnapProcess, 0)
	if err != nil {
		return nil, fmt.Errorf("enumerate processes: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, fmt.Errorf("read process list: %w", err)
	}
	var matching []uint32
	for {
		if strings.EqualFold(filepath.Base(windows.UTF16ToString(entry.ExeFile[:])), filepath.Base(target)) {
			imagePath, pathErr := processImagePath(entry.ProcessID)
			if pathErr == nil && strings.EqualFold(filepath.Clean(imagePath), filepath.Clean(target)) {
				matching = append(matching, entry.ProcessID)
			}
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, fmt.Errorf("continue process enumeration: %w", err)
		}
	}
	return matching, nil
}

func processImagePath(pid uint32) (string, error) {
	process, err := windows.OpenProcess(processQueryLimited, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(process)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

func mainWindowsByProcess() (map[uint32][]uintptr, error) {
	windowsByPID := make(map[uint32][]uintptr)
	callback := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var pid uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		owner, _, _ := procGetWindow.Call(hwnd, gwOwner)
		if pid != 0 && visible != 0 && owner == 0 {
			windowsByPID[pid] = append(windowsByPID[pid], hwnd)
		}
		return 1
	})
	ok, _, callErr := procEnumWindows.Call(callback, 0)
	if ok == 0 {
		if callErr == syscall.Errno(0) {
			callErr = errors.New("EnumWindows returned failure")
		}
		return nil, fmt.Errorf("enumerate application windows: %w", callErr)
	}
	return windowsByPID, nil
}

func processIsRunning(pid uint32) bool {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return !errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer windows.CloseHandle(process)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(process, &exitCode); err != nil {
		return true
	}
	return exitCode == processStillActive
}
