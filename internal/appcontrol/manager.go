package appcontrol

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

type AppInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	CanStartStop bool   `json:"can_start_stop"`
}

type StopResult struct {
	ProcessesFound int
	MessagesSent   int
	StillRunning   int
}

type Controller interface {
	Start(path string) error
	Stop(path string) (StopResult, error)
}

type Manager struct {
	mu         sync.RWMutex
	apps       []store.AppConfig
	allowed    map[string]bool
	controller Controller
}

func NewManager(apps []store.AppConfig) *Manager {
	return newManager(apps, newPlatformController())
}

func newManager(apps []store.AppConfig, controller Controller) *Manager {
	return &Manager{
		apps:       append([]store.AppConfig(nil), apps...),
		allowed:    make(map[string]bool),
		controller: controller,
	}
}

func (m *Manager) List() []AppInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	apps := make([]AppInfo, len(m.apps))
	for i, app := range m.apps {
		id := strconv.Itoa(i)
		apps[i] = AppInfo{
			ID:           id,
			Name:         app.Name,
			Path:         app.Path,
			CanStartStop: m.allowed[id],
		}
	}
	return apps
}

func (m *Manager) SetPermission(id string, allowed bool) error {
	if _, err := m.app(id); err != nil {
		return err
	}
	m.mu.Lock()
	m.allowed[id] = allowed
	m.mu.Unlock()
	return nil
}

func (m *Manager) Control(id, action string) (string, error) {
	app, err := m.app(id)
	if err != nil {
		return "", err
	}
	m.mu.RLock()
	allowed := m.allowed[id]
	m.mu.RUnlock()
	if !allowed {
		return "", errors.New("Start/Stop permission is disabled for this app")
	}
	if app.Name == "" {
		return "", errors.New("configured app name cannot be empty")
	}
	path, err := resolveExecutable(app.Path)
	if err != nil {
		return "", err
	}

	switch action {
	case "start":
		if err := m.controller.Start(path); err != nil {
			return "", fmt.Errorf("start %s: %w", app.Name, err)
		}
		return fmt.Sprintf("Start command sent for %s.", app.Name), nil
	case "stop":
		result, err := m.controller.Stop(path)
		if err != nil {
			return "", fmt.Errorf("stop %s: %w", app.Name, err)
		}
		if result.ProcessesFound == 0 {
			return fmt.Sprintf("%s is not running.", app.Name), nil
		}
		return fmt.Sprintf(
			"WM_CLOSE sent to %d window(s) for %s; %d process(es) are still running.",
			result.MessagesSent, app.Name, result.StillRunning,
		), nil
	default:
		return "", fmt.Errorf("unsupported app action %q", action)
	}
}

func (m *Manager) app(id string) (store.AppConfig, error) {
	index, err := strconv.Atoi(id)
	if err != nil || index < 0 {
		return store.AppConfig{}, fmt.Errorf("unknown configured app %q", id)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if index >= len(m.apps) {
		return store.AppConfig{}, fmt.Errorf("unknown configured app %q", id)
	}
	return m.apps[index], nil
}

func resolveExecutable(path string) (string, error) {
	if path == "" {
		return "", errors.New("configured executable path cannot be empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve configured executable path: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("inspect configured executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("configured executable must be a regular file")
	}
	return absolute, nil
}
