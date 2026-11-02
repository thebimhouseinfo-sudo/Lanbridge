package agentmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/ui"
)

const executorTestChunkLimit = 200 * 1024

func TestMCPInitializeAndToolsList(t *testing.T) {
	isolateMCPAuthFile(t)
	outBuf := new(bytes.Buffer)
	s := NewStdioServer(nil, outBuf)

	// 1. Test tools/list
	listReq := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
	}

	s.handleRequest(listReq)

	var resp JSONRPCResponse
	if err := json.Unmarshal(outBuf.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse JSON-RPC response: %v", err)
	}

	resMap, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("Expected result map from tools/list")
	}

	rawTools, ok := resMap["tools"].([]any)
	if !ok || len(rawTools) != 9 {
		t.Fatalf("Expected 9 tools in tools/list, got %d", len(rawTools))
	}

	expectedNames := map[string]bool{
		"bridge_status":  true,
		"request_access": true,
		"list_dir":       true,
		"file_info":      true,
		"read_file":      true,
		"sync_files":     true,
		"run_command":    true,
		"command_result": true,
		"chat_send":      true,
	}

	for _, rt := range rawTools {
		toolMap := rt.(map[string]any)
		name := toolMap["name"].(string)
		if !expectedNames[name] {
			t.Errorf("Unexpected tool in list: %s", name)
		}
		delete(expectedNames, name)
	}

	if len(expectedNames) > 0 {
		t.Errorf("Missing expected tools: %v", expectedNames)
	}

	outBuf.Reset()
	s.handleRequest(&JSONRPCRequest{JSONRPC: "2.0", ID: 4, Method: "initialize"})
	if err := json.Unmarshal(outBuf.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("initialize did not return a result object")
	}
	capabilities, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatal("initialize did not declare capabilities")
	}
	toolsCapability, ok := capabilities["tools"].(map[string]any)
	if !ok || toolsCapability["listChanged"] != true {
		t.Fatal("initialize did not declare tools.listChanged")
	}
}

func TestAgentChatSendUsesSharedBridgeChat(t *testing.T) {
	isolateMCPAuthFile(t)
	failChat := false
	var received string
	activityStatuses := make(chan string, 4)
	chatRequests := make(chan struct{}, 2)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Query().Get("t") != "test-token" {
			http.Error(w, "unexpected request", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/api/mcp/activity" {
			var activity struct {
				Status string `json:"status"`
			}
			if err := json.NewDecoder(r.Body).Decode(&activity); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			activityStatuses <- activity.Status
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path != "/api/mcp/chat/send" {
			http.Error(w, "unexpected endpoint", http.StatusNotFound)
			return
		}
		chatRequests <- struct{}{}
		if failChat {
			http.Error(w, "delivery unavailable", http.StatusServiceUnavailable)
			return
		}
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		received = body.Text
		w.WriteHeader(http.StatusNoContent)
	}))
	defer httpServer.Close()
	_, portText, err := net.SplitHostPort(httpServer.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatal(err)
	}
	auth, err := WriteAuthFile(port, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := RemoveAuthFile(auth); err != nil {
			t.Errorf("remove MCP auth file: %v", err)
		}
	})

	out := new(bytes.Buffer)
	server := NewStdioServer(nil, out)
	params, err := json.Marshal(map[string]any{
		"name":      "chat_send",
		"arguments": map[string]any{"text": "Please run this on Windows:\nWrite-Host 'ready'"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server.handleRequest(&JSONRPCRequest{JSONRPC: "2.0", ID: 7, Method: "tools/call", Params: params})
	result := decodeToolResult(t, out)
	if result["isError"] == true {
		t.Fatalf("chat_send failed: %+v", result)
	}
	if received != "Please run this on Windows:\nWrite-Host 'ready'" {
		t.Fatalf("Bridge received %q", received)
	}
	if got := <-activityStatuses; got != "running" {
		t.Fatalf("initial activity status = %q, want running", got)
	}
	if got := <-activityStatuses; got != "completed" {
		t.Fatalf("completed activity status = %q, want completed", got)
	}
	<-chatRequests

	failChat = true
	out.Reset()
	server.handleRequest(&JSONRPCRequest{JSONRPC: "2.0", ID: 8, Method: "tools/call", Params: params})
	if result := decodeToolResult(t, out); result["isError"] != true {
		t.Fatalf("failed delivery was not reported as a tool error: %+v", result)
	}
	if got := <-activityStatuses; got != "running" {
		t.Fatalf("failure initial activity status = %q, want running", got)
	}
	if got := <-activityStatuses; got != "failed" {
		t.Fatalf("failed activity status = %q, want failed", got)
	}
	<-chatRequests
	select {
	case <-chatRequests:
		t.Fatal("chat delivery was retried")
	default:
	}
}

func TestActivityLabelDoesNotExposeCommandArguments(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		args     map[string]any
		want     string
	}{
		{
			name:     "command",
			toolName: "run_command",
			args:     map[string]any{"project": "CAI", "command": "distinctive secret command"},
			want:     "CAI · Chạy lệnh",
		},
		{
			name:     "command without project",
			toolName: "run_command",
			args:     map[string]any{"command": "distinctive secret command"},
			want:     "Chạy lệnh",
		},
		{
			name:     "project tool",
			toolName: "CAI__draw_line",
			want:     "CAI · draw_line",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := activityLabel(tt.toolName, tt.args)
			if got != tt.want {
				t.Fatalf("activityLabel() = %q, want %q", got, tt.want)
			}
			if strings.Contains(got, "distinctive secret command") {
				t.Fatalf("activity label exposed command arguments: %q", got)
			}
		})
	}
}

