package ui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/agentlock"
)

func TestWebServerTokenAndEmbed(t *testing.T) {
	ws, err := NewWebServer("Host (Windows)", "127.0.0.1:54823")
	if err != nil {
		t.Fatalf("Failed to create WebServer: %v", err)
	}
	ws.Start()
	defer ws.Close()

	respNoToken, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", ws.port))
	if err != nil {
		t.Fatalf("Failed request: %v", err)
	}
	defer respNoToken.Body.Close()
	if respNoToken.StatusCode != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden without token, got %d", respNoToken.StatusCode)
	}

	respWithToken, err := http.Get(ws.URL())
	if err != nil {
		t.Fatalf("Failed request: %v", err)
	}
	defer respWithToken.Body.Close()
	if respWithToken.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK with token, got %d", respWithToken.StatusCode)
	}
	body, err := io.ReadAll(respWithToken.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)
	if !strings.Contains(html, "LAN Test Bridge") {
		t.Error("Embedded HTML does not contain 'LAN Test Bridge'")
	}
	for _, expected := range []string{
		`id="chatView" class="view-panel active"`,
		`id="settingsView" class="view-panel"`,
		`id="settingsToggle"`,
		`id="pendingBadge"`,
		`.pending-badge[hidden] { display: none; }`,
		`id="activityBanner"`,
		`id="chatMessages"`,
		`id="btnClaudeMCP"`,
		`id="btnCodexMCP"`,
		`id="mcpProviderStates"`,
	} {
		if !strings.Contains(html, expected) {
			t.Errorf("Embedded UI is missing %q", expected)
		}
	}
	if strings.Contains(html, `class="view-tabs"`) {
		t.Error("Embedded UI should use the header Settings toggle instead of a separate tab bar")
	}
	if !strings.Contains(html, `onclick="selectMCPProvider('codex')"`) {
		t.Error("Embedded UI is missing the Codex provider action")
	}
	settingsStart := strings.Index(html, `id="settingsView"`)
	statusBadge := strings.Index(html, `id="statusBadge"`)
	if settingsStart < 0 || statusBadge < settingsStart {
		t.Error("Connection status badge should be inside Settings, not the chat header")
	}
}

func TestWebServerSSE(t *testing.T) {
	ws, err := NewWebServer("Host", "127.0.0.1:54823")
	if err != nil {
		t.Fatalf("Failed to create WebServer: %v", err)
	}
	ws.Start()
	defer ws.Close()

	sseURL := fmt.Sprintf("http://127.0.0.1:%d/events?t=%s", ws.port, ws.token)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(sseURL)
	if err != nil {
		t.Fatalf("Failed to connect to SSE: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Expected text/event-stream content type, got %s", resp.Header.Get("Content-Type"))
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read SSE initial line: %v", err)
	}
	if !strings.HasPrefix(line, "data: ") {
		t.Errorf("Expected SSE frame starting with 'data: ', got: %s", line)
	}
}

func TestWebServerMCPToolsChangedEvent(t *testing.T) {
	ws, err := NewWebServer("Client (macOS)", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.Start()
	defer ws.Close()

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/mcp/events", ws.port))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unauthorized MCP events request returned %d, want 403", resp.StatusCode)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err = client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/mcp/events?t=%s", ws.port, ws.token))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorized MCP events request returned %d, want 200", resp.StatusCode)
	}
	ws.NotifyMCPToolsChanged()

	var event strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		event.WriteString(scanner.Text())
		if strings.Contains(event.String(), "notifications/tools/list_changed") {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("MCP tools changed notification not received: %q", event.String())
}

func TestMCPProviderEndpointUpdatesStateAndReportsErrors(t *testing.T) {
	ws, err := NewWebServer("Client (macOS)", "")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var callbackCalls int
	ws.SetMCPProviderHandler(func(_ context.Context, provider string) ([]MCPProviderState, error) {
		callbackCalls++
		if provider != "codex" {
			return nil, fmt.Errorf("provider callback received %q, want codex", provider)
		}
		if callbackCalls == 1 {
			return []MCPProviderState{
				{Provider: "claude", Status: "managed-active"},
				{Provider: "codex", Status: "absent"},
			}, agentlock.ErrBusy
		}
		if callbackCalls == 2 {
			return []MCPProviderState{
				{Provider: "claude", Status: "absent"},
				{Provider: "codex", Status: "managed-active"},
			}, nil
		}
		return []MCPProviderState{
			{Provider: "claude", Status: "absent"},
			{Provider: "codex", Status: "conflict", Message: "config conflict"},
		}, errors.New("config conflict")
	})
	ws.Start()
	readState := func() UIState {
		ws.stateMu.RLock()
		defer ws.stateMu.RUnlock()
		return ws.state
	}

	post := func(body string, authorized bool) *http.Response {
		t.Helper()
		url := fmt.Sprintf("http://127.0.0.1:%d/api/mcp/provider", ws.port)
		if authorized {
			url += "?t=" + ws.token
		}
		request, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	response := post(`{"provider":"codex"}`, false)
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("unauthorized provider change returned %d, want 403", response.StatusCode)
	}
	response = post(`{"provider":"unknown"}`, true)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid provider returned %d, want 400", response.StatusCode)
	}
	if callbackCalls != 0 {
		t.Fatalf("invalid requests invoked callback %d times, want 0", callbackCalls)
	}

	response = post(`{"provider":"codex"}`, true)
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("busy provider change returned %d, want 409", response.StatusCode)
	}
	state := readState()
	if state.MCPProvider != "claude" || len(state.MCPProviders) != 2 {
		t.Fatalf("busy response did not publish actual provider state: %#v", state)
	}

	response = post(`{"provider":"codex"}`, true)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("successful provider change returned %d, want 204", response.StatusCode)
	}
	state = readState()
	if state.MCPProvider != "codex" || state.MCPProviders[1].Status != "managed-active" {
		t.Fatalf("successful provider change did not publish new state: %#v", state)
	}

	response = post(`{"provider":"codex"}`, true)
	response.Body.Close()
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("config error returned %d, want 500", response.StatusCode)
	}
	state = readState()
	if state.MCPProvider != "" || state.MCPProviders[1].Status != "conflict" {
		t.Fatalf("failed provider change did not publish actual config state: %#v", state)
	}
}

