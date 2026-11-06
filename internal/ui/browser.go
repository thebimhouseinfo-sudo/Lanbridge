package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func browserFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func browserCandidates(platform string, getenv func(string) string) []string {
	var paths []string
	switch platform {
	case "windows":
		for _, browser := range [][]string{{"Microsoft", "Edge", "Application", "msedge.exe"}, {"Google", "Chrome", "Application", "chrome.exe"}} {
			for _, key := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
				if root := getenv(key); root != "" {
					paths = append(paths, filepath.Join(append([]string{root}, browser...)...))
				}
			}
		}
	case "darwin":
		for _, app := range []struct{ bundle, binary string }{{"Microsoft Edge.app", "Microsoft Edge"}, {"Google Chrome.app", "Google Chrome"}} {
			for _, root := range []string{"/Applications", filepath.Join(getenv("HOME"), "Applications")} {
				paths = append(paths, filepath.Join(root, app.bundle, "Contents", "MacOS", app.binary))
			}
		}
	}
	return paths
}

func openUIWindow(platform, url string, getenv func(string) string, exists func(string) bool, start func(string, ...string) error) error {
	for _, path := range browserCandidates(platform, getenv) {
		if platform == "darwin" && exists(path) {
			// A dedicated profile forces a new browser process. Passing --app to
			// an already running Chrome/Edge (e.g. started without a window)
			// forwards the request and may never show a window.
			bundle := filepath.Dir(filepath.Dir(filepath.Dir(path)))
			profile := filepath.Join(getenv("HOME"), "Library", "Application Support", "LanBridge", "ui-profile")
			if start("open", "-n", bundle, "--args", "--app="+url, "--window-size=500,900",
				"--user-data-dir="+profile, "--no-first-run", "--no-default-browser-check") == nil {
				return nil
			}
			continue
		}
		if exists(path) && start(path, "--app="+url, "--window-size=500,900") == nil {
			return nil
		}
	}
	switch platform {
	case "windows":
		if start("rundll32", "url.dll,FileProtocolHandler", url) == nil {
			return nil
		}
	case "darwin":
		if start("open", url) == nil {
			return nil
		}
	default:
		if start("xdg-open", url) == nil {
			return nil
		}
	}
	return errors.New("could not start a browser")
}

// OpenURL opens the UI URL in the preferred app window or default browser.
func OpenURL(url string) error {
	return openUIWindow(runtime.GOOS, url, os.Getenv, browserFileExists, func(name string, args ...string) error {
		return exec.Command(name, args...).Start()
	})
}
