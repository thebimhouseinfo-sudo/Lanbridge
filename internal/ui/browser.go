package ui

import (
	"errors"
	"os"
	"path/filepath"
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
