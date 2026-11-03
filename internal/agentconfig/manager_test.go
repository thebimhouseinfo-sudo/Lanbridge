package agentconfig

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/agentlock"
)

func TestInspectProviderConfigs(t *testing.T) {
	manager, fixture := newTestManager(t)
	writeClaudeConfig(t, manager.claudeConfig, map[string]any{
		"other":     map[string]any{"command": "node", "args": []string{"other-server"}},
		"lanbridge": claudeEntry(manager.bridgeExecutable),
	})
	writeCodexConfig(t, manager.codexConfig, map[string]any{
		"other": map[string]any{"command": "node", "args": []string{"other-server"}},
		"lanbridge": map[string]any{
			"command": manager.bridgeExecutable,
			"args":    []string{"mcp"},
			"enabled": false,
		},
	})

	snapshot := manager.Inspect()
	if snapshot.Claude.Status != StatusManagedActive {
		t.Fatalf("Claude status = %q, want %q", snapshot.Claude.Status, StatusManagedActive)
	}
	if snapshot.Codex.Status != StatusManagedDisable {
		t.Fatalf("Codex status = %q, want %q", snapshot.Codex.Status, StatusManagedDisable)
	}
	if fixture.calls != 0 {
		t.Fatalf("Inspect ran CLI %d times, want 0", fixture.calls)
	}
}

func TestSelectSwitchesProvidersAndPreservesOtherServers(t *testing.T) {
	manager, fixture := newTestManager(t)
	writeClaudeConfig(t, manager.claudeConfig, map[string]any{
		"other":     map[string]any{"command": "node", "args": []string{"keep-claude"}},
		"lanbridge": claudeEntry(manager.bridgeExecutable),
	})
	writeCodexConfig(t, manager.codexConfig, map[string]any{
		"other": map[string]any{"command": "node", "args": []string{"keep-codex"}},
	})

	snapshot, err := manager.Select(context.Background(), ProviderCodex)
	if err != nil {
		t.Fatalf("Select(Codex): %v", err)
	}
	if !isSelected(snapshot, ProviderCodex) {
		t.Fatalf("snapshot after selecting Codex = %#v", snapshot)
	}
	if fixture.calls != 2 {
		t.Fatalf("CLI call count = %d, want remove+add", fixture.calls)
	}
	assertClaudeOtherServer(t, manager.claudeConfig, "keep-claude")
	assertCodexOtherServer(t, manager.codexConfig, "keep-codex")

	snapshot, err = manager.Select(context.Background(), ProviderClaude)
	if err != nil {
		t.Fatalf("Select(Claude): %v", err)
	}
	if !isSelected(snapshot, ProviderClaude) {
		t.Fatalf("snapshot after selecting Claude = %#v", snapshot)
	}
	assertClaudeOtherServer(t, manager.claudeConfig, "keep-claude")
	assertCodexOtherServer(t, manager.codexConfig, "keep-codex")
}

func TestSelectReconcilesDualActiveRegistration(t *testing.T) {
	manager, _ := newTestManager(t)
	writeClaudeConfig(t, manager.claudeConfig, map[string]any{
		"lanbridge": claudeEntry(manager.bridgeExecutable),
	})
	writeCodexConfig(t, manager.codexConfig, map[string]any{
		"lanbridge": codexEntry(manager.bridgeExecutable, true),
	})

	snapshot, err := manager.Select(context.Background(), ProviderCodex)
	if err != nil {
		t.Fatalf("Select(Codex) from dual registration: %v", err)
	}
	if !isSelected(snapshot, ProviderCodex) {
		t.Fatalf("snapshot after reconciliation = %#v", snapshot)
	}
}

func TestSelectRejectsDisabledEntriesWithoutChangingConfig(t *testing.T) {
	for _, target := range []Provider{ProviderClaude, ProviderCodex} {
		t.Run(string(target), func(t *testing.T) {
			manager, fixture := newTestManager(t)
			writeClaudeConfig(t, manager.claudeConfig, map[string]any{
				"other":     map[string]any{"command": "node", "args": []string{"keep"}},
				"lanbridge": claudeEntry(manager.bridgeExecutable),
			})
			writeCodexConfig(t, manager.codexConfig, map[string]any{
				"lanbridge": codexEntry(manager.bridgeExecutable, false),
			})
			claudeBefore := readFile(t, manager.claudeConfig)
			codexBefore := readFile(t, manager.codexConfig)

			_, err := manager.Select(context.Background(), target)
			if err == nil || !strings.Contains(err.Error(), "disabled") {
				t.Fatalf("Select error = %v, want explicit disabled-entry error", err)
			}
			if fixture.calls != 0 {
				t.Fatalf("Select ran CLI %d times, want 0", fixture.calls)
			}
			assertBytesEqual(t, readFile(t, manager.claudeConfig), claudeBefore)
			assertBytesEqual(t, readFile(t, manager.codexConfig), codexBefore)
		})
	}
}

