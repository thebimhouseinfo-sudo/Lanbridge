package agentconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/agentlock"
)

const serverName = "lanbridge"

type Provider string

const (
	ProviderClaude Provider = "claude"
	ProviderCodex  Provider = "codex"
)

type EntryStatus string

const (
	StatusAbsent         EntryStatus = "absent"
	StatusManagedActive  EntryStatus = "managed-active"
	StatusManagedDisable EntryStatus = "managed-disabled"
	StatusConflict       EntryStatus = "conflict"
	StatusInvalid        EntryStatus = "invalid"
)

type ProviderState struct {
	Provider Provider    `json:"provider"`
	Status   EntryStatus `json:"status"`
	Message  string      `json:"message,omitempty"`
}

type Snapshot struct {
	Claude ProviderState `json:"claude"`
	Codex  ProviderState `json:"codex"`
}

func (s Snapshot) state(provider Provider) ProviderState {
	switch provider {
	case ProviderClaude:
		return s.Claude
	case ProviderCodex:
		return s.Codex
	default:
		return ProviderState{Provider: provider, Status: StatusInvalid, Message: "Unsupported MCP provider"}
	}
}

type commandRunner func(context.Context, string, ...string) error
type lockAcquirer func() (*agentlock.Lock, error)

type Manager struct {
	bridgeExecutable string
	claudeConfig     string
	codexConfig      string
	home             string
	run              commandRunner
	acquireLock      lockAcquirer
	lookPath         func(string) (string, error)
}

func NewManager(bridgeExecutable string) (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home for agent configs: %w", err)
	}
	canonicalExecutable, err := canonicalPath(bridgeExecutable)
	if err != nil {
		return nil, fmt.Errorf("resolve bridge executable: %w", err)
	}
	return &Manager{
		bridgeExecutable: canonicalExecutable,
		claudeConfig:     filepath.Join(home, ".claude.json"),
		codexConfig:      filepath.Join(home, ".codex", "config.toml"),
		home:             home,
		run:              runCommand,
		acquireLock:      agentlock.Acquire,
		lookPath:         exec.LookPath,
	}, nil
}

func (m *Manager) Inspect() Snapshot {
	return Snapshot{
		Claude: m.inspectClaude(),
		Codex:  m.inspectCodex(),
	}
}

func (m *Manager) Select(ctx context.Context, provider Provider) (result Snapshot, resultErr error) {
	if provider != ProviderClaude && provider != ProviderCodex {
		return m.Inspect(), fmt.Errorf("unsupported MCP provider %q", provider)
	}

	initial := m.Inspect()
	if err := validateSnapshot(initial); err != nil {
		return initial, err
	}
	if isSelected(initial, provider) {
		return initial, nil
	}

	lock, err := m.acquireLock()
	if err != nil {
		return initial, fmt.Errorf("cannot change MCP provider: %w", err)
	}
	defer func() {
		if err := lock.Close(); err != nil {
			result = m.Inspect()
			resultErr = errors.Join(resultErr, fmt.Errorf("release MCP configuration lock: %w", err))
		}
	}()

	initial = m.Inspect()
	if err := validateSnapshot(initial); err != nil {
		return initial, err
	}
	if isSelected(initial, provider) {
		return initial, nil
	}

	commands, err := m.prepareCommands(initial, provider)
	if err != nil {
		return initial, err
	}
	previous := initial

	for _, current := range []Provider{ProviderClaude, ProviderCodex} {
		if current == provider || initial.state(current).Status != StatusManagedActive {
			continue
		}
		if err := m.runProviderCommand(ctx, commands, current, false); err != nil {
			return m.rollback(commands, previous, fmt.Errorf("remove %s Bridge registration: %w", current, err))
		}
		snapshot := m.Inspect()
		if snapshot.state(current).Status != StatusAbsent {
			return m.rollback(commands, previous, fmt.Errorf("remove %s Bridge registration did not take effect", current))
		}
	}

	if initial.state(provider).Status != StatusManagedActive {
		if err := m.runProviderCommand(ctx, commands, provider, true); err != nil {
			return m.rollback(commands, previous, fmt.Errorf("add %s Bridge registration: %w", provider, err))
		}
	}

	current := m.Inspect()
	if !isSelected(current, provider) {
		return m.rollback(commands, previous, fmt.Errorf("provider configuration did not reach the requested state"))
	}
	return current, nil
}