func TestPermissionManagementEndpoints(t *testing.T) {
	ws, err := NewWebServer("Host", "127.0.0.1:54823")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	responded := make(chan bool, 1)
	updated := make(chan bool, 1)
	revoked := make(chan bool, 1)
	ws.SetPermissionHandlers(
		func(id string, approved bool, capabilities []string) error {
			responded <- id == "request-1" && approved && len(capabilities) == 1 && capabilities[0] == "read"
			return nil
		},
		func(path string, capabilities []string) error {
			updated <- path == "C:\\Projects\\CAI" && len(capabilities) == 1 && capabilities[0] == "sync"
			return nil
		},
		func(path string) error {
			revoked <- path == "C:\\Projects\\CAI"
			return nil
		},
	)
	ws.Start()

	post := func(path string, body any) *http.Response {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		url := fmt.Sprintf("http://127.0.0.1:%d%s?t=%s", ws.port, path, ws.token)
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	cases := []struct {
		path string
		body any
	}{
		{"/api/permissions/respond", map[string]any{"id": "request-1", "approved": true, "capabilities": []string{"read"}}},
		{"/api/permissions/update", map[string]any{"path": "C:\\Projects\\CAI", "capabilities": []string{"sync"}}},
		{"/api/permissions/revoke", map[string]any{"path": "C:\\Projects\\CAI"}},
	}
	for _, test := range cases {
		resp := post(test.path, test.body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("%s returned %d, want %d", test.path, resp.StatusCode, http.StatusNoContent)
		}
	}

	if !<-responded || !<-updated || !<-revoked {
		t.Fatal("Permission callbacks were not invoked with the expected values")
	}

	unauthorizedURL := fmt.Sprintf("http://127.0.0.1:%d/api/permissions/revoke", ws.port)
	req, err := http.NewRequest(http.MethodPost, unauthorizedURL, bytes.NewReader([]byte(`{"path":"C:\\Projects\\CAI"}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Unauthorized permission action returned %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestAppAndChatEndpoints(t *testing.T) {
	ws, err := NewWebServer("Host (Windows)", "")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	appPermission := make(chan bool, 1)
	appAction := make(chan string, 1)
	chatText := make(chan string, 1)
	agentChatText := make(chan string, 1)
	ws.SetAppHandlers(
		func(id string, allowed bool) error {
			appPermission <- id == "0" && allowed
			return nil
		},
		func(id, action string) (string, error) {
			appAction <- id + ":" + action
			return "sent", nil
		},
	)
	ws.SetChatHandler(func(text string) error {
		chatText <- text
		return nil
	})
	ws.SetAgentChatHandler(func(text string) (string, error) {
		agentChatText <- text
		return "msg-test", nil
	})
	ws.Start()

	post := func(path, body string, token bool) *http.Response {
		t.Helper()
		url := fmt.Sprintf("http://127.0.0.1:%d%s", ws.port, path)
		if token {
			url += "?t=" + ws.token
		}
		request, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	response := post("/api/apps/permission", `{"id":"0","allowed":true}`, true)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent || !<-appPermission {
		t.Fatalf("app permission endpoint status=%d", response.StatusCode)
	}
	response = post("/api/apps/permission", `{"id":"0"}`, true)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("app permission without allowed returned status=%d", response.StatusCode)
	}
	response = post("/api/apps/control", `{"id":"0","action":"stop"}`, true)
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || <-appAction != "0:stop" || !strings.Contains(string(body), "sent") {
		t.Fatalf("app control endpoint status=%d body=%s", response.StatusCode, body)
	}
	response = post("/api/chat/send", `{"text":"paste\nlog"}`, true)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent || <-chatText != "paste\nlog" {
		t.Fatalf("chat endpoint status=%d", response.StatusCode)
	}
	response = post("/api/chat/send", `{"text":" "}`, true)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty chat returned status=%d, want %d", response.StatusCode, http.StatusBadRequest)
	}
	response = post("/api/chat/send", `{"text":"unauthorized"}`, false)
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("unauthorized chat returned status=%d, want %d", response.StatusCode, http.StatusForbidden)
	}
	response = post("/api/mcp/chat/send", `{"text":"Run this on Windows:\nWrite-Host 'ready'","sender_kind":"spoofed"}`, true)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || <-agentChatText != "Run this on Windows:\nWrite-Host 'ready'" {
		t.Fatalf("agent chat endpoint status=%d", response.StatusCode)
	}
	response = post("/api/mcp/activity", `{"label":"CAI · build","status":"running"}`, true)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("activity endpoint returned status=%d", response.StatusCode)
	}
	ws.stateMu.RLock()
	activity := ws.state.Activity
	ws.stateMu.RUnlock()
	if activity == nil || activity.Label != "CAI · build" || activity.Status != "running" {
		t.Fatalf("unexpected activity state: %+v", activity)
	}
	response = post("/api/mcp/activity", `{"label":" ","status":"running"}`, true)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid activity returned status=%d, want %d", response.StatusCode, http.StatusBadRequest)
	}
}

func TestManualHostConnectionEndpoint(t *testing.T) {
	ws, err := NewWebServer("Client (macOS)", "")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	requestedAddress := make(chan string, 1)
	ws.SetHostConnectionHandler(func(address string) error {
		if address == "bad-address" {
			return fmt.Errorf("invalid host address")
		}
		requestedAddress <- address
		return nil
	})
	ws.Start()

	post := func(body string, token string) *http.Response {
		t.Helper()
		url := fmt.Sprintf("http://127.0.0.1:%d/api/host/connect", ws.port)
		if token != "" {
			url += "?t=" + token
		}
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := post(`{"address":"192.168.1.20:54823"}`, ws.token)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("manual host connect returned %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if address := <-requestedAddress; address != "192.168.1.20:54823" {
		t.Fatalf("host connection callback received %q", address)
	}

	resp = post(`{"address":"bad-address"}`, ws.token)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "invalid host address") {
		t.Fatalf("invalid address returned status %d and body %q", resp.StatusCode, body)
	}

	resp = post(`{"address":"192.168.1.20:54823"}`, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing token returned %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestPairCodeSubmissionEndpoint(t *testing.T) {
	ws, err := NewWebServer("Client (macOS)", "")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	submittedCode := make(chan string, 1)
	ws.SetPairCodeHandler(func(code string) error {
		if code == "123456" {
			submittedCode <- code
			return nil
		}
		return fmt.Errorf("pairing failed")
	})
	ws.Start()

	post := func(body string) *http.Response {
		t.Helper()
		url := fmt.Sprintf("http://127.0.0.1:%d/api/pair/submit?t=%s", ws.port, ws.token)
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := post(`{"code":"123456"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("pair code submit returned %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	if code := <-submittedCode; code != "123456" {
		t.Fatalf("pair code callback received %q", code)
	}

	resp = post(`{"code":"654321"}`)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "pairing failed") {
		t.Fatalf("failed pairing returned status %d and body %q", resp.StatusCode, body)
	}
}

func TestPairCodeGenerationEndpointUpdatesUIState(t *testing.T) {
	ws, err := NewWebServer("Host (Windows)", "192.168.1.20:54823")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.SetCallbacks(func() (string, error) {
		return "123456", nil
	}, func() error {
		return nil
	}, nil)
	ws.Start()

	url := fmt.Sprintf("http://127.0.0.1:%d/api/pair/generate?t=%s", ws.port, ws.token)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pair code generation returned %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ws.stateMu.RLock()
	state := ws.state
	ws.stateMu.RUnlock()
	if state.ActiveCode != "123456" || !state.WaitingPair {
		t.Fatalf("generated code was not reflected in UI state: %+v", state)
	}
}

func TestUnpairEndpointPreservesConnectionState(t *testing.T) {
	ws, err := NewWebServer("Client (macOS)", "127.0.0.1:54823")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.UpdateState(func(s *UIState) {
		s.Connected = true
		s.PairedFP = "paired-host"
	})
	ws.SetCallbacks(nil, func() error {
		ws.UpdateState(func(s *UIState) {
			s.PairedFP = ""
			s.WaitingPair = s.Connected
		})
		return nil
	}, nil)
	ws.Start()

	url := fmt.Sprintf("http://127.0.0.1:%d/api/unpair?t=%s", ws.port, ws.token)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unpair returned %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ws.stateMu.RLock()
	state := ws.state
	ws.stateMu.RUnlock()
	if !state.Connected || state.PairedFP != "" || !state.WaitingPair {
		t.Fatalf("unexpected UI state after unpair: %+v", state)
	}
}
