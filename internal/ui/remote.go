package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/projectconfig"
)

// RemoteConfigRequest is the Mac agent's request to configure the Windows
// permission file. It is honoured only while the Windows user has switched
// "Allow Mac to configure" on, and only inside the user-chosen scope folder.
type RemoteConfigRequest struct {
	Action       string   `json:"action"`
	ProjectID    string   `json:"project_id,omitempty"`
	Path         string   `json:"path,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Name         string   `json:"name,omitempty"`
	ID           string   `json:"id,omitempty"`
	Start        bool     `json:"start,omitempty"`
	Stop         bool     `json:"stop,omitempty"`
}

// SetRemoteScopeSaver registers persistence for the scope folder.
func (ws *WebServer) SetRemoteScopeSaver(save func(string) error) { ws.onRemoteScopeSave = save }

// SetRemoteScope sets the scope folder without persisting (startup load).
func (ws *WebServer) SetRemoteScope(path string) {
	ws.UpdateState(func(s *UIState) { s.RemoteScope = path })
}

// DisableRemoteConfig switches the permission off (disconnect/unpair/exit).
func (ws *WebServer) DisableRemoteConfig() {
	ws.UpdateState(func(s *UIState) { s.RemoteConfig = false })
}

func normWinPath(p string) (string, error) {
	p = strings.ReplaceAll(strings.TrimSpace(p), "/", `\`)
	if len(p) < 3 || p[1] != ':' || p[2] != '\\' {
		return "", fmt.Errorf("đường dẫn phải là đường dẫn tuyệt đối Windows (C:\\...): %q", p)
	}
	var parts []string
	for _, seg := range strings.Split(p[3:], `\`) {
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." {
			return "", errors.New("đường dẫn không được chứa '..'")
		}
		parts = append(parts, seg)
	}
	out := strings.ToLower(p[:2]) + `\` + strings.ToLower(strings.Join(parts, `\`))
	return strings.TrimSuffix(out, `\`), nil
}

// insideScope reports whether p equals scope or is below it (case-insensitive).
func insideScope(scope, p string) (bool, error) {
	s, err := normWinPath(scope)
	if err != nil {
		return false, err
	}
	n, err := normWinPath(p)
	if err != nil {
		return false, err
	}
	return n == s || strings.HasPrefix(n, s+`\`), nil
}

// HandleRemoteConfig executes one remote configuration action.
func (ws *WebServer) HandleRemoteConfig(req RemoteConfigRequest) (any, error) {
	ws.stateMu.RLock()
	enabled, scope := ws.state.RemoteConfig, ws.state.RemoteScope
	apps := append([]appInfoCopy(nil), copyApps(ws.state)...)
	ws.stateMu.RUnlock()

	if req.Action == "status" {
		return map[string]any{"enabled": enabled, "scope": scope}, nil
	}
	if !enabled {
		return nil, errors.New("REMOTE_CONFIG_DISABLED: Windows user chưa bật 'Cho phép Mac tự cấu hình' trong Settings")
	}
	if scope == "" {
		return nil, errors.New("REMOTE_SCOPE_MISSING: Windows user chưa chọn thư mục source LanBridge làm phạm vi")
	}
	checkScope := func(p string) error {
		ok, err := insideScope(scope, p)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("OUTSIDE_SCOPE: %q nằm ngoài phạm vi %q; dùng request_access để Windows user tự duyệt", p, scope)
		}
		return nil
	}
	need := func(f any, what string) error {
		if f == nil {
			return fmt.Errorf("%s chưa sẵn sàng", what)
		}
		return nil
	}
	switch req.Action {
	case "list":
		ws.stateMu.RLock()
		defer ws.stateMu.RUnlock()
		return map[string]any{
			"enabled": enabled, "scope": scope, "project_id": ws.state.ProjectID,
			"ready": ws.state.ProjectReady, "projects": ws.state.ProjectConfigs,
			"folders": ws.state.Permissions, "programs": ws.state.Apps,
		}, nil
	case "setup_project":
		if err := need(ws.onProjectSetup, "setup"); err != nil {
			return nil, err
		}
		created, err := createProjectIfMissing(ws.onProjectSetup, req.ProjectID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"project_id": req.ProjectID, "created": created, "already_exists": !created}, nil
	case "open_project":
		if err := need(ws.onProjectOpen, "open"); err != nil {
			return nil, err
		}
		if err := ws.onProjectOpen(req.ProjectID); err != nil {
			return nil, err
		}
		return map[string]any{"opened": req.ProjectID}, nil
	case "grant_folder":
		if err := checkScope(req.Path); err != nil {
			return nil, err
		}
		if err := need(ws.onGrantUpdate, "grant"); err != nil {
			return nil, err
		}
		if err := ws.onGrantUpdate(req.Path, req.Capabilities); err != nil {
			return nil, err
		}
		return map[string]any{"granted": req.Path, "capabilities": req.Capabilities}, nil
	case "revoke_folder":
		if err := checkScope(req.Path); err != nil {
			return nil, err
		}
		if err := need(ws.onGrantRevoke, "revoke"); err != nil {
			return nil, err
		}
		return map[string]any{"revoked": req.Path}, ws.onGrantRevoke(req.Path)
	case "add_program":
		if err := checkScope(req.Path); err != nil {
			return nil, err
		}
		if err := need(ws.onAppAdd, "add program"); err != nil {
			return nil, err
		}
		if err := ws.onAppAdd(req.Name, req.Path); err != nil {
			return nil, err
		}
		return map[string]any{"added": req.Name}, nil
	case "remove_program", "set_program":
		var path string
		for _, a := range apps {
			if a.ID == req.ID {
				path = a.Path
			}
		}
		if path == "" {
			return nil, fmt.Errorf("không có program id %q", req.ID)
		}
		if err := checkScope(path); err != nil {
			return nil, err
		}
		if req.Action == "remove_program" {
			if err := need(ws.onAppRemove, "remove program"); err != nil {
				return nil, err
			}
			return map[string]any{"removed": req.ID}, ws.onAppRemove(req.ID)
		}
		if err := need(ws.onAppCaps, "program caps"); err != nil {
			return nil, err
		}
		return map[string]any{"updated": req.ID}, ws.onAppCaps(req.ID, req.Start, req.Stop)
	case "save":
		if err := need(ws.onProjectSave, "save"); err != nil {
			return nil, err
		}
		return map[string]any{"saved": true}, ws.onProjectSave()
	case "bootstrap":
		// One shot right after connecting: create-if-missing, open, grant the
		// scope folder (read/sync/execute) and save.
		if req.ProjectID == "" {
			return nil, errors.New("project_id is required")
		}
		for _, f := range []any{ws.onProjectSetup, ws.onProjectOpen, ws.onGrantUpdate} {
			if f == nil {
				return nil, errors.New("bridge chưa sẵn sàng")
			}
		}
		if _, err := createProjectIfMissing(ws.onProjectSetup, req.ProjectID); err != nil {
			return nil, err
		}
		if err := ws.onProjectOpen(req.ProjectID); err != nil {
			return nil, err
		}
		if err := ws.onGrantUpdate(scope, []string{"read", "sync", "execute"}); err != nil {
			return nil, err
		}
		if ws.onProjectSave != nil {
			if err := ws.onProjectSave(); err != nil {
				return nil, err
			}
		}
		return map[string]any{"project_id": req.ProjectID, "granted": scope}, nil
	}
	return nil, fmt.Errorf("unknown action %q", req.Action)
}

type appInfoCopy struct{ ID, Path string }

func copyApps(s UIState) []appInfoCopy {
	out := make([]appInfoCopy, 0, len(s.Apps))
	for _, a := range s.Apps {
		out = append(out, appInfoCopy{a.ID, a.Path})
	}
	return out
}

// handleRemoteToggle: POST {enabled?:bool, scope?:string} from the Windows UI.
func (ws *WebServer) handleRemoteToggle(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Enabled *bool   `json:"enabled"`
		Scope   *string `json:"scope"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Scope != nil {
		scope := strings.TrimSpace(*body.Scope)
		if scope != "" {
			n, err := normWinPath(scope)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(n) <= 3 {
				http.Error(w, "không dùng ổ đĩa gốc làm phạm vi", http.StatusBadRequest)
				return
			}
		}
		if ws.onRemoteScopeSave != nil {
			if err := ws.onRemoteScopeSave(scope); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		ws.UpdateState(func(s *UIState) { s.RemoteScope = scope })
	}
	if body.Enabled != nil {
		ws.stateMu.RLock()
		scope := ws.state.RemoteScope
		ws.stateMu.RUnlock()
		if *body.Enabled && scope == "" {
			http.Error(w, "chọn thư mục source LanBridge trước khi bật", http.StatusBadRequest)
			return
		}
		ws.UpdateState(func(s *UIState) { s.RemoteConfig = *body.Enabled })
	}
	w.WriteHeader(http.StatusNoContent)
}

// createProjectIfMissing creates the Windows project profile. It reports whether
// a new profile was created. An existing profile is success, not an error.
func createProjectIfMissing(setup func(id, workspace, mcpCommand string) error, id string) (bool, error) {
	if setup == nil {
		return false, errors.New("setup chưa sẵn sàng")
	}
	err := setup(id, "", "")
	if err == nil {
		return true, nil
	}
	if errors.Is(err, projectconfig.ErrAlreadyExists) {
		return false, nil
	}
	return false, err
}

// EnsureProjectOpen makes a Windows project usable right after the Windows user
// approves a request for it: it creates the profile if missing, then opens it.
// It is idempotent, so approving twice (or for an existing project) is safe.
func (ws *WebServer) EnsureProjectOpen(id string) (created bool, err error) {
	if id == "" {
		return false, nil
	}
	created, err = createProjectIfMissing(ws.onProjectSetup, id)
	if err != nil {
		return false, err
	}
	if ws.onProjectOpen == nil {
		return created, errors.New("project open unavailable")
	}
	if err := ws.onProjectOpen(id); err != nil {
		return created, err
	}
	return created, nil
}
