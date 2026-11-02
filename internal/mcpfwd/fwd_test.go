package mcpfwd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestManagerInitializesSessionAndForwardsTool(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LANBRIDGE_MCPFWD_HELPER", "server")
	t.Setenv("LANBRIDGE_MCPFWD_ROOT_FILE", filepath.Join(root, "cwd.txt"))
	manager := NewManager(nil)
	session, err := manager.Session("CAI", root, "stdio", helperCommand())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.CloseProject("CAI")

	tools := session.Tools()
	if len(tools) != 2 || tools[0].Name != "draw_line" {
		t.Fatalf("session tools = %+v", tools)
	}
	cwd, err := os.ReadFile(filepath.Join(root, "cwd.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(cwd) != root {
		t.Fatalf("MCP server working directory = %q, want %q", cwd, root)
	}

	allTools, warnings := manager.AllTools()
	if len(allTools) != 1 || allTools[0].Name != "CAI__draw_line" {
		t.Fatalf("qualified tools = %+v", allTools)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "64-character") {
		t.Fatalf("tool name warnings = %v", warnings)
	}

	result, err := manager.CallTool("CAI__draw_line", map[string]any{"length": 12})
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(result, &response); err != nil {
		t.Fatal(err)
	}
	content, ok := response["content"].([]any)
	if !ok || len(content) != 1 || content[0].(map[string]any)["text"] != "drawn" {
		t.Fatalf("forwarded tool result = %#v", response)
	}
}

func TestManagerReopensExitedServer(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LANBRIDGE_MCPFWD_HELPER", "restart_once")
	t.Setenv("LANBRIDGE_MCPFWD_ROOT_FILE", filepath.Join(root, "cwd.txt"))
	t.Setenv("LANBRIDGE_MCPFWD_COUNT_FILE", filepath.Join(root, "count.txt"))
	manager := NewManager(nil)

	first, err := manager.Session("CAI", root, "stdio", helperCommand())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-first.stream.done:
	case <-time.After(5 * time.Second):
		t.Fatal("first MCP process did not exit")
	}

	second, err := manager.Session("CAI", root, "stdio", helperCommand())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.CloseProject("CAI")
	if second == first {
		t.Fatal("manager returned the exited MCP session instead of reopening it")
	}
	data, err := os.ReadFile(filepath.Join(root, "count.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if count, err := strconv.Atoi(string(data)); err != nil || count != 2 {
		t.Fatalf("MCP server launch count = %q, want 2 (err=%v)", data, err)
	}
}

func TestMCPServerHelper(t *testing.T) {
	mode := os.Getenv("LANBRIDGE_MCPFWD_HELPER")
	if mode == "" {
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     *int64         `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			return
		}
		if request.ID == nil {
			continue
		}

		var result any = map[string]any{}
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05"}
		case "tools/list":
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(os.Getenv("LANBRIDGE_MCPFWD_ROOT_FILE"), []byte(cwd), 0600); err != nil {
				t.Fatal(err)
			}
			result = map[string]any{
				"tools": []map[string]any{
					{"name": "draw_line", "description": "Draw", "inputSchema": map[string]any{"type": "object"}},
					{"name": strings.Repeat("x", 60), "description": "Too long", "inputSchema": map[string]any{"type": "object"}},
				},
			}
			if mode == "restart_once" {
				countPath := os.Getenv("LANBRIDGE_MCPFWD_COUNT_FILE")
				count := 0
				data, err := os.ReadFile(countPath)
				if err == nil {
					count, _ = strconv.Atoi(string(data))
				}
				count++
				if err := os.WriteFile(countPath, []byte(strconv.Itoa(count)), 0600); err != nil {
					t.Fatal(err)
				}
				if count == 1 {
					writeHelperResponse(*request.ID, result)
					return
				}
			}
		case "tools/call":
			result = map[string]any{
				"content": []map[string]string{{"type": "text", "text": "drawn"}},
			}
		}
		writeHelperResponse(*request.ID, result)
	}
}

func writeHelperResponse(id int64, result any) {
	response, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	_, _ = fmt.Fprintln(os.Stdout, string(response))
}

func helperCommand() []string {
	return []string{os.Args[0], "-test.run=^TestMCPServerHelper$"}
}
