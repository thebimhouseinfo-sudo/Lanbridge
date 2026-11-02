package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ProjectConfig describes a repository configured on Windows Host
type ProjectConfig struct {
	Name              string              `json:"name"`
	Root              string              `json:"root"`
	RelatedDirs       map[string]string   `json:"related_dirs,omitempty"`
	Commands          map[string][]string `json:"commands,omitempty"`
	CommandTimeoutMin int                 `json:"command_timeout_min,omitempty"`
	MCP               *MCPConfig          `json:"mcp,omitempty"`
}

// MCPConfig describes how to connect to project's local MCP server
type MCPConfig struct {
	Transport string   `json:"transport"` // "stdio" or "http"
	Command   []string `json:"command,omitempty"`
	URL       string   `json:"url,omitempty"`
}

// AppConfig describes approved software that can be started/stopped
type AppConfig struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// HostConfig represents %APPDATA%\LanBridge\config.json
type HostConfig struct {
	Port      int             `json:"port"`
	MaxSyncMB int             `json:"max_sync_mb"`
	Projects  []ProjectConfig `json:"projects"`
	Apps      []AppConfig     `json:"apps"`
}

// DefaultHostConfig returns initial defaults
func DefaultHostConfig() *HostConfig {
	return &HostConfig{
		Port:      54823,
		MaxSyncMB: 5,
		Projects:  []ProjectConfig{},
		Apps:      []AppConfig{},
	}
}

// GetConfigDir returns the OS-specific directory for LanBridge
func GetConfigDir() (string, error) {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", errors.New("%APPDATA% not set")
		}
		return filepath.Join(appData, "LanBridge"), nil
	}

	// macOS / Unix
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "LanBridge"), nil
	}
	return filepath.Join(home, ".config", "lanbridge"), nil
}

// LoadHostConfig reads %APPDATA%\LanBridge\config.json
func LoadHostConfig() (*HostConfig, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(dir, "config.json")

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Return default config if file does not exist yet
			return DefaultHostConfig(), nil
		}
		return nil, fmt.Errorf("failed to read config.json: %w", err)
	}

	cfg := DefaultHostConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("invalid json in config.json: %w", err)
	}

	if cfg.Port <= 0 {
		cfg.Port = 54823
	}
	if cfg.MaxSyncMB <= 0 {
		cfg.MaxSyncMB = 5
	}

	return cfg, nil
}
