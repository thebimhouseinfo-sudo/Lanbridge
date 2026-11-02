package agentmcp

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAuthFileWriteReadAndOwnershipSafeRemoval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "mcp_auth.json")
	first := &AuthInfo{Port: 49152, Token: "first-token"}
	if err := writeAuthFileAt(path, first); err != nil {
		t.Fatal(err)
	}
	loaded, err := readAuthFileAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if *loaded != *first {
		t.Fatalf("read auth info = %+v, want %+v", loaded, first)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0077 != 0 {
			t.Fatalf("auth file permissions are too broad: %o", info.Mode().Perm())
		}
	}

	second := &AuthInfo{Port: 49153, Token: "second-token"}
	if err := writeAuthFileAt(path, second); err != nil {
		t.Fatal(err)
	}
	if err := removeAuthFileAt(path, first); err == nil {
		t.Fatal("removal accepted credentials belonging to a different client")
	}
	if loaded, err := readAuthFileAt(path); err != nil || *loaded != *second {
		t.Fatalf("replacement auth file changed after rejected removal: auth=%+v err=%v", loaded, err)
	}
	if err := removeAuthFileAt(path, second); err != nil {
		t.Fatal(err)
	}
	if err := removeAuthFileAt(path, second); err != nil {
		t.Fatalf("removing an already absent auth file: %v", err)
	}
	if _, err := readAuthFileAt(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reading removed auth file returned %v, want os.ErrNotExist", err)
	}
}

func TestAuthFileRejectsInvalidEndpoint(t *testing.T) {
	for _, info := range []*AuthInfo{
		{Port: 0, Token: "token"},
		{Port: 65536, Token: "token"},
		{Port: 1234},
		nil,
	} {
		if err := writeAuthFileAt(filepath.Join(t.TempDir(), "mcp_auth.json"), info); err == nil {
			t.Fatalf("write accepted invalid auth info: %+v", info)
		}
	}
}