func TestSelectRejectsConflictAndMalformedConfigsWithoutChanges(t *testing.T) {
	tests := []struct {
		name        string
		writeConfig func(*testing.T, *Manager)
	}{
		{
			name: "foreign Claude entry",
			writeConfig: func(t *testing.T, manager *Manager) {
				writeClaudeConfig(t, manager.claudeConfig, map[string]any{
					"lanbridge": map[string]any{"command": "node", "args": []string{"other"}},
				})
			},
		},
		{
			name: "malformed Codex TOML",
			writeConfig: func(t *testing.T, manager *Manager) {
				if err := os.MkdirAll(filepath.Dir(manager.codexConfig), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(manager.codexConfig, []byte("[mcp_servers\n"), 0600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, fixture := newTestManager(t)
			test.writeConfig(t, manager)
			beforeClaude := readOptionalFile(t, manager.claudeConfig)
			beforeCodex := readOptionalFile(t, manager.codexConfig)

			_, err := manager.Select(context.Background(), ProviderCodex)
			if err == nil {
				t.Fatal("Select unexpectedly succeeded")
			}
			if fixture.calls != 0 {
				t.Fatalf("Select ran CLI %d times, want 0", fixture.calls)
			}
			assertBytesEqual(t, readOptionalFile(t, manager.claudeConfig), beforeClaude)
			assertBytesEqual(t, readOptionalFile(t, manager.codexConfig), beforeCodex)
		})
	}
}

func TestSelectRollsBackWhenAddingTargetFails(t *testing.T) {
	manager, fixture := newTestManager(t)
	writeClaudeConfig(t, manager.claudeConfig, map[string]any{
		"lanbridge": claudeEntry(manager.bridgeExecutable),
	})
	fixture.failNext["codex:add"] = errors.New("simulated add failure")

	snapshot, err := manager.Select(context.Background(), ProviderCodex)
	if err == nil || !strings.Contains(err.Error(), "simulated add failure") {
		t.Fatalf("Select error = %v, want add failure", err)
	}
	if snapshot.Claude.Status != StatusManagedActive || snapshot.Codex.Status != StatusAbsent {
		t.Fatalf("snapshot after rollback = %#v", snapshot)
	}
}

func TestSelectReportsRollbackFailureAndActualSnapshot(t *testing.T) {
	manager, fixture := newTestManager(t)
	writeClaudeConfig(t, manager.claudeConfig, map[string]any{
		"lanbridge": claudeEntry(manager.bridgeExecutable),
	})
	fixture.failNext["codex:add"] = errors.New("simulated add failure")
	fixture.failNext["claude:add"] = errors.New("simulated rollback failure")

	snapshot, err := manager.Select(context.Background(), ProviderCodex)
	if err == nil || !strings.Contains(err.Error(), "rollback could not restore claude") {
		t.Fatalf("Select error = %v, want rollback failure", err)
	}
	if snapshot.Claude.Status != StatusAbsent || snapshot.Codex.Status != StatusAbsent {
		t.Fatalf("snapshot should report actual post-rollback state: %#v", snapshot)
	}
}

func TestSelectFailsWhenRuntimeLockIsBusy(t *testing.T) {
	manager, fixture := newTestManager(t)
	lock, err := agentlock.AcquireAt(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()

	_, err = manager.Select(context.Background(), ProviderClaude)
	if !errors.Is(err, agentlock.ErrBusy) {
		t.Fatalf("Select error = %v, want ErrBusy", err)
	}
	if fixture.calls != 0 {
		t.Fatalf("Select ran CLI %d times while locked, want 0", fixture.calls)
	}
}

func TestSelectAlreadyActiveIsNoOp(t *testing.T) {
	manager, fixture := newTestManager(t)
	writeClaudeConfig(t, manager.claudeConfig, map[string]any{
		"lanbridge": claudeEntry(manager.bridgeExecutable),
	})

	snapshot, err := manager.Select(context.Background(), ProviderClaude)
	if err != nil {
		t.Fatalf("Select(Claude): %v", err)
	}
	if !isSelected(snapshot, ProviderClaude) || fixture.calls != 0 {
		t.Fatalf("Select should be a no-op, snapshot=%#v calls=%d", snapshot, fixture.calls)
	}
}

type fakeCLI struct {
	manager  *Manager
	calls    int
	lockPath string
	failNext map[string]error
}

func newTestManager(t *testing.T) (*Manager, *fakeCLI) {
	t.Helper()
	dir := t.TempDir()
	executable := filepath.Join(dir, "bridge")
	if err := os.WriteFile(executable, []byte("bridge"), 0700); err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalPath(executable)
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{
		bridgeExecutable: canonical,
		claudeConfig:     filepath.Join(dir, "home", ".claude.json"),
		codexConfig:      filepath.Join(dir, "home", ".codex", "config.toml"),
		home:             filepath.Join(dir, "home"),
		lookPath: func(name string) (string, error) {
			if name != "claude" && name != "codex" {
				return "", os.ErrNotExist
			}
			return name, nil
		},
		acquireLock: func() (*agentlock.Lock, error) {
			return agentlock.AcquireAt(filepath.Join(dir, "bridge-state", "mcp.lock"))
		},
	}
	fixture := &fakeCLI{
		manager:  manager,
		lockPath: filepath.Join(dir, "bridge-state", "mcp.lock"),
		failNext: make(map[string]error),
	}
	manager.run = fixture.run
	return manager, fixture
}

func (f *fakeCLI) run(_ context.Context, executable string, args ...string) error {
	f.calls++
	if len(args) < 3 || args[0] != "mcp" {
		return errors.New("unexpected CLI arguments")
	}
	action := args[1]
	key := string(Provider(executable)) + ":" + action
	if err := f.failNext[key]; err != nil {
		delete(f.failNext, key)
		return err
	}
	switch Provider(executable) {
	case ProviderClaude:
		return f.mutateClaude(action)
	case ProviderCodex:
		return f.mutateCodex(action)
	default:
		return errors.New("unexpected CLI executable")
	}
}

func (f *fakeCLI) mutateClaude(action string) error {
	config := readJSONMap(f.manager.claudeConfig)
	servers := jsonMap(config["mcpServers"])
	switch action {
	case "add":
		servers[serverName] = claudeEntry(f.manager.bridgeExecutable)
	case "remove":
		delete(servers, serverName)
	default:
		return errors.New("unexpected Claude action")
	}
	config["mcpServers"] = servers
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.manager.claudeConfig), 0700); err != nil {
		return err
	}
	return os.WriteFile(f.manager.claudeConfig, data, 0600)
}

func (f *fakeCLI) mutateCodex(action string) error {
	config := readTOMLMap(f.manager.codexConfig)
	servers := mapMap(config["mcp_servers"])
	switch action {
	case "add":
		servers[serverName] = codexEntry(f.manager.bridgeExecutable, true)
	case "remove":
		delete(servers, serverName)
	default:
		return errors.New("unexpected Codex action")
	}
	config["mcp_servers"] = servers
	data, err := toml.Marshal(config)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.manager.codexConfig), 0700); err != nil {
		return err
	}
	return os.WriteFile(f.manager.codexConfig, data, 0600)
}

