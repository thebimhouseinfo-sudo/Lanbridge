package agentmcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClassifyErrorMapsKnownFailures(t *testing.T) {
	cases := map[string]string{
		"LAN Bridge trên Mac chưa mở (mcp_auth.json không tồn tại)": CodeBridgeNotRunning,
		"LAN Bridge trên Mac chưa mở hoặc chưa kết nối tới host":    CodeBridgeNotRunning,
		"chưa kết nối tới host Windows":                             CodeHostUnreachable,
		"OUTSIDE_SCOPE: \"D:\\x\" nằm ngoài phạm vi":                CodeOutsideScope,
		"CONFIG_NOT_OPEN":                    CodeConfigNotOpen,
		"CONFIG_ALREADY_EXISTS: use Save":    CodeConfigExists,
		"ERR_PROJECT: project 'x' not found": CodeProjectNotFound,
		"MCP tool \"foo\" is not available":  CodeToolUnavailable,
		"something unexpected":               CodeInternal,
	}
	for message, want := range cases {
		code, next := ClassifyError(message)
		if code != want {
			t.Errorf("ClassifyError(%q) = %s, want %s", message, code, want)
		}
		if next == "" {
			t.Errorf("ClassifyError(%q) returned no next step", message)
		}
	}
}

func TestEnsureNonEmptyResultNeverReturnsEmptyContent(t *testing.T) {
	empty := ensureNonEmptyResult("bridge_status", map[string]any{})
	if empty["isError"] != true {
		t.Fatalf("empty host reply must be an error, got %v", empty)
	}
	structured, _ := empty["structuredContent"].(map[string]any)
	if structured["error_code"] != CodeEmptyResult {
		t.Fatalf("empty reply error_code = %v, want %s", structured["error_code"], CodeEmptyResult)
	}

	noContent := ensureNonEmptyResult("config_list", map[string]any{"projects": []any{}})
	content, _ := noContent["content"].([]map[string]string)
	if len(content) != 1 || !strings.Contains(content[0]["text"], `"projects":[]`) {
		t.Fatalf("reply without content must keep raw data in text, got %v", noContent)
	}

	ok := map[string]any{"content": []any{map[string]any{"type": "text", "text": "hi"}}}
	if got := ensureNonEmptyResult("list_dir", ok); got["content"] == nil {
		t.Fatal("valid result must pass through")
	}
}

func TestErrorResultIsJSONSerializable(t *testing.T) {
	data, err := json.Marshal(errorResult("boom", CodeInternal))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"error_code":"INTERNAL_ERROR"`) {
		t.Fatalf("error_code missing from %s", data)
	}
}
