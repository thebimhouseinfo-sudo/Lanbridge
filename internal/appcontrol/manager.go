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
	CanStart bool       `json:"can_start"`
	CanStop bool        `json:"can_stop"`
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
	allowed    map[string]ActionPermissions
	controller Controller
}

type ActionPermissions struct {Start bool;Stop bool}

func NewManager(apps []store.AppConfig) *Manager {
	return newManager(apps, newPlatformController())
}

func newManager(apps []store.AppConfig, controller Controller) *Manager {
	return &Manager{
		apps:       append([]store.AppConfig(nil), apps...),
		allowed:    make(map[string]ActionPermissions),
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
			CanStartStop: m.allowed[id].Start && m.allowed[id].Stop,
			CanStart: m.allowed[id].Start,
			CanStop: m.allowed[id].Stop,
		}
	}
	return apps
}

func (m *Manager) SetPermission(id string, allowed bool) error {
	if _, err := m.app(id); err != nil {
		return err
	}
	return m.SetActionPermissions(id,allowed,allowed)
}

// SetActionPermissions allows Windows user to grant Start and Stop separately.
func (m *Manager) SetActionPermissions(id string,start,stop bool) error {
 if _,err:=m.app(id);err!=nil{return err}
 m.mu.Lock();m.allowed[id]=ActionPermissions{Start:start,Stop:stop};m.mu.Unlock()
 return nil
}

// ReplaceApps loads the currently opened Windows user-owned profile.
func (m *Manager) ReplaceApps(apps []store.AppConfig,permissions []ActionPermissions) error {
 if len(apps)!=len(permissions){return errors.New("app permissions count mismatch")}
 m.mu.Lock();defer m.mu.Unlock()
 m.apps=append([]store.AppConfig(nil),apps...)
 m.allowed=make(map[string]ActionPermissions,len(apps))
 for i,p:=range permissions{m.allowed[strconv.Itoa(i)]=p}
 return nil
}

func (m *Manager) AddApp(name,path string) error {
 if name==""||path==""{return errors.New("app name and executable path required")}
 m.mu.Lock();defer m.mu.Unlock()
 for _,a:=range m.apps{if a.Name==name && a.Path==path{return errors.New("app already exists")}}
 m.apps=append(m.apps,store.AppConfig{Name:name,Path:path})
 return nil
}

func (m *Manager) RemoveApp(id string) error {
 index,err:=strconv.Atoi(id)
 if err!=nil||index<0{return fmt.Errorf("invalid app ID %q",id)}
 m.mu.Lock();defer m.mu.Unlock()
 if index>=len(m.apps){return fmt.Errorf("unknown app ID %q",id)}
 next:=append([]store.AppConfig(nil),m.apps[:index]...)
 next=append(next,m.apps[index+1:]...)
 grants:=make(map[string]ActionPermissions,len(next))
 for i:=range next{
  old:=i;if i>=index{old=i+1}
  grants[strconv.Itoa(i)]=m.allowed[strconv.Itoa(old)]
 }
 m.apps=next;m.allowed=grants
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
	if (action=="start"&&!allowed.Start)||(action=="stop"&&!allowed.Stop) {
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