func claudeEntry(command string) map[string]any {
	return map[string]any{"type": "stdio", "command": command, "args": []string{"mcp"}}
}

func codexEntry(command string, enabled bool) map[string]any {
	return map[string]any{"command": command, "args": []string{"mcp"}, "enabled": enabled}
}

func writeClaudeConfig(t *testing.T, path string, servers map[string]any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"mcpServers": servers, "otherSetting": "preserve"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func writeCodexConfig(t *testing.T, path string, servers map[string]any) {
	t.Helper()
	data, err := toml.Marshal(map[string]any{
		"model":       "keep-model",
		"mcp_servers": servers,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func assertClaudeOtherServer(t *testing.T, path, want string) {
	t.Helper()
	config := readJSONMap(path)
	servers := jsonMap(config["mcpServers"])
	entry := jsonMap(servers["other"])
	args, ok := stringSlice(entry["args"])
	if entry["command"] != "node" || !ok || len(args) != 1 || args[0] != want {
		t.Fatalf("Claude other MCP entry was not preserved: %#v", entry)
	}
}

func assertCodexOtherServer(t *testing.T, path, want string) {
	t.Helper()
	config := readTOMLMap(path)
	servers := mapMap(config["mcp_servers"])
	entry := mapMap(servers["other"])
	args, ok := stringSlice(entry["args"])
	if entry["command"] != "node" || !ok || len(args) != 1 || args[0] != want {
		t.Fatalf("Codex other MCP entry was not preserved: %#v", entry)
	}
}

func readJSONMap(path string) map[string]any {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]any)
	}
	if err != nil {
		panic(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		panic(err)
	}
	return result
}

func readTOMLMap(path string) map[string]any {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]any)
	}
	if err != nil {
		panic(err)
	}
	var result map[string]any
	if err := toml.Unmarshal(data, &result); err != nil {
		panic(err)
	}
	return result
}

func jsonMap(value any) map[string]any {
	result, _ := value.(map[string]any)
	if result == nil {
		return make(map[string]any)
	}
	return result
}

func mapMap(value any) map[string]any {
	result, _ := value.(map[string]any)
	if result == nil {
		return make(map[string]any)
	}
	return result
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readOptionalFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertBytesEqual(t *testing.T, got, want []byte) {
	t.Helper()
	if string(got) != string(want) {
		t.Fatalf("config bytes changed:\n got: %q\nwant: %q", got, want)
	}
}
