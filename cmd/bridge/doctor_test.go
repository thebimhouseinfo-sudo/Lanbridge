package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/agentmcp"
)

func isolateHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
}

func TestDoctorReportsNotRunningWithFix(t *testing.T) {
	isolateHome(t)
	checks := collectDoctorChecks(func() (*agentmcp.AuthInfo, error) {
		return nil, errNotRunning
	}, http.DefaultClient)
	last := checks[len(checks)-1]
	if last.OK || !strings.Contains(last.Fix, "Mở LAN Bridge trên Mac") {
		t.Fatalf("expected Mac-not-running failure with fix, got %+v", last)
	}
}

func TestDoctorFetchesStatusFromLocalEndpoint(t *testing.T) {
	isolateHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mcp/forward" || r.URL.Query().Get("t") != "tok" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var body struct {
			Tool string `json:"tool"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Tool != "bridge_status" {
			http.Error(w, "wrong tool", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(buildBridgeStatus("1.0.0", true, false, false, "192.168.1.249:54823", "", false))
	}))
	defer server.Close()

	port := portOf(t, server.URL)
	auth := &agentmcp.AuthInfo{Port: port, Token: "tok"}
	status, err := fetchBridgeStatus(server.Client(), auth)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Paired || status.Connected || status.Host != "192.168.1.249:54823" {
		t.Fatalf("status = %+v", status)
	}

	var out bytes.Buffer
	checks := collectDoctorChecks(func() (*agentmcp.AuthInfo, error) { return auth, nil }, server.Client())
	if got := printDoctorReport(&out, checks); got != 1 {
		t.Fatalf("exit code = %d, want 1 (host disconnected)\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "[FAIL] Kết nối tới Windows") {
		t.Fatalf("doctor output missing host failure:\n%s", out.String())
	}
}

func TestFetchBridgeStatusRejectsEmptyReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	_, err := fetchBridgeStatus(server.Client(), &agentmcp.AuthInfo{Port: portOf(t, server.URL), Token: "tok"})
	if err == nil || !strings.Contains(err.Error(), "rỗng") {
		t.Fatalf("empty reply error = %v", err)
	}
}

var errNotRunning = &notRunningError{}

type notRunningError struct{}

func (*notRunningError) Error() string {
	return "LAN Bridge trên Mac chưa mở (mcp_auth.json không tồn tại)"
}

func portOf(t *testing.T, rawURL string) int {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}
