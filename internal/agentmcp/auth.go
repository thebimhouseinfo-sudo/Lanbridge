package agentmcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

// AuthInfo contains local bridge client connection parameters for bridge mcp
type AuthInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

// WriteAuthFile stores the bridge client's localhost endpoint and its API token.
func WriteAuthFile(port int, token string) (*AuthInfo, error) {
	info := &AuthInfo{Port: port, Token: token}
	if err := info.Validate(); err != nil {
		return nil, err
	}
	dir, err := store.GetConfigDir()
	if err != nil {
		return nil, err
	}
	if err := writeAuthFileAt(filepath.Join(dir, "mcp_auth.json"), info); err != nil {
		return nil, err
	}
	return info, nil
}

// RemoveAuthFile deletes the endpoint file only if it still belongs to this client.
func RemoveAuthFile(expected *AuthInfo) error {
	if err := expected.Validate(); err != nil {
		return err
	}
	dir, err := store.GetConfigDir()
	if err != nil {
		return err
	}
	return removeAuthFileAt(filepath.Join(dir, "mcp_auth.json"), expected)
}

// ReadAuthFile reads and validates mcp_auth.json.
func ReadAuthFile() (*AuthInfo, error) {
	dir, err := store.GetConfigDir()
	if err != nil {
		return nil, err
	}
	return readAuthFileAt(filepath.Join(dir, "mcp_auth.json"))
}

func (info *AuthInfo) Validate() error {
	if info == nil || info.Port < 1 || info.Port > 65535 || info.Token == "" {
		return errors.New("invalid mcp_auth.json: port and token are required")
	}
	return nil
}

func writeAuthFileAt(authPath string, info *AuthInfo) error {
	if err := info.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(authPath), 0700); err != nil {
		return fmt.Errorf("failed to create MCP state directory: %w", err)
	}
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to encode mcp_auth.json: %w", err)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(authPath), ".mcp_auth-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary mcp_auth.json: %w", err)
	}
	tempPath := tempFile.Name()
	committed := false
	defer func() {
		_ = tempFile.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := tempFile.Chmod(0600); err != nil {
		return fmt.Errorf("failed to restrict mcp_auth.json permissions: %w", err)
	}
	if _, err := tempFile.Write(data); err != nil {
		return fmt.Errorf("failed to write temporary mcp_auth.json: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temporary mcp_auth.json: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary mcp_auth.json: %w", err)
	}
	if err := os.Rename(tempPath, authPath); err != nil {
		return fmt.Errorf("failed to replace mcp_auth.json: %w", err)
	}
	committed = true
	return nil
}

func readAuthFileAt(authPath string) (*AuthInfo, error) {
	data, err := os.ReadFile(authPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("LAN Bridge trên Mac chưa mở (mcp_auth.json không tồn tại): %w", os.ErrNotExist)
		}
		return nil, fmt.Errorf("failed to read mcp_auth.json: %w", err)
	}

	var info AuthInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("invalid json in mcp_auth.json: %w", err)
	}
	if err := info.Validate(); err != nil {
		return nil, err
	}
	return &info, nil
}

func removeAuthFileAt(authPath string, expected *AuthInfo) error {
	current, err := readAuthFileAt(authPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if current.Port != expected.Port || current.Token != expected.Token {
		return errors.New("mcp_auth.json belongs to a different bridge client")
	}
	if err := os.Remove(authPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove mcp_auth.json: %w", err)
	}
	return nil
}
