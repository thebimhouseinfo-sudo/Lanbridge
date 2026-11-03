package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWindowsPickerEndpoint(t *testing.T) {
	ws, err := NewWebServer("Host (Windows)", "")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.windowsPicker = func(ctx context.Context, kind string) (string, error) { return `D:\Bản vẽ`, nil }
	req := httptest.NewRequest(http.MethodPost, "/api/windows/pick?kind=folder&t="+ws.Token(), nil)
	w := httptest.NewRecorder()
	ws.handleWindowsPicker(w, req)
	var result map[string]string
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result["path"] != `D:\Bản vẽ` {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/windows/pick?kind=folder&t=wrong", nil)
	w = httptest.NewRecorder()
	ws.handleWindowsPicker(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unauthorized: %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/windows/pick?kind=invalid&t="+ws.Token(), nil)
	w = httptest.NewRecorder()
	ws.handleWindowsPicker(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid kind: %d", w.Code)
	}
}
func TestPickerUIHasNoWorkspaceInput(t *testing.T) {
	raw, err := embedFS.ReadFile("embed/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `id="projectWorkspace"`) {
		t.Fatal("workspace is redundant with folder grants")
	}
}
