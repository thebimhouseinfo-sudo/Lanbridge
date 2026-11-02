package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// PermissionGrant represents a folder grant approved by user
type PermissionGrant struct {
	Path         string   `json:"path"`
	Capabilities []string `json:"capabilities"` // "read", "sync", "execute", "mcp"
	GrantedAt    string   `json:"granted_at"`
}

// AppState represents state.json (app-managed persistent identity and grants)
type AppState struct {
	CertPEM               string            `json:"cert_pem"`
	KeyPEM                string            `json:"key_pem"`
	MyFingerprint         string            `json:"my_fingerprint"`
	PairedPeerFingerprint string            `json:"paired_peer_fingerprint"`
	PendingUnpair         bool              `json:"pending_unpair,omitempty"`
	LastConnectedAddr     string            `json:"last_connected_addr,omitempty"`
	Permissions           []PermissionGrant `json:"permissions,omitempty"`
}

// StateStore handles atomic read/write of state.json
type StateStore struct {
	mu       sync.Mutex
	filePath string
}

// NewStateStore initializes the store at the default OS location
func NewStateStore() (*StateStore, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return nil, err
	}
	return NewStateStoreAt(filepath.Join(dir, "state.json"))
}

// NewStateStoreAt creates a state store at an explicit file path.
func NewStateStoreAt(filePath string) (*StateStore, error) {
	if filePath == "" {
		return nil, errors.New("state file path cannot be empty")
	}
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create config dir: %w", err)
	}

	return &StateStore{
		filePath: filePath,
	}, nil
}

// Load reads and parses state.json
func (s *StateStore) Load() (*AppState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &AppState{Permissions: []PermissionGrant{}}, nil
		}
		return nil, fmt.Errorf("failed to read state.json: %w", err)
	}

	var state AppState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("invalid json in state.json: %w", err)
	}
	if state.Permissions == nil {
		state.Permissions = []PermissionGrant{}
	}
	return &state, nil
}

// Save atomically writes state.json via temp file and rename
func (s *StateStore) Save(state *AppState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state.json: %w", err)
	}

	tempPath := s.filePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp state: %w", err)
	}

	if err := os.Rename(tempPath, s.filePath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("atomic rename failed: %w", err)
	}

	return nil
}

// Unpair clears the paired peer fingerprint and all granted permissions
func (s *StateStore) Unpair() error {
	state, err := s.Load()
	if err != nil {
		return err
	}
	state.PairedPeerFingerprint = ""
	state.PendingUnpair = false
	state.Permissions = []PermissionGrant{}
	return s.Save(state)
}