func TestMCPRejectsUnknownTools(t *testing.T) {
	isolateMCPAuthFile(t)
	out := new(bytes.Buffer)
	server := NewStdioServer(nil, out)
	params, err := json.Marshal(map[string]any{
		"name":      "unknown_tool",
		"arguments": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	server.handleRequest(&JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  params,
	})

	result := decodeToolResult(t, out)
	if result["isError"] != true {
		t.Fatalf("unknown tool call was not rejected: %+v", result)
	}
}

func TestMCPToolsListIncludesProjectTools(t *testing.T) {
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mcp/project-tools" || r.URL.Query().Get("t") != "test-token" {
			http.Error(w, "unexpected request", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tools":[{"name":"CAI__draw_line","description":"Draw line","inputSchema":{"type":"object"}}]}`))
	}))
	defer httpServer.Close()
	_, portText, err := net.SplitHostPort(httpServer.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatal(err)
	}

	out := new(bytes.Buffer)
	server := NewStdioServer(nil, out)
	server.auth = &AuthInfo{Port: port, Token: "test-token"}
	server.handleToolsList(&JSONRPCRequest{JSONRPC: "2.0", ID: 1})

	var response JSONRPCResponse
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	result, ok := response.Result.(map[string]any)
	if !ok {
		t.Fatalf("tools/list result = %#v", response.Result)
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) != 10 {
		t.Fatalf("tools/list returned %d tools, want 10", len(tools))
	}
	lastTool, ok := tools[9].(map[string]any)
	if !ok || lastTool["name"] != "CAI__draw_line" {
		t.Fatalf("project tool missing from tools/list: %#v", tools[9])
	}
}

func TestStdioServerForwardsToolsChangedNotifications(t *testing.T) {
	isolateMCPAuthFile(t)
	webServer, err := ui.NewWebServer("Client (macOS)", "")
	if err != nil {
		t.Fatal(err)
	}
	webServer.Start()
	defer webServer.Close()
	auth, err := WriteAuthFile(webServer.Port(), webServer.Token())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := RemoveAuthFile(auth); err != nil {
			t.Errorf("remove MCP auth file: %v", err)
		}
	})

	output := new(lockedBuffer)
	server := NewStdioServer(nil, output)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		server.watchMCPNotifications(ctx)
		close(done)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		webServer.NotifyMCPToolsChanged()
		if strings.Contains(output.String(), "notifications/tools/list_changed") {
			cancel()
			<-done
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatalf("stdio notification missing from output: %q", output.String())
}

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

