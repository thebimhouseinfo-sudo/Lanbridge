package main

import "testing"

func TestBuildBridgeStatusNeverEmptyAndExplainsDisconnect(t *testing.T) {
	status := buildBridgeStatus("1.0.0", true, false, false, "192.168.1.249:54823", "", false)
	structured := status["structuredContent"].(map[string]any)
	if structured["state"] != stateDisconnected || structured["error_code"] != "HOST_UNREACHABLE" {
		t.Fatalf("disconnected status = %v", structured)
	}
	if structured["next_step"] == "" {
		t.Fatal("disconnected status must include next_step")
	}
	content := status["content"].([]map[string]string)
	if content[0]["text"] == "" {
		t.Fatal("status text must not be empty")
	}
	if structured["project_open"] != false || structured["version"] != "1.0.0" {
		t.Fatalf("missing fields: %v", structured)
	}
}

func TestBuildBridgeStatusConnectedAndUnpaired(t *testing.T) {
	connected := buildBridgeStatus("1.0.0", true, true, false, "h:1", "CAI", true)["structuredContent"].(map[string]any)
	if connected["state"] != stateConnected || connected["ok"] != true || connected["error_code"] != "" {
		t.Fatalf("connected status = %v", connected)
	}
	if connected["project_open"] != true || connected["project_ready"] != true {
		t.Fatalf("project fields = %v", connected)
	}
	unpaired := buildBridgeStatus("1.0.0", false, false, false, "", "", false)["structuredContent"].(map[string]any)
	if unpaired["state"] != stateUnpaired || unpaired["error_code"] != "NOT_PAIRED" {
		t.Fatalf("unpaired status = %v", unpaired)
	}
}

func TestBuildBridgeStatusReportsSearching(t *testing.T) {
	searching := buildBridgeStatus("1.0.0", true, false, true, "192.168.1.231:54823", "", false)["structuredContent"].(map[string]any)
	if searching["state"] != stateSearching {
		t.Fatalf("state = %v, want searching", searching["state"])
	}
	if searching["connected"] != false {
		t.Fatal("searching must not be reported as connected")
	}
}
