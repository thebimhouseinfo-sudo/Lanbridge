package ui

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOpenUIWindowUsesAppModeAndFallsBack(t *testing.T) {
	getenv := func(key string) string {
		if key == "ProgramFiles" {
			return `C:\Program Files`
		}
		return ""
	}
	paths := browserCandidates("windows", getenv)
	if len(paths) != 2 || filepath.Base(paths[0]) != "msedge.exe" || filepath.Base(paths[1]) != "chrome.exe" {
		t.Fatalf("unexpected candidates: %v", paths)
	}
	var calls [][]string
	err := openUIWindow("windows", "http://127.0.0.1/?t=test", getenv, func(string) bool { return true }, func(name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		if name == paths[0] {
			return errors.New("cannot start")
		}
		return nil
	})
	if err != nil || len(calls) != 2 || !reflect.DeepEqual(calls[1], []string{paths[1], "--app=http://127.0.0.1/?t=test", "--window-size=500,900"}) {
		t.Fatalf("err=%v calls=%v", err, calls)
	}
	calls = nil
	err = openUIWindow("windows", "http://localhost/", getenv, func(string) bool { return false }, func(name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	})
	if err != nil || !reflect.DeepEqual(calls, [][]string{{"rundll32", "url.dll,FileProtocolHandler", "http://localhost/"}}) {
		t.Fatalf("err=%v calls=%v", err, calls)
	}
	if openUIWindow("darwin", "secret", getenv, func(string) bool { return false }, func(string, ...string) error { return errors.New("secret") }) == nil {
		t.Fatal("expected failure")
	}
}

func TestMacBrowserCandidates(t *testing.T) {
	paths := browserCandidates("darwin", func(key string) string {
		if key == "HOME" {
			return "/Users/test"
		}
		return ""
	})
	if len(paths) != 4 || filepath.Base(paths[0]) != "Microsoft Edge" || filepath.Base(paths[2]) != "Google Chrome" {
		t.Fatalf("unexpected candidates: %v", paths)
	}
}