var _ io.Writer = (*lockedBuffer)(nil)

func TestSyncFilesValidation(t *testing.T) {
	isolateMCPAuthFile(t)
	outBuf := new(bytes.Buffer)
	s := NewStdioServer(nil, outBuf)

	s.handleLocalSyncCall(3, map[string]any{
		"project": "CAI",
		"target":  "root",
		"files":   []any{}, // empty files
	})
	result := decodeToolResult(t, outBuf)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Errorf("Expected isError = true for empty files sync call")
	}
}

func TestLocalSyncRejectsEscapingAndOversizedSources(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("must not be read"), 0600); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(root, "large.bin")
	if err := os.WriteFile(large, make([]byte, maxLocalSyncBytes+1), 0600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		file string
	}{
		{name: "parent traversal", file: filepath.Join("..", filepath.Base(outside))},
		{name: "oversized file", file: "large.bin"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			out := new(bytes.Buffer)
			server := NewStdioServer(nil, out)
			server.handleLocalSyncCall(1, map[string]any{
				"project":    "CAI",
				"target":     "root",
				"local_root": root,
				"files":      []any{test.file},
			})
			result := decodeToolResult(t, out)
			if isErr, _ := result["isError"].(bool); !isErr {
				t.Fatal("invalid local sync source was not rejected")
			}
		})
	}
}

func TestLocalSyncRejectsSymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	out := new(bytes.Buffer)
	server := NewStdioServer(nil, out)
	server.handleLocalSyncCall(1, map[string]any{
		"project":    "CAI",
		"target":     "root",
		"local_root": root,
		"files":      []any{"link.txt"},
	})
	if result := decodeToolResult(t, out); result["isError"] != true {
		t.Fatalf("sync accepted a symlink outside local_root: %+v", result)
	}
}

func TestLocalSyncAllowsOnlyFilesInsideRoot(t *testing.T) {
	root := t.TempDir()
	content := []byte("sync this")
	if err := os.WriteFile(filepath.Join(root, "valid.txt"), content, 0600); err != nil {
		t.Fatal(err)
	}

	var received struct {
		Tool string `json:"tool"`
		Args struct {
			Project string `json:"project"`
			Target  string `json:"target"`
			Files   []struct {
				RelPath string `json:"rel_path"`
				Data    []byte `json:"data"`
			} `json:"files"`
		} `json:"args"`
	}
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mcp/forward" || r.URL.Query().Get("t") != "test-token" {
			http.Error(w, "unexpected request", http.StatusForbidden)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer httpServer.Close()
	_, portText, err := net.SplitHostPort(httpServer.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var portNumber int
	if _, err := fmt.Sscanf(portText, "%d", &portNumber); err != nil {
		t.Fatal(err)
	}

	out := new(bytes.Buffer)
	server := NewStdioServer(nil, out)
	server.auth = &AuthInfo{Port: portNumber, Token: "test-token"}
	server.handleLocalSyncCall(2, map[string]any{
		"project":    "CAI",
		"target":     "root",
		"local_root": root,
		"files":      []any{"valid.txt"},
	})
	if result := decodeToolResult(t, out); result["isError"] == true {
		t.Fatalf("valid local sync was rejected: %+v", result)
	}
	if received.Tool != "sync_files_batch" || received.Args.Project != "CAI" ||
		received.Args.Target != "root" || len(received.Args.Files) != 1 ||
		received.Args.Files[0].RelPath != "valid.txt" || string(received.Args.Files[0].Data) != string(content) {
		t.Fatalf("unexpected sync payload: %+v", received)
	}
}

func TestForwardingReturnsHTTPFailuresAsToolErrors(t *testing.T) {
	isolateMCPAuthFile(t)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bridge unavailable", http.StatusServiceUnavailable)
	}))
	defer httpServer.Close()
	_, portText, err := net.SplitHostPort(httpServer.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatal(err)
	}

	out := new(bytes.Buffer)
	server := NewStdioServer(nil, out)
	server.auth = &AuthInfo{Port: port, Token: "test-token"}
	server.forwardToBridgeClient(3, "list_dir", map[string]any{"path": "."})
	result := decodeToolResult(t, out)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("HTTP failure was not returned as a tool error: %+v", result)
	}
}