func (m *Manager) inspectClaude() ProviderState {
	state := ProviderState{Provider: ProviderClaude, Status: StatusAbsent}
	data, err := os.ReadFile(m.claudeConfig)
	if errors.Is(err, os.ErrNotExist) {
		return state
	}
	if err != nil {
		state.Status = StatusInvalid
		state.Message = fmt.Sprintf("Cannot read %s: %v", m.claudeConfig, err)
		return state
	}
	var config struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		state.Status = StatusInvalid
		state.Message = fmt.Sprintf("Invalid Claude Code config: %v", err)
		return state
	}
	entry, ok := config.MCPServers[serverName]
	if !ok {
		return state
	}
	var server struct {
		Type    string   `json:"type"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(entry, &server); err != nil {
		state.Status = StatusInvalid
		state.Message = fmt.Sprintf("Invalid Claude Code %s entry: %v", serverName, err)
		return state
	}
	if server.Type != "" && server.Type != "stdio" {
		state.Status = StatusConflict
		state.Message = "The Claude Code lanbridge entry is not a stdio server"
		return state
	}
	if !m.ownsEntry(server.Command, server.Args) {
		state.Status = StatusConflict
		state.Message = "The Claude Code lanbridge entry does not point to this Bridge executable"
		return state
	}
	state.Status = StatusManagedActive
	return state
}

func (m *Manager) inspectCodex() ProviderState {
	state := ProviderState{Provider: ProviderCodex, Status: StatusAbsent}
	data, err := os.ReadFile(m.codexConfig)
	if errors.Is(err, os.ErrNotExist) {
		return state
	}
	if err != nil {
		state.Status = StatusInvalid
		state.Message = fmt.Sprintf("Cannot read %s: %v", m.codexConfig, err)
		return state
	}
	var config map[string]any
	if err := toml.Unmarshal(data, &config); err != nil {
		state.Status = StatusInvalid
		state.Message = fmt.Sprintf("Invalid Codex config: %v", err)
		return state
	}
	rawServers, exists := config["mcp_servers"]
	if !exists {
		return state
	}
	servers, ok := rawServers.(map[string]any)
	if !ok {
		state.Status = StatusInvalid
		state.Message = "Codex mcp_servers value is not a table"
		return state
	}
	rawEntry, ok := servers[serverName]
	if !ok {
		return state
	}
	entry, ok := rawEntry.(map[string]any)
	if !ok {
		state.Status = StatusInvalid
		state.Message = fmt.Sprintf("Invalid Codex %s entry", serverName)
		return state
	}
	command, commandOK := entry["command"].(string)
	args, argsOK := stringSlice(entry["args"])
	if !commandOK || !argsOK {
		state.Status = StatusConflict
		state.Message = "The Codex lanbridge entry is not a Bridge stdio command"
		return state
	}
	if !m.ownsEntry(command, args) {
		state.Status = StatusConflict
		state.Message = "The Codex lanbridge entry does not point to this Bridge executable"
		return state
	}
	if enabled, exists := entry["enabled"]; exists {
		value, ok := enabled.(bool)
		if !ok {
			state.Status = StatusInvalid
			state.Message = "The Codex lanbridge enabled value is not a boolean"
			return state
		}
		if !value {
			state.Status = StatusManagedDisable
			state.Message = "The Codex lanbridge entry is disabled in config.toml"
			return state
		}
	}
	state.Status = StatusManagedActive
	return state
}

func (m *Manager) ownsEntry(command string, args []string) bool {
	if len(args) != 1 || args[0] != "mcp" {
		return false
	}
	registeredPath, err := canonicalPath(command)
	if err != nil {
		return false
	}
	if runtime.GOOS == "darwin" {
		return strings.EqualFold(registeredPath, m.bridgeExecutable)
	}
	return registeredPath == m.bridgeExecutable
}

func validateSnapshot(snapshot Snapshot) error {
	for _, state := range []ProviderState{snapshot.Claude, snapshot.Codex} {
		switch state.Status {
		case StatusAbsent, StatusManagedActive:
			continue
		case StatusManagedDisable:
			return fmt.Errorf("%s provider has a disabled Bridge entry: %s; remove it or enable it in the agent config before switching", state.Provider, state.Message)
		case StatusConflict, StatusInvalid:
			return fmt.Errorf("%s provider config cannot be changed safely: %s", state.Provider, state.Message)
		default:
			return fmt.Errorf("%s provider has an unknown config state", state.Provider)
		}
	}
	return nil
}

func isSelected(snapshot Snapshot, provider Provider) bool {
	other := ProviderClaude
	if provider == ProviderClaude {
		other = ProviderCodex
	}
	return snapshot.state(provider).Status == StatusManagedActive &&
		snapshot.state(other).Status == StatusAbsent
}

type providerCommands struct {
	claude string
	codex  string
}

func (m *Manager) prepareCommands(snapshot Snapshot, target Provider) (providerCommands, error) {
	commands := providerCommands{}
	needsClaude := target == ProviderClaude && snapshot.Claude.Status != StatusManagedActive ||
		target != ProviderClaude && snapshot.Claude.Status == StatusManagedActive
	needsCodex := target == ProviderCodex && snapshot.Codex.Status != StatusManagedActive ||
		target != ProviderCodex && snapshot.Codex.Status == StatusManagedActive
	if needsClaude {
		path, err := m.findCLI("claude")
		if err != nil {
			return commands, fmt.Errorf("Claude Code CLI is required to manage its MCP config: %w", err)
		}
		commands.claude = path
	}
	if needsCodex {
		path, err := m.findCLI("codex")
		if err != nil {
			return commands, fmt.Errorf("Codex CLI is required to manage its MCP config: %w", err)
		}
		commands.codex = path
	}
	return commands, nil
}

func (m *Manager) findCLI(name string) (string, error) {
	if path, err := m.lookPath(name); err == nil {
		return path, nil
	}
	for _, dir := range []string{
		filepath.Join(m.home, ".local", "bin"),
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/opt/local/bin",
		"/usr/bin",
	} {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("%q was not found on PATH or in standard install directories", name)
}

func (m *Manager) runProviderCommand(ctx context.Context, commands providerCommands, provider Provider, add bool) error {
	var executable string
	var args []string
	switch provider {
	case ProviderClaude:
		executable = commands.claude
		if add {
			args = []string{"mcp", "add", "--scope", "user", "--transport", "stdio", serverName, "--", m.bridgeExecutable, "mcp"}
		} else {
			args = []string{"mcp", "remove", "--scope", "user", serverName}
		}
	case ProviderCodex:
		executable = commands.codex
		if add {
			args = []string{"mcp", "add", serverName, "--", m.bridgeExecutable, "mcp"}
		} else {
			args = []string{"mcp", "remove", serverName}
		}
	default:
		return fmt.Errorf("unsupported MCP provider %q", provider)
	}
	if executable == "" {
		return fmt.Errorf("CLI executable for %s is unavailable", provider)
	}
	return m.run(ctx, executable, args...)
}

func (m *Manager) rollback(commands providerCommands, previous Snapshot, cause error) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	current := m.Inspect()
	for _, provider := range []Provider{ProviderClaude, ProviderCodex} {
		wanted := previous.state(provider).Status
		got := current.state(provider).Status
		if wanted == got {
			continue
		}
		if wanted == StatusManagedActive && got == StatusAbsent {
			if err := m.runProviderCommand(ctx, commands, provider, true); err != nil {
				current = m.Inspect()
				return current, fmt.Errorf("%w; rollback could not restore %s: %v", cause, provider, err)
			}
		} else if wanted == StatusAbsent && got == StatusManagedActive {
			if err := m.runProviderCommand(ctx, commands, provider, false); err != nil {
				current = m.Inspect()
				return current, fmt.Errorf("%w; rollback could not remove %s: %v", cause, provider, err)
			}
		} else {
			return current, fmt.Errorf("%w; rollback stopped because %s changed to unexpected state %q", cause, provider, got)
		}
		current = m.Inspect()
	}
	for _, provider := range []Provider{ProviderClaude, ProviderCodex} {
		if current.state(provider).Status != previous.state(provider).Status {
			return current, fmt.Errorf("%w; rollback did not restore %s state (got %q, wanted %q)", cause, provider, current.state(provider).Status, previous.state(provider).Status)
		}
	}
	return current, cause
}

func runCommand(ctx context.Context, executable string, args ...string) error {
	commandCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, executable, args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(output))
	if detail != "" {
		return fmt.Errorf("%w: %s", err, detail)
	}
	return err
}

func canonicalPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("empty executable path")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		absolute = resolved
	}
	return filepath.Clean(absolute), nil
}

func stringSlice(value any) ([]string, bool) {
	switch args := value.(type) {
	case []string:
		return args, true
	case []any:
		result := make([]string, len(args))
		for i, arg := range args {
			text, ok := arg.(string)
			if !ok {
				return nil, false
			}
			result[i] = text
		}
		return result, true
	default:
		return nil, false
	}
}
