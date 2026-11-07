package main

import (
	"fmt"
	"strings"
)

// Connection states reported to agents and by `bridge doctor`.
const (
	stateConnected    = "connected"
	stateDisconnected = "disconnected"
	stateUnpaired     = "unpaired"
	stateSearching    = "searching"
)

// buildBridgeStatus is the complete, never-empty answer to bridge_status.
// Every field is always present so an agent can tell "not connected" apart
// from "tool broke".
func buildBridgeStatus(version string, paired, connected, searching bool, host, projectID string, projectReady bool) map[string]any {
	state := stateDisconnected
	errorCode := "HOST_UNREACHABLE"
	next := "Mở LAN Bridge trên Windows. Bridge Mac sẽ tự kết nối lại khi host sẵn sàng."
	switch {
	case connected:
		state = stateConnected
		errorCode = ""
		next = ""
	case !paired:
		state = stateUnpaired
		errorCode = "NOT_PAIRED"
		next = "Ghép nối Mac với Windows một lần trong LAN Bridge (nhập mã ghép nối trên Windows)."
	case searching:
		state = stateSearching
		next = "Đang tìm Windows host trong mạng LAN theo chứng chỉ đã ghép nối. Chờ vài giây."
	}

	projectOpen := projectID != ""
	summary := fmt.Sprintf("Trạng thái: %s. Host: %s. Project: %s (mở: %t, sẵn sàng: %t).",
		state, orDash(host), orDash(projectID), projectOpen, projectReady)
	if next != "" {
		summary += " " + next
	}

	structured := map[string]any{
		"ok":            connected,
		"state":         state,
		"error_code":    errorCode,
		"next_step":     next,
		"connected":     connected,
		"paired":        paired,
		"host":          host,
		"project_id":    projectID,
		"project_open":  projectOpen,
		"project_ready": projectReady,
		"version":       version,
	}
	return map[string]any{
		"content":           []map[string]string{{"type": "text", "text": strings.TrimSpace(summary)}},
		"structuredContent": structured,
	}
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