func TestForwardCommandToolCollectsOutputChunksIntoTemporaryFile(t *testing.T) {
	stdout := strings.Repeat("x", 200*1024) + "tail"
	var pollCount int
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tool string         `json:"tool"`
			Args map[string]any `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode bridge request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		offset := int64(0)
		if value, ok := request.Args["stdout_offset"].(float64); ok {
			offset = int64(value)
		}
		if request.Tool == "run_command" {
			offset = int64(len(stdout) - executorTestChunkLimit)
		} else {
			pollCount++
		}
		end := offset + executorTestChunkLimit
		if end > int64(len(stdout)) {
			end = int64(len(stdout))
		}
		result := map[string]any{
			"job_id":           "CAI-1",
			"status":           "done",
			"exit_code":        0,
			"stdout":           stdout[offset:end],
			"stderr":           "",
			"stdout_offset":    end,
			"stderr_offset":    0,
			"stdout_total":     len(stdout),
			"stderr_total":     0,
			"stdout_truncated": false,
			"stderr_truncated": false,
		}
		if request.Tool == "run_command" {
			result["stdout_offset"] = int64(len(stdout))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer httpServer.Close()
	_, portText, err := net.SplitHostPort(httpServer.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatal(err)
	}

	output := new(bytes.Buffer)
	server := NewStdioServer(nil, output)
	server.auth = &AuthInfo{Port: port, Token: "test-token"}
	server.forwardCommandTool(1, "run_command", map[string]any{"project": "CAI", "command": "build"})

	result := decodeToolResult(t, output)
	if result["isError"] == true {
		t.Fatalf("run_command returned error: %+v", result)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("unexpected result content: %#v", result["content"])
	}
	message, _ := content[0].(map[string]any)
	text, _ := message["text"].(string)
	const pathPrefix = "Job CAI-1 done (exit code 0). Output lưu tại: "
	if !strings.HasPrefix(text, pathPrefix) {
		t.Fatalf("unexpected command result: %q", text)
	}
	outputPath := strings.TrimPrefix(text, pathPrefix)
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read returned output file %q: %v", outputPath, err)
	}
	t.Cleanup(func() {
		if err := os.Remove(outputPath); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove output file: %v", err)
		}
	})
	if !strings.Contains(string(data), stdout) {
		t.Fatal("temporary output file did not contain all command output")
	}
	if pollCount == 0 {
		t.Fatal("large output was not fetched in additional chunks")
	}
}

func TestToolCallTimeoutsMatchHostWaits(t *testing.T) {
	cases := []struct {
		name     string
		toolName string
		args     map[string]any
		want     time.Duration
	}{
		{name: "permission approval", toolName: "request_access", want: 5*time.Minute + 15*time.Second},
		{name: "default command wait", toolName: "run_command", want: 615 * time.Second},
		{name: "zero command wait uses host default", toolName: "run_command", args: map[string]any{"wait_seconds": 0}, want: 615 * time.Second},
		{name: "command poll", toolName: "command_result", args: map[string]any{"wait_seconds": 10.0}, want: 25 * time.Second},
		{name: "project tool", toolName: "CAI__draw_line", want: 135 * time.Second},
		{name: "project tool discovery", toolName: "__list_tools", want: 5 * time.Minute},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := ToolCallTimeout(test.toolName, test.args)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("ToolCallTimeout() = %s, want %s", got, test.want)
			}
		})
	}
	if _, err := ToolCallTimeout("run_command", map[string]any{"wait_seconds": 601.0}); err == nil {
		t.Fatal("command wait above the supported maximum was accepted")
	}
}

func decodeToolResult(t *testing.T, output *bytes.Buffer) map[string]any {
	t.Helper()
	var response JSONRPCResponse
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	result, ok := response.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected tool result map, got %#v", response.Result)
	}
	return result
}

func isolateMCPAuthFile(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	t.Setenv("HOME", root)
	dir, err := store.GetConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
}
