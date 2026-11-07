package ui

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/agentlock"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/appcontrol"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/projectconfig"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/protocol"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

//go:embed embed/*
var embedFS embed.FS

// UIState represents the real-time state broadcast to Web UI via SSE
type UIState struct {
	// chatSeq numbers chat messages for the agent inbox cursor. It never resets
	// while the process runs, so a cursor stays valid after the list is cleared.
	chatSeq            uint64
	Role               string                  `json:"role"`
	ProjectID          string                  `json:"project_id,omitempty"`
	ProjectReady       bool                    `json:"project_ready"`
	ProjectError       string                  `json:"project_error,omitempty"`
	ProjectConfigs     []string                `json:"project_configs,omitempty"`
	BlacklistEntries   []projectconfig.Rule    `json:"blacklist_entries,omitempty"`
	BlacklistLocked    bool                    `json:"blacklist_locked"`
	ListenAddr         string                  `json:"listen_addr"`
	Connected          bool                    `json:"connected"`
	RunningCommand     bool                    `json:"running_command"`
	WaitingPair        bool                    `json:"waiting_pair"`
	ActiveCode         string                  `json:"active_code,omitempty"`
	PairedFP           string                  `json:"paired_fp,omitempty"`
	ConfigError        string                  `json:"config_error,omitempty"`
	Projects           []ProjectInfo           `json:"projects,omitempty"`
	Apps               []appcontrol.AppInfo    `json:"apps,omitempty"`
	PendingPermissions []PermissionRequest     `json:"pending_permissions,omitempty"`
	Permissions        []store.PermissionGrant `json:"permissions,omitempty"`
	ChatMessages       []ChatMessage           `json:"chat_messages,omitempty"`
	Activity           *BridgeActivity         `json:"activity,omitempty"`
	MCPRegistration    string                  `json:"mcp_registration,omitempty"`
	MCPProvider        string                  `json:"mcp_provider,omitempty"`
	MCPProviders       []MCPProviderState      `json:"mcp_providers,omitempty"`
	MCPWarnings        []string                `json:"mcp_warnings,omitempty"`
	RemoteConfig       bool                    `json:"remote_config"`
	RemoteScope        string                  `json:"remote_scope,omitempty"`
}

type MCPProviderState struct {
	Provider string `json:"provider"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
}

type ProjectInfo struct {
	Name string `json:"name"`
	Root string `json:"root"`
}

type ChatMessage struct {
	ID         string    `json:"id,omitempty"`
	Seq        int64     `json:"seq,omitempty"`
	From       string    `json:"from"`
	Text       string    `json:"text"`
	SentAt     time.Time `json:"sent_at"`
	SenderKind string    `json:"sender_kind,omitempty"`
}

const (
	ChatSenderMacUser  = "mac_user"
	ChatSenderMacAgent = "mac_agent"
)

type BridgeActivity struct {
	Label  string `json:"label"`
	Status string `json:"status"`
}

type PermissionRequest struct {
	ID           string   `json:"id"`
	Path         string   `json:"path"`
	Capabilities []string `json:"capabilities"`
}

// WebServer manages the localhost HTTP server serving the embedded Web UI
type WebServer struct {
	listener            net.Listener
	token               string
	port                int
	state               UIState
	stateMu             sync.RWMutex
	clients             map[chan []byte]struct{}
	clientsMu           sync.Mutex
	mcpClients          map[chan struct{}]struct{}
	mcpClientsMu        sync.Mutex
	onGenerateCode      func() (string, error)
	onPairCode          func(code string) error
	onUnpair            func() error
	onExit              func()
	onHostConnect       func(address string) error
	onProjectList       func() ([]string,error)
	onProjectSetup      func(id,workspace,mcpCommand string) error
	windowsPicker func(context.Context, string) (string, error)
	onProjectOpen       func(id string) error
	onProjectSave       func() error
	onProfileSaveAs     func(oldID,newID string) error
	onProfileRename     func(oldID,newID string) error
	onProfileDelete     func(id string) error
	onBlacklistLock     func(locked bool) error
	onBlacklistEdit     func(entries []projectconfig.Rule) error
	onPermissionRespond func(id string, approved bool, capabilities []string) error
	onGrantUpdate       func(path string, capabilities []string) error
	onGrantRevoke       func(path string) error
	onAppPermission     func(id string, allowed bool) error
	onAppControl        func(id, action string) (string, error)
	onAppAdd            func(name,path string) error
	onAppRemove         func(id string) error
	onAppCaps           func(id string,start,stop bool) error
	onChatSend          func(text string) error
	onAgentChatSend     func(text string) (string, error)
	onMCPProviderChange func(context.Context, string) ([]MCPProviderState, error)
	// M3: MCP tool forwarding (set by bridge client)
	onMCPForward       func(toolName string, args map[string]any) (any, error)
	onRemoteScopeSave  func(string) error
	onProjectToolsList func() ([]map[string]any, error)
}

// NewWebServer initializes the embedded UI web server
func NewWebServer(role string, listenAddr string) (*WebServer, error) {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random ui token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to listen on localhost: %w", err)
	}

	ws := &WebServer{
		windowsPicker: nativeWindowsPicker,
		listener: ln,
		token:    token,
		port:     ln.Addr().(*net.TCPAddr).Port,
		state: UIState{
			Role:       role,
			ListenAddr: listenAddr,
		},
		clients:    make(map[chan []byte]struct{}),
		mcpClients: make(map[chan struct{}]struct{}),
	}

	return ws, nil
}

// SetCallbacks registers action handlers
func (ws *WebServer) SetCallbacks(onGenerateCode func() (string, error), onUnpair func() error, onExit func()) {
	ws.onGenerateCode = onGenerateCode
	ws.onUnpair = onUnpair
	ws.onExit = onExit
}

func (ws *WebServer) SetHostConnectionHandler(onConnect func(address string) error) {
	ws.onHostConnect = onConnect
}

func (ws *WebServer) SetPairCodeHandler(onSubmit func(code string) error) {
	ws.onPairCode = onSubmit
}

// SetProjectHandlers routes local profile actions to each machine's own
// App Data. On Windows only the human UI opens permissions; Mac may also
// expose an existing-only read-only Open to the local agent.
func (ws *WebServer) SetProjectManagementHandlers(
 copy func(oldID,newID string) error,
 rename func(oldID,newID string) error,
 del func(id string) error,
) {
 ws.onProfileSaveAs=copy;ws.onProfileRename=rename;ws.onProfileDelete=del
}

func (ws *WebServer) SetProjectSaveHandler(save func() error) {
 ws.onProjectSave=save
}

func (ws *WebServer) SetProjectHandlers(
 list func() ([]string,error),
 setup func(id,workspace,mcpCommand string) error,
 open func(string) error,
 lock func(bool) error,
 edit func([]projectconfig.Rule) error,
) {
 ws.onProjectList=list
 ws.onProjectSetup=setup
 ws.onProjectOpen=open
 ws.onBlacklistLock=lock
 ws.onBlacklistEdit=edit
}

// SetPermissionHandlers registers host-side permission review actions.
func (ws *WebServer) SetPermissionHandlers(
	onRespond func(id string, approved bool, capabilities []string) error,
	onUpdate func(path string, capabilities []string) error,
	onRevoke func(path string) error,
) {
	ws.onPermissionRespond = onRespond
	ws.onGrantUpdate = onUpdate
	ws.onGrantRevoke = onRevoke
}

func (ws *WebServer) SetAppHandlers(
	onPermission func(id string, allowed bool) error,
	onControl func(id, action string) (string, error),
) {
	ws.onAppPermission = onPermission
	ws.onAppControl = onControl
}

func (ws *WebServer) SetAppEditHandlers(
 add func(name,path string) error,
 remove func(id string) error,
 caps func(id string,start,stop bool) error,
) {
 ws.onAppAdd=add;ws.onAppRemove=remove;ws.onAppCaps=caps
}

func (ws *WebServer) SetChatHandler(onSend func(text string) error) {
	ws.onChatSend = onSend
}

// SetAgentChatHandler sends an agent message and returns the stored message ID.
func (ws *WebServer) SetAgentChatHandler(onSend func(text string) (string, error)) {
	ws.onAgentChatSend = onSend
}

func (ws *WebServer) SetMCPProviderHandler(onChange func(context.Context, string) ([]MCPProviderState, error)) {
	ws.onMCPProviderChange = onChange
}

// AddChatMessage stores a chat message with a unique ID and sequence number and
// returns the stored copy. Agents use the ID and the sequence as a cursor.
func (ws *WebServer) AddChatMessage(message ChatMessage) ChatMessage {
	var stored ChatMessage
	ws.UpdateState(func(state *UIState) {
		state.chatSeq++
		message.Seq = int64(state.chatSeq)
		if message.ID == "" {
			message.ID = newChatMessageID()
		}
		state.ChatMessages = append(state.ChatMessages, message)
		stored = message
	})
	return stored
}

func newChatMessageID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("msg-%d", time.Now().UnixNano())
	}
	return "msg-" + hex.EncodeToString(raw[:])
}

// ChatInbox returns messages after the cursor that were not written by the Mac
// agent, oldest first, and the cursor to pass next time.
func (ws *WebServer) ChatInbox(after int64, limit int) ([]ChatMessage, int64) {
	ws.stateMu.RLock()
	defer ws.stateMu.RUnlock()
	cursor := after
	var out []ChatMessage
	for _, message := range ws.state.ChatMessages {
		if message.Seq <= after {
			continue
		}
		cursor = message.Seq
		if message.SenderKind == ChatSenderMacAgent {
			continue
		}
		out = append(out, message)
		if len(out) >= limit {
			break
		}
	}
	return out, cursor
}

// SetMCPHandlers registers MCP forwarding callbacks used by the local bridge mcp process.
// onForward forwards a tool call over TLS to the Windows host and returns the result.
// onProjectTools returns the list of project MCP tools as generic map slices.
func (ws *WebServer) SetMCPHandlers(
	onForward func(toolName string, args map[string]any) (any, error),
	onProjectTools func() ([]map[string]any, error),
) {
	ws.onMCPForward = onForward
	ws.onProjectToolsList = onProjectTools
}

// NotifyMCPToolsChanged notifies local bridge mcp processes to refresh tools/list.
func (ws *WebServer) NotifyMCPToolsChanged() {
	ws.mcpClientsMu.Lock()
	defer ws.mcpClientsMu.Unlock()
	for client := range ws.mcpClients {
		select {
		case client <- struct{}{}:
		default:
		}
	}
}

// Start begins serving HTTP requests
func (ws *WebServer) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", ws.handleRoot)
	mux.HandleFunc("/favicon.ico", ws.handleFavicon)
	mux.HandleFunc("/events", ws.handleSSE)
	mux.HandleFunc("/api/host/connect", ws.handleHostConnect)
	mux.HandleFunc("/api/pair/generate", ws.handleGenerateCode)
	mux.HandleFunc("/api/pair/submit", ws.handleSubmitPairCode)
	mux.HandleFunc("/api/unpair", ws.handleUnpair)
	mux.HandleFunc("/api/config/open", ws.handleOpenConfig)
	mux.HandleFunc("/api/project/action", ws.handleProjectAction)
	mux.HandleFunc("/api/exit", ws.handleExit)
	mux.HandleFunc("/api/remote/toggle", ws.handleRemoteToggle)
	mux.HandleFunc("/api/permissions/respond", ws.handlePermissionRespond)
	mux.HandleFunc("/api/permissions/update", ws.handleGrantUpdate)
	mux.HandleFunc("/api/permissions/revoke", ws.handleGrantRevoke)
	mux.HandleFunc("/api/apps/permission", ws.handleAppPermission)
	mux.HandleFunc("/api/apps/control", ws.handleAppControl)
	mux.HandleFunc("/api/apps/edit", ws.handleAppEdit)
	mux.HandleFunc("/api/windows/pick", ws.handleWindowsPicker)
	mux.HandleFunc("/api/chat/send", ws.handleChatSend)
	mux.HandleFunc("/api/mcp/chat/send", ws.handleMCPChatSend)
	mux.HandleFunc("/api/mcp/chat/read", ws.handleMCPChatRead)
	mux.HandleFunc("/api/mcp/activity", ws.handleMCPActivity)
	mux.HandleFunc("/api/mcp/provider", ws.handleMCPProvider)
	// Local MCP forwarding endpoints (used by bridge mcp on the same Mac)
	mux.HandleFunc("/api/mcp/forward", ws.handleMCPForward)
	mux.HandleFunc("/api/mcp/project-tools", ws.handleProjectTools)
	mux.HandleFunc("/api/mcp/events", ws.handleMCPEvents)

	go func() {
		_ = http.Serve(ws.listener, mux)
	}()
}

func (ws *WebServer) handleMCPEvents(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodGet {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	events := make(chan struct{}, 1)
	ws.mcpClientsMu.Lock()
	ws.mcpClients[events] = struct{}{}
	ws.mcpClientsMu.Unlock()
	defer func() {
		ws.mcpClientsMu.Lock()
		delete(ws.mcpClients, events)
		ws.mcpClientsMu.Unlock()
	}()

	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-events:
			_, _ = fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n")
			flusher.Flush()
		}
	}
}

// URL returns the full localhost URL with security token
func (ws *WebServer) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/?t=%s", ws.port, ws.token)
}

// OpenBrowser opens the user's default browser to the UI
func (ws *WebServer) OpenBrowser() error {
	return openUIWindow(runtime.GOOS, ws.URL(), os.Getenv, browserFileExists, func(name string, args ...string) error {
		return exec.Command(name, args...).Start()
	})
}

func (ws *WebServer) checkToken(r *http.Request) bool {
	return r.URL.Query().Get("t") == ws.token
}

func (ws *WebServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) {
		http.Error(w, "Forbidden: Invalid token", http.StatusForbidden)
		return
	}
	data, err := embedFS.ReadFile("embed/index.html")
	if err != nil {
		http.Error(w, "Internal embed error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (ws *WebServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) {
		http.Error(w, "Forbidden: Invalid token", http.StatusForbidden)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan []byte, 10)
	ws.clientsMu.Lock()
	ws.clients[ch] = struct{}{}
	ws.clientsMu.Unlock()

	defer func() {
		ws.clientsMu.Lock()
		delete(ws.clients, ch)
		close(ch)
		ws.clientsMu.Unlock()
	}()

	// Send initial state
	ws.stateMu.RLock()
	initData, _ := json.Marshal(ws.state)
	ws.stateMu.RUnlock()
	fmt.Fprintf(w, "data: %s\n\n", initData)
	flusher.Flush()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// UpdateState broadcasts updated state to all connected SSE clients
func (ws *WebServer) UpdateState(mutator func(*UIState)) {
	ws.stateMu.Lock()
	mutator(&ws.state)
	data, _ := json.Marshal(ws.state)
	ws.stateMu.Unlock()

	ws.clientsMu.Lock()
	defer ws.clientsMu.Unlock()
	for ch := range ws.clients {
		select {
		case ch <- data:
		default:
		}
	}
}

func (ws *WebServer) handleGenerateCode(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if ws.onGenerateCode != nil {
		code, err := ws.onGenerateCode()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ws.UpdateState(func(s *UIState) {
			s.WaitingPair = true
			s.ActiveCode = code
		})
	}
	w.WriteHeader(http.StatusOK)
}

func (ws *WebServer) handleSubmitPairCode(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if ws.onPairCode == nil {
		http.Error(w, "Client pairing is not configured", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	body.Code = strings.TrimSpace(body.Code)
	if body.Code == "" {
		http.Error(w, "pairing code is required", http.StatusBadRequest)
		return
	}
	if err := ws.onPairCode(body.Code); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (ws *WebServer) handleHostConnect(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if ws.onHostConnect == nil {
		http.Error(w, "Host connection is not configured", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var body struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	body.Address = strings.TrimSpace(body.Address)
	if body.Address == "" {
		http.Error(w, "host address is required", http.StatusBadRequest)
		return
	}
	if err := ws.onHostConnect(body.Address); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (ws *WebServer) handleUnpair(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if ws.onUnpair == nil {
		http.Error(w, "Unpair is not configured", http.StatusServiceUnavailable)
		return
	}
	if err := ws.onUnpair(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}


func (ws *WebServer) handleProjectAction(w http.ResponseWriter,r *http.Request) {
 if !ws.checkToken(r) || r.Method!=http.MethodPost {
  http.Error(w,"Forbidden",http.StatusForbidden);return
 }
 r.Body=http.MaxBytesReader(w,r.Body,1024*1024)
 var req struct {
  Action string `json:"action"`
  ProjectID string `json:"project_id"`
  NewID string `json:"new_id"`
  Workspace string `json:"workspace"`
  MCPCommand string `json:"mcp_command"`
  Entries []projectconfig.Rule `json:"entries"`
 }
 if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{
  http.Error(w,"invalid project action",http.StatusBadRequest);return
 }
 var err error
 var ids []string
 switch req.Action {
 case "list":
  if ws.onProjectList==nil {err=errors.New("config list unavailable")} else {ids,err=ws.onProjectList()}
 case "setup":
  if ws.onProjectSetup==nil {err=errors.New("project setup unavailable")} else {err=ws.onProjectSetup(req.ProjectID,req.Workspace,req.MCPCommand)}
 case "open":
  if ws.onProjectOpen==nil {err=errors.New("project open unavailable")} else {err=ws.onProjectOpen(req.ProjectID)}
 case "save":
  if ws.onProjectSave==nil {err=errors.New("Windows project save unavailable")} else {err=ws.onProjectSave()}
 case "save_as":
  if ws.onProfileSaveAs==nil {err=errors.New("profile Save As unavailable")}else{err=ws.onProfileSaveAs(req.ProjectID,req.NewID)}
 case "rename":
  if ws.onProfileRename==nil{err=errors.New("profile rename unavailable")}else{err=ws.onProfileRename(req.ProjectID,req.NewID)}
 case "delete":
  if ws.onProfileDelete==nil{err=errors.New("profile delete unavailable")}else{err=ws.onProfileDelete(req.ProjectID)}
 case "lock","unlock":
  if ws.onBlacklistLock==nil {err=errors.New("Mac blacklist UI unavailable")} else {err=ws.onBlacklistLock(req.Action=="lock")}
 case "edit":
  if ws.onBlacklistEdit==nil {err=errors.New("Mac blacklist editing unavailable")} else {err=ws.onBlacklistEdit(req.Entries)}
 default:
  http.Error(w,"unknown project action",http.StatusBadRequest);return
 }
 if err!=nil {http.Error(w,err.Error(),http.StatusBadRequest);return}
 if ids==nil {ids=[]string{}}
 w.Header().Set("Content-Type","application/json")
 _=json.NewEncoder(w).Encode(map[string]any{"ok":true,"projects":ids})
}

func (ws *WebServer) handleOpenConfig(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	dir, err := store.GetConfigDir()
	if err == nil {
		if runtime.GOOS == "windows" {
			_ = exec.Command("explorer", dir).Start()
		} else if runtime.GOOS == "darwin" {
			_ = exec.Command("open", dir).Start()
		}
	}
	w.WriteHeader(http.StatusOK)
}


func (ws *WebServer) handleAppEdit(w http.ResponseWriter,r *http.Request) {
 if !ws.checkToken(r)||r.Method!=http.MethodPost {
  http.Error(w,"Forbidden",http.StatusForbidden);return
 }
 r.Body=http.MaxBytesReader(w,r.Body,64*1024)
 var req struct {
  Action string `json:"action"`
  ID string `json:"id"`
  Name string `json:"name"`
  Path string `json:"path"`
  Start bool `json:"start"`
  Stop bool `json:"stop"`
 }
 if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{
  http.Error(w,"bad app request",http.StatusBadRequest);return
 }
 var err error
 switch req.Action{
 case "add":
  if ws.onAppAdd==nil{err=errors.New("Windows app add unavailable")}else{err=ws.onAppAdd(req.Name,req.Path)}
 case "remove":
  if ws.onAppRemove==nil{err=errors.New("Windows app remove unavailable")}else{err=ws.onAppRemove(req.ID)}
 case "caps":
  if ws.onAppCaps==nil{err=errors.New("Windows app permission edit unavailable")}else{err=ws.onAppCaps(req.ID,req.Start,req.Stop)}
 default:
  http.Error(w,"unknown app action",http.StatusBadRequest);return
 }
 if err!=nil{http.Error(w,err.Error(),http.StatusBadRequest);return}
 w.WriteHeader(http.StatusOK)
}

// Windows native picker is invoked only by a local Windows UI button.
// The browser itself cannot reveal absolute OS folder paths.
func (ws *WebServer) handleWindowsPicker(w http.ResponseWriter,r *http.Request) {
 if !ws.checkToken(r)||r.Method!=http.MethodPost{http.Error(w,"Forbidden",http.StatusForbidden);return}
 ws.stateMu.RLock()
 role := ws.state.Role
 ws.stateMu.RUnlock()
 if role != "Host (Windows)" {http.Error(w,"Windows native picker unavailable",http.StatusNotImplemented);return}
 kind:=r.URL.Query().Get("kind")
 if kind!="folder" && kind!="app" {http.Error(w,"invalid picker kind",http.StatusBadRequest);return}
 ctx,cancel:=context.WithTimeout(r.Context(),5*time.Minute)
 defer cancel()
 path,err:=ws.windowsPicker(ctx,kind)
 if err!=nil {http.Error(w,"Windows picker unavailable: "+err.Error(),http.StatusServiceUnavailable);return}
 w.Header().Set("Content-Type","application/json")
 _=json.NewEncoder(w).Encode(map[string]string{"path":path})
}

func (ws *WebServer) handleExit(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusOK)
	if ws.onExit != nil {
		go func() {
			time.Sleep(200 * time.Millisecond)
			ws.onExit()
		}()
	}
}

func (ws *WebServer) handlePermissionRespond(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if ws.onPermissionRespond == nil {
		http.Error(w, "Permission review is not configured", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		ID           string   `json:"id"`
		Approved     bool     `json:"approved"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.ID == "" {
		http.Error(w, "request ID is required", http.StatusBadRequest)
		return
	}
	if err := ws.onPermissionRespond(body.ID, body.Approved, body.Capabilities); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (ws *WebServer) handleGrantUpdate(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if ws.onGrantUpdate == nil {
		http.Error(w, "Permission management is not configured", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Path         string   `json:"path"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	if err := ws.onGrantUpdate(body.Path, body.Capabilities); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (ws *WebServer) handleGrantRevoke(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if ws.onGrantRevoke == nil {
		http.Error(w, "Permission management is not configured", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	if err := ws.onGrantRevoke(body.Path); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (ws *WebServer) handleAppPermission(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if ws.onAppPermission == nil {
		http.Error(w, "App permission control is not configured", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		ID      string `json:"id"`
		Allowed *bool  `json:"allowed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.ID == "" || body.Allowed == nil {
		http.Error(w, "app ID and allowed are required", http.StatusBadRequest)
		return
	}
	if err := ws.onAppPermission(body.ID, *body.Allowed); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (ws *WebServer) handleAppControl(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if ws.onAppControl == nil {
		http.Error(w, "App control is not configured", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.ID == "" {
		http.Error(w, "app ID is required", http.StatusBadRequest)
		return
	}
	message, err := ws.onAppControl(body.ID, body.Action)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}

func (ws *WebServer) handleChatSend(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if ws.onChatSend == nil {
		http.Error(w, "Chat is not configured", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChatRequestBytes)
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := protocol.ValidateChatText(body.Text); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := ws.onChatSend(body.Text); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const maxChatRequestBytes = 6*protocol.MaxChatTextBytes + 1024

func (ws *WebServer) handleMCPChatSend(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if ws.onAgentChatSend == nil {
		http.Error(w, "Agent chat is not configured", http.StatusServiceUnavailable)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxChatRequestBytes)
	var body struct {
		Text string `json:"text"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&body); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := protocol.ValidateChatText(body.Text); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	messageID, err := ws.onAgentChatSend(body.Text)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message_id": messageID})
}

// handleMCPChatRead returns the agent inbox: GET /api/mcp/chat/read?t=&after=&limit=
func (ws *WebServer) handleMCPChatRead(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodGet {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	query := r.URL.Query()
	var after int64
	if raw := query.Get("after"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			http.Error(w, "after must be a non-negative integer", http.StatusBadRequest)
			return
		}
		after = parsed
	}
	limit := 20
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			http.Error(w, "limit must be between 1 and 100", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	messages, cursor := ws.ChatInbox(after, limit)
	if messages == nil {
		messages = []ChatMessage{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"messages": messages, "next_cursor": cursor})
}

func (ws *WebServer) handleMCPActivity(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var activity BridgeActivity
	if err := json.NewDecoder(r.Body).Decode(&activity); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	activity.Label = strings.TrimSpace(activity.Label)
	if activity.Label == "" || len(activity.Label) > 160 {
		http.Error(w, "activity label must contain 1 to 160 bytes", http.StatusBadRequest)
		return
	}
	if activity.Status != "running" && activity.Status != "completed" && activity.Status != "failed" {
		http.Error(w, "activity status is invalid", http.StatusBadRequest)
		return
	}
	ws.UpdateState(func(state *UIState) {
		state.Activity = &activity
	})
	w.WriteHeader(http.StatusNoContent)
}

func (ws *WebServer) handleMCPProvider(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if ws.onMCPProviderChange == nil {
		http.Error(w, "MCP provider management is not configured", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var body struct {
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Provider != "claude" && body.Provider != "codex" {
		http.Error(w, "provider must be claude or codex", http.StatusBadRequest)
		return
	}

	providers, err := ws.onMCPProviderChange(r.Context(), body.Provider)
	ws.UpdateState(func(state *UIState) {
		state.MCPProviders = providers
		state.MCPProvider = selectedMCPProvider(providers)
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, agentlock.ErrBusy) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func selectedMCPProvider(providers []MCPProviderState) string {
	var selected string
	for _, provider := range providers {
		if provider.Status != "managed-active" {
			continue
		}
		if selected != "" {
			return ""
		}
		selected = provider.Provider
	}
	return selected
}

// Close stops the HTTP server
func (ws *WebServer) Close() error {
	if ws.listener != nil {
		return ws.listener.Close()
	}
	return errors.New("listener nil")
}

// Port returns the local listening port (used by agentmcp auth file).
func (ws *WebServer) Port() int { return ws.port }

// Token returns the security token (used by agentmcp auth file).
func (ws *WebServer) Token() string { return ws.token }

// handleMCPForward receives a tool call from bridge mcp and forwards it
// to the Windows host over the established TLS connection.
// POST /api/mcp/forward?t=<token>
// Body: {"tool":"<name>","args":{...}}
// Response: MCP tool result JSON
func (ws *WebServer) handleMCPForward(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) || r.Method != http.MethodPost {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if ws.onMCPForward == nil {
		http.Error(w, "MCP forward not configured", http.StatusServiceUnavailable)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8*1024*1024)
	var body struct {
		Tool string         `json:"tool"`
		Args map[string]any `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	result, err := ws.onMCPForward(body.Tool, body.Args)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]any{
			"content": []map[string]string{{"type": "text", "text": "LỖI: " + err.Error()}},
			"isError": true,
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// handleProjectTools returns the list of project MCP tools from the Windows host.
// GET /api/mcp/project-tools?t=<token>
// Response: {"tools":[...]}
func (ws *WebServer) handleProjectTools(w http.ResponseWriter, r *http.Request) {
	if !ws.checkToken(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if ws.onProjectToolsList == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tools": []any{}})
		return
	}

	tools, err := ws.onProjectToolsList()
	if err != nil {
		http.Error(w, "failed to list project tools: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if tools == nil {
		tools = []map[string]any{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"tools": tools})
}

//go:embed icons/desktop.ico
var faviconICO []byte

// handleFavicon gives the --app browser window (taskbar/Alt-Tab) the app icon.
func (ws *WebServer) handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(faviconICO)
}
