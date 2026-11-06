package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/agentconfig"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/agentlock"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/agentmcp"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/appcontrol"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/executor"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/fileops"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/mcpfwd"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/permissions"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/projectconfig"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/protocol"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/transport"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/transport/pairing"
	"github.com/thebimhouseinfo-sudo/testbridge/internal/ui"
)

const (
	Version     = "1.0.0"
	DefaultPort = 54823
)

func main() {
	if len(os.Args) < 2 {
		if defaultCommand(runtime.GOOS) == "client" {
			runClient(nil)
			return
		}
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "host":
		runHost(os.Args[2:])
	case "client":
		runClient(os.Args[2:])
	case "mcp":
		runMCP(os.Args[2:])
	case "ui":
		runUI()
	case "version", "-v", "--version":
		fmt.Printf("LAN Test Bridge v%s\n", Version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func defaultCommand(goos string) string {
	if goos == "darwin" {
		return "client"
	}
	return ""
}

func printUsage() {
	fmt.Printf(`LAN Test Bridge v%s — High-reliability, lightweight Mac <-> Windows bridge

Usage:
  bridge <command> [options]

Commands:
  host      Run on Windows to host commands, MCP forwarding, and file operations
  client    Run on macOS to pair and coordinate with the Windows host
  mcp       Run on macOS as a stdio MCP server for Claude Code or Codex
  ui        Open the control panel of the running Mac client
  version   Show version information
  help      Show this help message

Run 'bridge <command> --help' for command-specific flags.
`, Version)
}

func runHost(args []string) {
	fs := flag.NewFlagSet("host", flag.ExitOnError)
	portFlag := fs.Int("port", 0, "Port to listen on for LAN TLS connections (default from config or 54823)")
	headless := fs.Bool("headless", false, "Run without system tray icon (CLI/testing mode)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	// 1. Load config.json
	cfg, err := store.LoadHostConfig()
	configError := ""
	if err != nil {
		slog.Warn("Failed to load config.json, using defaults", "error", err)
		configError = err.Error()
		cfg = store.DefaultHostConfig()
	}

	listenPort := cfg.Port
	if *portFlag > 0 {
		listenPort = *portFlag
	}

	// 2. Load or generate persistent state (cert, identity, paired peers)
	stateStore, err := store.NewStateStore()
	if err != nil {
		slog.Error("Failed to initialize state store", "error", err)
		os.Exit(1)
	}

	state, err := stateStore.Load()
	if err != nil {
		slog.Error("Failed to load state", "error", err)
		os.Exit(1)
	}

	certPair, err := loadOrCreateIdentity(stateStore, state, "LAN-Bridge-Host")
	if err != nil {
		slog.Error("Failed to prepare host certificate identity", "error", err)
		os.Exit(1)
	}

	// Local Windows project profiles are explicitly opened by the user.
	// Network reconnection never reactivates a saved project.
	profileDir, err := store.GetConfigDir()
	if err != nil {
		slog.Error("Failed to determine Windows profile directory", "error", err)
		os.Exit(1)
	}
	hostProfiles, err := projectconfig.New(profileDir)
	if err != nil {
		slog.Error("Failed to initialize profile store", "error", err)
		os.Exit(1)
	}
	hostSession := &projectconfig.Session{}
	var activeProjectMu sync.RWMutex
	var activeProject *store.ProjectConfig
	getActiveProject := func(id string) *store.ProjectConfig {
		state:=hostSession.Snapshot()
		if !state.Ready || id!=state.WindowsID{return nil}
		activeProjectMu.RLock();defer activeProjectMu.RUnlock()
		if activeProject==nil || activeProject.Name!=id{return nil}
		copy:=*activeProject
		return &copy
	}

	// 3. Setup TLS server
	var pairedClientFingerprint atomic.Value
	var hostConnected atomic.Bool
	pairedClientFingerprint.Store(state.PairedPeerFingerprint)
	srvTLS := transport.NewServerTLSConfigWithFingerprint(certPair, func() string {
		return pairedClientFingerprint.Load().(string)
	})
	server := transport.NewServer(listenPort, srvTLS)
	server.SetRequestGuard(func(msg *protocol.Message) *protocol.Message {
		if pairedClientFingerprint.Load().(string) == "" &&
			msg.Type != protocol.TypePairInit && msg.Type != protocol.TypePairVerify && msg.Type != protocol.TypeUnpair {
			return protocol.NewErrorResponse(msg.ID, "ERR_UNPAIRED", "pair this device before using the bridge")
		}
		if isProjectOperation(msg.Type) {
			status := hostSession.Snapshot()
			if !status.Ready {
				return protocol.NewErrorResponse(msg.ID, status.Error,
					"both Windows and Mac must open matching local project configs before using project tools")
			}
		}
		return nil
	})

	// 4. Setup pairing manager
	pairMgr := pairing.NewHostPairingManager(state.MyFingerprint)

	// 5. Setup Web UI
	listenAddrStr := hostListenAddresses(listenPort)
	webUI, err := ui.NewWebServer("Host (Windows)", listenAddrStr)
	if err != nil {
		slog.Error("Failed to initialize Web UI", "error", err)
		os.Exit(1)
	}
	appManager := appcontrol.NewManager(cfg.Apps)

	webUI.UpdateState(func(s *ui.UIState) {
		s.Connected = false
		s.PairedFP = state.PairedPeerFingerprint
		s.ConfigError = configError
		s.Apps = appManager.List()
		s.Projects = make([]ui.ProjectInfo, 0, len(cfg.Projects))
		for _, project := range cfg.Projects {
			s.Projects = append(s.Projects, ui.ProjectInfo{Name: project.Name, Root: project.Root})
		}
	})

	// Setup tray manager
	var execMgr *executor.Manager
	var tray *ui.TrayManager
	var stopMDNS func()
	if !*headless {
		tray = ui.NewTrayManager(func() {
			if err := webUI.OpenBrowser(); err != nil {
				slog.Error("Could not open Bridge UI")
			}
		}, func() {
			slog.Info("Exiting bridge host...")
			if stopMDNS != nil {
				stopMDNS()
			}
			execMgr.CancelAll()
			_ = server.Close()
			_ = webUI.Close()
			os.Exit(0)
		})
	}

	// M3: Wire executor (run_command / exec_start, exec_result)
	execMgr = executor.NewManager()
	execMgr.SetStateChangeHandler(func() {
		running := execMgr.HasRunning()
		webUI.UpdateState(func(s *ui.UIState) {
			s.RunningCommand = running
		})
		if tray == nil {
			return
		}
		if running {
			tray.SetState(ui.TrayStateBusy, "LAN Test Bridge (Đang chạy build/test…)")
		} else if !hostConnected.Load() {
			tray.SetState(ui.TrayStateDisconnected, "LAN Test Bridge (Mất kết nối)")
		} else if pairedClientFingerprint.Load().(string) == "" {
			tray.SetState(ui.TrayStateWaiting, "LAN Test Bridge (Chờ ghép nối)")
		} else {
			tray.SetState(ui.TrayStateReady, "LAN Test Bridge (Đã kết nối, sẵn sàng)")
		}
	})

	// M3: Wire MCP forwarding (mcp_call)
	gate, err := permissions.NewGatekeeper(stateStore, cfg)
	if err != nil {
		slog.Error("Failed to initialize gatekeeper", "error", err)
		os.Exit(1)
	}
	registerReadHandlers(server, gate)
	server.RegisterHandler(protocol.TypeFileSync,
		syncFilesHandlerWithProject(gate,cfg,getActiveProject))
	sendClientUIState := func() {
		if !hostConnected.Load() || pairedClientFingerprint.Load().(string) == "" {
			return
		}
		projectState := hostSession.Snapshot()
		payload, err := json.Marshal(map[string]any{
			"permissions": gate.ListGrants(),
			"project_id": projectState.WindowsID,
			"project_ready": projectState.Ready,
			"project_error": projectState.Error,
		})
		if err != nil {
			slog.Error("Failed to encode client UI state", "error", err)
			return
		}
		if err := server.SendMessage(&protocol.Message{Type: protocol.TypeUIState, Payload: payload}); err != nil {
			slog.Warn("Could not update client permissions in the UI", "error", err)
		}
	}
	updatePermissionUI := func() {
		pending := gate.PendingRequests()
		pendingUI := make([]ui.PermissionRequest, 0, len(pending))
		for _, request := range pending {
			pendingUI = append(pendingUI, ui.PermissionRequest{
				ID:           request.ID,
				Path:         request.Path,
				Capabilities: request.Capabilities,
			})
		}
		webUI.UpdateState(func(s *ui.UIState) {
			s.PendingPermissions = pendingUI
			s.Permissions = gate.ListGrants()
		})
		sendClientUIState()
	}
	var mcpMgr *mcpfwd.Manager
	// Permission files are created/selected only by Windows human UI.
	// Mac can see the project ID metadata but never receives the file.
	webUI.SetProjectHandlers(
		func() ([]string,error) {return hostProfiles.ListIDs(false)},
		func(id,_,mcpCommand string) error {
			if err:=projectconfig.ValidateID(id);err!=nil{return err}
			// First-time project creation must NOT clone grants/program rights
			// from another active project or legacy global state. Windows user
			// explicitly grants those in this project's UI after opening it.
			folders:=[]projectconfig.FolderGrant{}
			apps:=[]projectconfig.ProgramGrant{}
			// Legacy project command/MCP definitions are only metadata
			// candidates; no permissions transfer without Windows choice.
			// Legacy config values are migration defaults only; brand-new
			// Windows repositories can be selected directly in this UI.
			var namedCommands map[string][]string
			var mcp *projectconfig.MCPConfig
			if old:=findProject(cfg,id);old!=nil {
				namedCommands=old.Commands
				if old.MCP!=nil {
					mcp=&projectconfig.MCPConfig{Transport:old.MCP.Transport,
						Command:append([]string(nil),old.MCP.Command...),URL:old.MCP.URL}
				}
			}
			if mcpCommand!=""{mcp=&projectconfig.MCPConfig{
				Transport:"stdio",Command:[]string{mcpCommand},
			}}
			_,err:=hostProfiles.InitializeWindows(id,projectconfig.WindowsPermissions{
				SchemaVersion:projectconfig.SchemaVersion,ProjectID:id,
				DisplayName:id,Folders:folders,Programs:apps,
				NamedCommands:namedCommands,MCP:mcp,
			})
			if err!=nil{return err}
			ids,_:=hostProfiles.ListIDs(false)
			webUI.UpdateState(func(st *ui.UIState){st.ProjectConfigs=ids})
			return nil
		},
		func(id string) error {
			// Reopening/switching a Windows project invalidates previously
			// loaded pairing for the project. A missing or corrupt new file
			// must not leave the old project silently authorized.
			if !hostConnected.Load(){return errors.New("connect to Mac before opening a project")}
			previousID:=hostSession.Snapshot().WindowsID
			hostSession.ClearWindows()
			activeProjectMu.Lock();activeProject=nil;activeProjectMu.Unlock()
			if previousID!="" {
				execMgr.CancelAll()
				if mcpMgr!=nil{mcpMgr.CloseProject(previousID)}
			}
			webUI.UpdateState(func(st *ui.UIState){
				st.ProjectID="";st.ProjectReady=false
				st.ProjectError=projectconfig.ErrNotOpen.Error()
			})
			sendClientUIState()
			p,err:=hostProfiles.LoadWindows(id)
			if err!=nil{return err}
			current:=&store.ProjectConfig{Name:id,
				Commands:p.NamedCommands}
			if p.MCP!=nil{current.MCP=&store.MCPConfig{
				Transport:p.MCP.Transport,Command:append([]string(nil),p.MCP.Command...),URL:p.MCP.URL,
			}}
			grants:=make([]store.PermissionGrant,0,len(p.Folders))
			for _,folder:=range p.Folders {
				grants=append(grants,store.PermissionGrant{
					Path:folder.Path,Capabilities:folder.Capabilities,
				})
			}
			if err:=gate.ReplaceGrants(grants);err!=nil{return err}
			// Previous project jobs/MCP sessions were invalidated before
			// opening the selected local file. No silent old-profile reuse.
			approvedApps:=make([]store.AppConfig,0,len(p.Programs))
			appRights:=make([]appcontrol.ActionPermissions,0,len(p.Programs))
			for _,a:=range p.Programs {
				approvedApps=append(approvedApps,store.AppConfig{Name:a.Name,Path:a.Executable})
				appRights=append(appRights,appcontrol.ActionPermissions{Start:a.Start,Stop:a.Stop})
			}
			if err:=appManager.ReplaceApps(approvedApps,appRights);err!=nil{return err}
			webUI.UpdateState(func(st *ui.UIState){st.Apps=appManager.List()})
			activeProjectMu.Lock();activeProject=current;activeProjectMu.Unlock()
			if mcpMgr!=nil{
				if previousID!=""{mcpMgr.CloseProject(previousID)}
				if id!=previousID{mcpMgr.CloseProject(id)}
			}
			if err:=hostSession.SetWindowsLoaded(id);err!=nil{return err}
			status:=hostSession.Snapshot()
			webUI.UpdateState(func(st *ui.UIState){
				st.ProjectID=id;st.ProjectReady=status.Ready;st.ProjectError=status.Error
			})
			updatePermissionUI()
			return nil
		},
		nil,nil,
	)
	var saveCurrentWindowsProfile func() error
	webUI.SetProjectManagementHandlers(
		func(oldID,newID string) error {
			if oldID==hostSession.Snapshot().WindowsID && saveCurrentWindowsProfile!=nil{
				if err:=saveCurrentWindowsProfile();err!=nil{return err}
			}
			if err:=hostProfiles.SaveAs(oldID,newID,false);err!=nil{return err}
			ids,_:=hostProfiles.ListIDs(false)
			webUI.UpdateState(func(st *ui.UIState){st.ProjectConfigs=ids})
			return nil
		},
		func(oldID,newID string) error {
			if oldID==hostSession.Snapshot().WindowsID && saveCurrentWindowsProfile!=nil{
				if err:=saveCurrentWindowsProfile();err!=nil{return err}
			}
			if err:=hostProfiles.Rename(oldID,newID,false);err!=nil{return err}
			if hostSession.Snapshot().WindowsID==oldID {
				hostSession.ClearWindows()
				activeProjectMu.Lock();activeProject=nil;activeProjectMu.Unlock()
				webUI.UpdateState(func(st *ui.UIState){
					st.ProjectID="";st.ProjectReady=false
					st.ProjectError=projectconfig.ErrNotOpen.Error()
				})
				sendClientUIState()
			}
			ids,_:=hostProfiles.ListIDs(false)
			webUI.UpdateState(func(st *ui.UIState){st.ProjectConfigs=ids})
			return nil
		},
		func(id string) error {
			if err:=hostProfiles.Delete(id,false);err!=nil{return err}
			if hostSession.Snapshot().WindowsID==id {
				hostSession.ClearWindows()
				activeProjectMu.Lock();activeProject=nil;activeProjectMu.Unlock()
				webUI.UpdateState(func(st *ui.UIState){
					st.ProjectID="";st.ProjectReady=false
					st.ProjectError=projectconfig.ErrNotOpen.Error()
				})
				sendClientUIState()
			}
			ids,_:=hostProfiles.ListIDs(false)
			webUI.UpdateState(func(st *ui.UIState){st.ProjectConfigs=ids})
			return nil
		},
	)
	// Saving the current Windows grants is a separate intentional UI action.
	// It cannot be triggered by the Mac agent and never changes the Mac file.
	saveCurrentWindowsProfile=func() error {
		id:=hostSession.Snapshot().WindowsID
		if id=="" {return projectconfig.ErrNotOpen}
		p,err:=hostProfiles.LoadWindows(id);if err!=nil{return err}
		p.Folders=make([]projectconfig.FolderGrant,0)
		for _,g:=range gate.ListGrants(){
			p.Folders=append(p.Folders,projectconfig.FolderGrant{
				Path:g.Path,Capabilities:append([]string(nil),g.Capabilities...),
			})
		}
		p.Programs=make([]projectconfig.ProgramGrant,0)
		for _,app:=range appManager.List(){
			p.Programs=append(p.Programs,projectconfig.ProgramGrant{
				Name:app.Name,Executable:app.Path,
				Start:app.CanStart,Stop:app.CanStop,
			})
		}
		return hostProfiles.SaveWindows(id,p)
	}
	webUI.SetProjectSaveHandler(saveCurrentWindowsProfile)
	// Persist grant/program edits immediately so the user never loses them by
	// forgetting a manual Save. Skipped when no config is open yet.
	autoSaveProfile:=func() error {
		if !hostSession.Snapshot().Ready || saveCurrentWindowsProfile==nil {return nil}
		if err:=saveCurrentWindowsProfile();err!=nil{
			slog.Error("Auto-save of Windows config failed","error",err)
			return fmt.Errorf("đã áp dụng nhưng chưa lưu được config: %w",err)
		}
		return nil
	}
	if ids,err:=hostProfiles.ListIDs(false);err==nil{
		webUI.UpdateState(func(st *ui.UIState){st.ProjectConfigs=ids})
	}
	webUI.SetAppHandlers(
		func(id string, allowed bool) error {
			if !hostSession.Snapshot().Ready{return projectconfig.ErrNotOpen}
			if err := appManager.SetPermission(id, allowed); err != nil {
				return err
			}
			webUI.UpdateState(func(s *ui.UIState) {
				s.Apps = appManager.List()
			})
			return autoSaveProfile()
		},
		func(id, action string) (string, error) {
			if !hostSession.Snapshot().Ready{return "",projectconfig.ErrNotOpen}
			return appManager.Control(id, action)
		},
	)
	webUI.SetAppEditHandlers(
		func(name,path string) error {
			if err:=appManager.AddApp(name,path);err!=nil{return err}
			webUI.UpdateState(func(st *ui.UIState){st.Apps=appManager.List()})
			return autoSaveProfile()
		},
		func(id string) error {
			if err:=appManager.RemoveApp(id);err!=nil{return err}
			webUI.UpdateState(func(st *ui.UIState){st.Apps=appManager.List()})
			return autoSaveProfile()
		},
		func(id string,start,stop bool) error {
			if err:=appManager.SetActionPermissions(id,start,stop);err!=nil{return err}
			webUI.UpdateState(func(st *ui.UIState){st.Apps=appManager.List()})
			return autoSaveProfile()
		},
	)
	webUI.SetChatHandler(func(text string) error {
		if !hostConnected.Load() || pairedClientFingerprint.Load().(string) == "" {
			return errors.New("client must be connected and paired to use chat")
		}
		message := ui.ChatMessage{From: "Windows", Text: text, SentAt: time.Now().UTC()}
		payload, err := json.Marshal(message)
		if err != nil {
			return fmt.Errorf("encode chat message: %w", err)
		}
		if err := server.SendMessage(&protocol.Message{Type: protocol.TypeChat, Payload: payload}); err != nil {
			return fmt.Errorf("send chat message: %w", err)
		}
		webUI.AddChatMessage(message)
		return nil
	})
	notifyProjectToolsChanged := func() {
		payload, err := json.Marshal(map[string]string{"tool": "__tools_changed"})
		if err != nil {
			slog.Error("Failed to encode project MCP tools-changed notification", "error", err)
			return
		}
		if err := server.SendMessage(&protocol.Message{Type: protocol.TypeMCPCall, Payload: payload}); err != nil {
			slog.Warn("Could not notify the Mac client that project MCP access changed", "error", err)
		}
	}
	webUI.SetPermissionHandlers(
		func(id string, approved bool, capabilities []string) error {
			if err := gate.RespondRequest(id, approved, capabilities); err != nil {
				return err
			}
			defer func(){ _ = autoSaveProfile() }()
			updatePermissionUI()
			notifyProjectToolsChanged()
			return nil
		},
		func(path string, capabilities []string) error {
			if err := gate.AddGrant(path, capabilities); err != nil {
				return err
			}
			defer func(){ _ = autoSaveProfile() }()
			// Any permission edit can remove an existing capability; cancel
			// old jobs rather than letting them continue under stale grants.
			execMgr.CancelAll()
			if id:=hostSession.Snapshot().WindowsID;id!="" && mcpMgr!=nil{mcpMgr.CloseProject(id)}
			updatePermissionUI()
			notifyProjectToolsChanged()
			return nil
		},
		func(path string) error {
			if err := gate.RevokeGrant(path); err != nil {
				return err
			}
			defer func(){ _ = autoSaveProfile() }()
			execMgr.CancelAll()
			if id:=hostSession.Snapshot().WindowsID;id!="" && mcpMgr!=nil{mcpMgr.CloseProject(id)}
			updatePermissionUI()
			notifyProjectToolsChanged()
			return nil
		},
	)
	updatePermissionUI()
	clearHostPairing := func() error {
		if err := gate.Unpair(); err != nil {
			return err
		}
		pairedClientFingerprint.Store("")
		pairMgr.Invalidate()
		updatePermissionUI()
		webUI.UpdateState(func(s *ui.UIState) {
			s.PairedFP = ""
			s.ActiveCode = ""
			s.WaitingPair = s.Connected
		})
		if tray != nil {
			if hostConnected.Load() {
				tray.SetState(ui.TrayStateWaiting, "LAN Test Bridge (Chờ ghép nối)")
			} else {
				tray.SetState(ui.TrayStateDisconnected, "LAN Test Bridge (Chưa ghép nối)")
			}
		}
		return nil
	}
	updateExpiredPairingCode := func() {
		if pairMgr.HasActiveCode() {
			return
		}
		webUI.UpdateState(func(s *ui.UIState) {
			s.ActiveCode = ""
			s.WaitingPair = s.Connected && pairedClientFingerprint.Load().(string) == ""
		})
		if tray != nil {
			if hostConnected.Load() && pairedClientFingerprint.Load().(string) == "" {
				tray.SetState(ui.TrayStateWaiting, "LAN Test Bridge (Chờ ghép nối)")
			} else if !hostConnected.Load() {
				tray.SetState(ui.TrayStateDisconnected, "LAN Test Bridge (Chưa ghép nối)")
			}
		}
	}
	registerHostPairingHandlers(
		server,
		pairMgr,
		stateStore,
		&pairedClientFingerprint,
		func(clientFP string) {
			webUI.UpdateState(func(s *ui.UIState) {
				s.PairedFP = clientFP
				s.ActiveCode = ""
				s.WaitingPair = false
			})
			sendClientUIState()
			if tray != nil {
				tray.SetState(ui.TrayStateReady, "LAN Test Bridge (Đã kết nối, sẵn sàng)")
			}
		},
		updateExpiredPairingCode,
	)
	server.RegisterHandler(protocol.TypeUnpair, func(msg *protocol.Message) (*protocol.Message, error) {
		if err := clearHostPairing(); err != nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_UNPAIR", err.Error()), nil
		}
		return protocol.NewResponse(msg.ID, protocol.TypeUnpair, nil)
	})
	server.RegisterHandler(protocol.TypeChat, func(msg *protocol.Message) (*protocol.Message, error) {
		var message ui.ChatMessage
		if err := json.Unmarshal(msg.Payload, &message); err != nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", err.Error()), nil
		}
		if err := protocol.ValidateChatText(message.Text); err != nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", err.Error()), nil
		}
		pairedFingerprint := pairedClientFingerprint.Load().(string)
		if pairedFingerprint == "" || msg.PeerFingerprint != pairedFingerprint {
			return protocol.NewErrorResponse(msg.ID, "ERR_PERMISSION", "chat requires the paired client"), nil
		}
		switch message.SenderKind {
		case ui.ChatSenderMacUser:
			message.From = "Mac"
		case ui.ChatSenderMacAgent:
			message.From = "Agent (Mac)"
		default:
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", "unsupported chat sender"), nil
		}
		message.SenderKind = ""
		message.SentAt = time.Now().UTC()
		webUI.AddChatMessage(message)
		return protocol.NewResponse(msg.ID, protocol.TypeChat, message)
	})
	webUI.SetCallbacks(func() (string, error) {
		if pairedClientFingerprint.Load().(string) != "" {
			return "", errors.New("unpair the current device before pairing another")
		}
		code, err := pairMgr.GenerateNewCode()
		if err != nil {
			return "", err
		}
		if tray != nil {
			tray.SetState(ui.TrayStateWaiting, "LAN Test Bridge (Chờ nhập mã ghép nối)")
		}
		time.AfterFunc(pairing.CodeDuration, updateExpiredPairingCode)
		return code, nil
	}, func() error {
		if err := clearHostPairing(); err != nil {
			return err
		}
		if err := server.SendMessage(&protocol.Message{Type: protocol.TypeUnpair}); err != nil {
			slog.Warn("Peer will receive unpair state on its next connection", "error", err)
		}
		return nil
	}, func() {
		if tray != nil {
			tray.Quit()
		} else {
			if stopMDNS != nil {
				stopMDNS()
			}
			execMgr.CancelAll()
			if id:=hostSession.Snapshot().WindowsID;id!="" && mcpMgr!=nil{mcpMgr.CloseProject(id)}
			_ = server.Close()
			_ = webUI.Close()
			os.Exit(0)
		}
	})

	server.RegisterHandler(protocol.TypePermissionReq, func(msg *protocol.Message) (*protocol.Message, error) {
		var req struct {
			Path         string   `json:"path"`
			Capabilities []string `json:"capabilities"`
		}
		if err := json.Unmarshal(msg.Payload, &req); err != nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", err.Error()), nil
		}
		if err := gate.BeginRequest(msg.ID, req.Path, req.Capabilities); err != nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_PERMISSION_REQUEST", err.Error()), nil
		}
		updatePermissionUI()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		capabilities, approved, err := gate.WaitForRequest(ctx, msg.ID)
		updatePermissionUI()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return protocol.NewErrorResponse(msg.ID, "ERR_PERMISSION_TIMEOUT", "permission request expired"), nil
			}
			return protocol.NewErrorResponse(msg.ID, "ERR_PERMISSION", err.Error()), nil
		}
		if !approved {
			return protocol.NewResponse(msg.ID, protocol.TypePermissionRes, map[string]any{
				"approved": false,
			})
		}
		return protocol.NewResponse(msg.ID, protocol.TypePermissionRes, map[string]any{
			"approved":     true,
			"capabilities": capabilities,
		})
	})
	// The Mac reports only an ID after its local blacklist was really opened.
	// No Windows permission JSON or Mac blacklist JSON ever crosses the LAN.
	server.RegisterHandler(protocol.TypeProjectStatus, func(msg *protocol.Message) (*protocol.Message,error) {
		var req struct {
			ProjectID string `json:"project_id"`
			Loaded bool `json:"loaded"`
		}
		if err:=json.Unmarshal(msg.Payload,&req);err!=nil{
			return protocol.NewErrorResponse(msg.ID,"ERR_PARAMS",err.Error()),nil
		}
		if req.Loaded {
			if err:=hostSession.SetMacLoaded(req.ProjectID);err!=nil{
				return protocol.NewErrorResponse(msg.ID,"CONFIG_INVALID",err.Error()),nil
			}
		}else{
			_ = hostSession.SetMacLoaded("")
		}
		status:=hostSession.Snapshot()
		webUI.UpdateState(func(st *ui.UIState){
			st.ProjectReady=status.Ready;st.ProjectError=status.Error
		})
		sendClientUIState()
		notifyProjectToolsChanged()
		return protocol.NewResponse(msg.ID,protocol.TypeProjectStatus,status)
	})
	mcpMgr = mcpfwd.NewManager(notifyProjectToolsChanged)

	// Remote configuration: the paired Mac agent may edit this Windows
	// permission file only while the Windows user has it switched on, and only
	// inside the user-chosen scope folder (the LanBridge source directory).
	if dir,err:=store.GetConfigDir();err==nil{
		scopeFile:=filepath.Join(dir,"remote_scope.txt")
		scope:=""
		if b,err:=os.ReadFile(scopeFile);err==nil{scope=strings.TrimSpace(string(b))}
		if scope=="" {
			// Default scope: the LanBridge install location itself, so the user
			// never has to type it. A saved custom folder overrides this.
			if exe,err:=os.Executable();err==nil{scope=filepath.Dir(exe)}
		}
		webUI.SetRemoteScope(scope)
		webUI.SetRemoteScopeSaver(func(p string) error {return os.WriteFile(scopeFile,[]byte(p),0600)})
	}
	server.RegisterHandler(protocol.TypeRemoteConfig, func(msg *protocol.Message) (*protocol.Message,error) {
		if msg.PeerFingerprint=="" || msg.PeerFingerprint!=pairedClientFingerprint.Load().(string) {
			return protocol.NewErrorResponse(msg.ID,"ERR_UNPAIRED","client is not paired"),nil
		}
		var req ui.RemoteConfigRequest
		if err:=json.Unmarshal(msg.Payload,&req);err!=nil{
			return protocol.NewErrorResponse(msg.ID,"ERR_PARAMS",err.Error()),nil
		}
		result,err:=webUI.HandleRemoteConfig(req)
		if err!=nil{return protocol.NewErrorResponse(msg.ID,"ERR_REMOTE_CONFIG",err.Error()),nil}
		if req.Action!="status" && req.Action!="list" {
			webUI.AddChatMessage(ui.ChatMessage{From:"Bridge",Text:"Mac agent đã chạy remote config: "+req.Action,SentAt:time.Now()})
		}
		return protocol.NewResponse(msg.ID,protocol.TypeRemoteConfig,result)
	})

	// Register handler: exec_start → start a command, wait up to wait_seconds
	server.RegisterHandler(protocol.TypeExecStart, func(msg *protocol.Message) (*protocol.Message, error) {
		var req struct {
			Project     string   `json:"project"`
			Command     string   `json:"command"`
			Argv        []string `json:"argv"`
			WorkDir     string   `json:"workdir"`
			WaitSeconds int      `json:"wait_seconds"`
		}
		if err := json.Unmarshal(msg.Payload, &req); err != nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", err.Error()), nil
		}
		if req.WaitSeconds < 0 || req.WaitSeconds > 600 {
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", "wait_seconds must be between 0 and 600"), nil
		}
		if req.WaitSeconds == 0 {
			req.WaitSeconds = 600
		}

		// Only the manually loaded Windows project matching the Mac ID
		// can execute commands; old global config entries are not active.
		proj := getActiveProject(req.Project)
		if proj == nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_PROJECT", fmt.Sprintf("project '%s' not found", req.Project)), nil
		}

		// Check execute permission
		if !gate.HasCapability(permissions.CapExecute) {
			return protocol.NewErrorResponse(msg.ID, "ERR_PERMISSION", "quyền execute chưa được cấp cho project này"), nil
		}

        // A default is safe only when exactly one folder has Exec permission.
        // Shell callers can otherwise specify a workdir explicitly.
        if req.WorkDir=="" {
            dir,dirErr:=defaultGrantedDirectory(gate.ListGrants(),permissions.CapExecute)
            if dirErr!=nil{return protocol.NewErrorResponse(msg.ID,"ERR_PARAMS",dirErr.Error()),nil}
            proj.Root=dir
        } else {
            proj.Root=req.WorkDir
        }
		var job *executor.Job
		if len(req.Argv)>0 {
			job,err=execMgr.RunShell(proj,req.Argv,req.WorkDir)
		}else{
			job,err=execMgr.RunCommand(proj,req.Command)
		}
		if err != nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_EXEC", err.Error()), nil
		}

		// Wait up to wait_seconds for the job to complete
		deadline := time.Now().Add(time.Duration(req.WaitSeconds) * time.Second)
		for time.Now().Before(deadline) {
			status, _ := job.State()
			if status != executor.StatusRunning {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}

		result := commandResultPayload(job, -1, -1)
		return protocol.NewResponse(msg.ID, protocol.TypeExecResult, result)
	})

	// Register handler: exec_result → poll a running job by job_id
	server.RegisterHandler(protocol.TypeExecResult, func(msg *protocol.Message) (*protocol.Message, error) {
		var req struct {
			JobID        string `json:"job_id"`
			WaitSeconds  int    `json:"wait_seconds"`
			StdoutOffset int64  `json:"stdout_offset"`
			StderrOffset int64  `json:"stderr_offset"`
		}
		if err := json.Unmarshal(msg.Payload, &req); err != nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", err.Error()), nil
		}
		if req.WaitSeconds < 0 || req.WaitSeconds > 600 || req.StdoutOffset < 0 || req.StderrOffset < 0 {
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", "wait_seconds must be between 0 and 600 and output offsets cannot be negative"), nil
		}

		job := execMgr.GetJob(req.JobID)
		if job == nil || getActiveProject(job.ProjectID)==nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_PROJECT",
				"command result is not accessible outside the currently opened project"), nil
		}

		status, _ := job.State()
		if req.WaitSeconds > 0 && status == executor.StatusRunning {
			deadline := time.Now().Add(time.Duration(req.WaitSeconds) * time.Second)
			for time.Now().Before(deadline) {
				status, _ = job.State()
				if status != executor.StatusRunning {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
		}

		result := commandResultPayload(job, req.StdoutOffset, req.StderrOffset)
		return protocol.NewResponse(msg.ID, protocol.TypeExecResult, result)
	})

	// Register handler: mcp_call → forward to project MCP server
	server.RegisterHandler(protocol.TypeMCPCall, func(msg *protocol.Message) (*protocol.Message, error) {
		var req struct {
			Tool string         `json:"tool"`
			Args map[string]any `json:"args"`
		}
		if err := json.Unmarshal(msg.Payload, &req); err != nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", err.Error()), nil
		}

		if req.Tool == "__list_tools" {
			var sessionErrors []string
			id:=hostSession.Snapshot().WindowsID
			proj:=getActiveProject(id)
            if proj!=nil && proj.MCP!=nil && gate.HasCapability(permissions.CapMCP) {
                dir,dirErr:=mcpGrantedDirectory(gate.ListGrants(),proj.MCP.Command)
                if dirErr!=nil {sessionErrors=append(sessionErrors,dirErr.Error())} else if _,err:=mcpMgr.Session(proj.Name,dir,proj.MCP.Transport,proj.MCP.Command);err!=nil {
					message:=fmt.Sprintf("%s: %v",proj.Name,err)
					sessionErrors=append(sessionErrors,message)
				}
			}
			tools, warnings := mcpMgr.AllTools()
			sessionErrors = append(sessionErrors, warnings...)
			// A previous project's cached MCP session must never leak tools
			// into the currently opened project's list.
			toolMaps := make([]map[string]any,0,len(tools))
			for _,t:=range tools {
				if mcpfwd.ExtractProject(t.Name)!=id {continue}
				toolMaps=append(toolMaps,map[string]any{
					"name":t.Name,"description":t.Description,
					"inputSchema":t.InputSchema,
				})
			}
			return protocol.NewResponse(msg.ID, protocol.TypeMCPCall, map[string]any{
				"tools":  toolMaps,
				"errors": sessionErrors,
			})
		}

		// Check mcp permission for the target project
		projName := mcpfwd.ExtractProject(req.Tool)
		proj := getActiveProject(projName)
		if proj == nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_PROJECT", fmt.Sprintf("project '%s' not found", projName)), nil
		}
		if !gate.HasCapability(permissions.CapMCP) {
			return protocol.NewErrorResponse(msg.ID, "ERR_PERMISSION", "quyền mcp chưa được cấp"), nil
		}
		if proj.MCP == nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_MCP", fmt.Sprintf("project '%s' không có cấu hình mcp", projName)), nil
		}

        dir,dirErr:=mcpGrantedDirectory(gate.ListGrants(),proj.MCP.Command)
        if dirErr!=nil{return protocol.NewErrorResponse(msg.ID,"ERR_PARAMS",dirErr.Error()),nil}
		sess, err := mcpMgr.Session(projName, dir, proj.MCP.Transport, proj.MCP.Command)
		if err != nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_MCP", err.Error()), nil
		}

		rawResult, err := sess.CallTool(req.Tool[len(projName)+2:], req.Args)
		if err != nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_MCP", err.Error()), nil
		}

		var resultAny any
		_ = json.Unmarshal(rawResult, &resultAny)
		return protocol.NewResponse(msg.ID, protocol.TypeMCPCall, resultAny)
	})

	// Cancel running commands on graceful bye and reset incomplete pairing exchanges on disconnect.
	server.SetCallbacks(func(peerFP string) {
		hostSession.Clear()
		hostConnected.Store(true)
		webUI.UpdateState(func(st *ui.UIState){
			st.ProjectID="";st.ProjectReady=false
			st.ProjectError=projectconfig.ErrNotOpen.Error()
		})
		slog.Info("Client connected", "fingerprint", peerFP)
		trustedFP := pairedClientFingerprint.Load().(string)
		paired := trustedFP != "" && peerFP == trustedFP
		webUI.UpdateState(func(s *ui.UIState) {
			s.Connected = true
			s.PairedFP = trustedFP
			s.WaitingPair = !paired
			if paired {
				s.ActiveCode = ""
			}
		})
		sendClientUIState()
		if tray != nil {
			if paired {
				tray.SetState(ui.TrayStateReady, "LAN Test Bridge (Đã kết nối, sẵn sàng)")
			} else {
				tray.SetState(ui.TrayStateWaiting, "LAN Test Bridge (Chờ ghép nối)")
			}
		}
		if trustedFP == "" {
			if err := server.SendMessage(&protocol.Message{Type: protocol.TypeUnpair}); err != nil {
				slog.Warn("Could not notify the client that this host is unpaired", "error", err)
			}
		}
	}, func() {
		hostSession.Clear()
		hostConnected.Store(false)
		webUI.DisableRemoteConfig()
		webUI.UpdateState(func(st *ui.UIState){
			st.ProjectID="";st.ProjectReady=false
			st.ProjectError=projectconfig.ErrNotOpen.Error()
		})
		pairMgr.ResetExchange()
		slog.Info("Client bye — cancelling all running commands")
		execMgr.CancelAll()
		gate.CancelPendingRequests()
		updatePermissionUI()
		webUI.UpdateState(func(s *ui.UIState) {
			s.Connected = false
		})
		if tray != nil {
			tray.SetState(ui.TrayStateDisconnected, "LAN Test Bridge (Ngắt kết nối)")
		}
	}, func() {
		hostSession.Clear()
		hostConnected.Store(false)
		webUI.UpdateState(func(st *ui.UIState){
			st.ProjectID="";st.ProjectReady=false
			st.ProjectError=projectconfig.ErrNotOpen.Error()
		})
		pairMgr.ResetExchange()
		slog.Warn("Client connection dropped")
		gate.CancelPendingRequests()
		updatePermissionUI()
		webUI.UpdateState(func(s *ui.UIState) {
			s.Connected = false
		})
		if tray != nil {
			tray.SetState(ui.TrayStateDisconnected, "LAN Test Bridge (Mất kết nối)")
		}
	})
	// Start servers
	if err := server.Start(); err != nil {
		slog.Error("Failed to start host server", "error", err)
		os.Exit(1)
	}
	mdnsServer, err := transport.AdvertiseHost(listenPort)
	if err != nil {
		slog.Warn("mDNS advertisement unavailable; manual IP:port fallback remains available", "error", err)
	} else {
		stopMDNS = mdnsServer.Shutdown
	}
	webUI.Start()

	fmt.Printf("[LAN Bridge Host] Listening on port %d\n", listenPort)
	fmt.Printf("[LAN Bridge Host] Web UI: %s\n", webUI.URL())

	if !*headless {
		// Run tray main loop (blocks until Exit)
		tray.Run()
	} else {
		// Block forever in headless mode
		select {}
	}
}

// authorizeMacProjectCall enforces local Mac blacklist and profile readiness
// before any transport forwarding. Empty loaded list allows all real tools;
// missing or not-loaded blacklist must never be inferred as [].
func authorizeMacProjectCall(id string,blacklist projectconfig.MacBlacklist,ready bool,
 toolName string,args map[string]any) error {
 if !ready||id==""||blacklist.ProjectID!=id||blacklist.Entries==nil {
  return projectconfig.ErrNotOpen
 }
 if isQualifiedProjectTool(toolName)&&mcpfwd.ExtractProject(toolName)!=id{
  return projectconfig.ErrMismatch
 }
 if projectconfig.Denies(blacklist,toolName,args){
  return fmt.Errorf("ERR_COMMAND_BLACKLISTED: %s",toolName)
 }
 return nil
}

// isProjectOperation identifies calls that must not bypass a missing or
// unloaded Mac blacklist. Pairing and UI setup remain usable before READY.
func isProjectOperation(typ string) bool {
 switch typ {
 case protocol.TypeFileList,protocol.TypeFileInfo,protocol.TypeFileRead,
      protocol.TypeFileSync,protocol.TypeExecStart,protocol.TypeExecResult,
      protocol.TypeMCPCall,protocol.TypeAppControl:
  return true
 }
 return false
}

// findProject looks up a ProjectConfig by name from the host config.
func findProject(cfg *store.HostConfig, name string) *store.ProjectConfig {
	for i := range cfg.Projects {
		if cfg.Projects[i].Name == name {
			return &cfg.Projects[i]
		}
	}
	return nil
}

func commandResultPayload(job *executor.Job, stdoutOffset, stderrOffset int64) map[string]any {
	status, exitCode, stdout, stderr := job.ReadOutput(stdoutOffset, stderrOffset, executor.OutputChunkLimit)
	return map[string]any{
		"job_id":           job.ID,
		"status":           status,
		"exit_code":        exitCode,
		"stdout":           string(stdout.Data),
		"stderr":           string(stderr.Data),
		"stdout_offset":    stdout.NextOffset,
		"stderr_offset":    stderr.NextOffset,
		"stdout_total":     stdout.TotalBytes,
		"stderr_total":     stderr.TotalBytes,
		"stdout_truncated": stdout.Truncated,
		"stderr_truncated": stderr.Truncated,
	}
}

func loadOrCreateIdentity(stateStore *store.StateStore, state *store.AppState, commonName string) (*transport.CertificatePair, error) {
	if state.CertPEM != "" && state.KeyPEM != "" {
		pair, err := transport.LoadCertificatePair(state.CertPEM, state.KeyPEM)
		if err != nil {
			return nil, err
		}
		if state.MyFingerprint != "" && state.MyFingerprint != pair.Fingerprint {
			return nil, errors.New("stored fingerprint does not match stored certificate")
		}
		if state.MyFingerprint == "" {
			state.MyFingerprint = pair.Fingerprint
			if err := stateStore.Save(state); err != nil {
				return nil, fmt.Errorf("save restored certificate fingerprint: %w", err)
			}
		}
		return pair, nil
	}

	if state.PairedPeerFingerprint != "" {
		return nil, errors.New("stored identity is incomplete while a peer is paired; unpair before generating a new identity")
	}
	pair, err := transport.GenerateSelfSignedCert(commonName)
	if err != nil {
		return nil, err
	}
	state.MyFingerprint = pair.Fingerprint
	state.CertPEM = pair.CertPEM
	state.KeyPEM = pair.KeyPEM
	if err := stateStore.Save(state); err != nil {
		return nil, fmt.Errorf("save new certificate identity: %w", err)
	}
	return pair, nil
}

func registerReadHandlers(server *transport.Server, gate *permissions.Gatekeeper) {
	server.RegisterHandler(protocol.TypeFileList, fileListHandler(gate))
	server.RegisterHandler(protocol.TypeFileInfo, fileInfoHandler(gate))
	server.RegisterHandler(protocol.TypeFileRead, fileReadHandler(gate))
}

func fileListHandler(gate *permissions.Gatekeeper) transport.HandlerFunc {
	return func(msg *protocol.Message) (*protocol.Message, error) {
		var req struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(msg.Payload, &req); err != nil || req.Path == "" {
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", "path is required"), nil
		}

		items, err := fileops.ListDir(gate, req.Path)
		if err != nil {
			return fileOperationErrorResponse(msg.ID, err), nil
		}
		return protocol.NewResponse(msg.ID, protocol.TypeFileList, map[string]any{"items": items})
	}
}

func fileInfoHandler(gate *permissions.Gatekeeper) transport.HandlerFunc {
	return func(msg *protocol.Message) (*protocol.Message, error) {
		var req struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(msg.Payload, &req); err != nil || req.Path == "" {
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", "path is required"), nil
		}

		info, err := fileops.GetFileInfo(gate, req.Path)
		if err != nil {
			return fileOperationErrorResponse(msg.ID, err), nil
		}
		return protocol.NewResponse(msg.ID, protocol.TypeFileInfo, info)
	}
}

func fileReadHandler(gate *permissions.Gatekeeper) transport.HandlerFunc {
	return func(msg *protocol.Message) (*protocol.Message, error) {
		var req struct {
			Path      string `json:"path"`
			Offset    int64  `json:"offset"`
			Length    int    `json:"length"`
			TailLines *int   `json:"tail_lines"`
		}
		if err := json.Unmarshal(msg.Payload, &req); err != nil || req.Path == "" {
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", "path is required"), nil
		}

		if req.TailLines != nil {
			lines, err := fileops.ReadTailLines(gate, req.Path, *req.TailLines)
			if err != nil {
				return fileOperationErrorResponse(msg.ID, err), nil
			}
			return protocol.NewResponse(msg.ID, protocol.TypeFileRead, map[string]any{
				"path":  req.Path,
				"lines": lines,
				"eof":   true,
			})
		}

		data, eof, err := fileops.ReadFileChunk(gate, req.Path, req.Offset, req.Length)
		if err != nil {
			return fileOperationErrorResponse(msg.ID, err), nil
		}
		return protocol.NewResponse(msg.ID, protocol.TypeFileRead, map[string]any{
			"path":   req.Path,
			"offset": req.Offset,
			"data":   data,
			"eof":    eof,
		})
	}
}

func registerSyncHandler(server *transport.Server, gate *permissions.Gatekeeper, cfg *store.HostConfig) {
	server.RegisterHandler(protocol.TypeFileSync, syncFilesHandler(gate, cfg))
}

func registerHostPairingHandlers(
	server *transport.Server,
	pairMgr *pairing.HostPairingManager,
	stateStore *store.StateStore,
	pairedClientFingerprint *atomic.Value,
	onPaired func(clientFingerprint string),
	onCodeExpired func(),
) {
	server.RegisterHandler(protocol.TypePairInit, func(msg *protocol.Message) (*protocol.Message, error) {
		var request struct {
			ClientMessage []byte `json:"client_message"`
		}
		if err := json.Unmarshal(msg.Payload, &request); err != nil || len(request.ClientMessage) == 0 {
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", "client_message is required"), nil
		}

		hostMessage, err := pairMgr.StartResponder(msg.PeerFingerprint, request.ClientMessage)
		if err != nil {
			if onCodeExpired != nil {
				onCodeExpired()
			}
			return protocol.NewErrorResponse(msg.ID, "ERR_PAIR_INIT", err.Error()), nil
		}
		return protocol.NewResponse(msg.ID, protocol.TypePairInit, map[string][]byte{
			"host_message": hostMessage,
		})
	})
	server.RegisterHandler(protocol.TypePairVerify, func(msg *protocol.Message) (*protocol.Message, error) {
		var request struct {
			ClientTag []byte `json:"client_tag"`
		}
		if err := json.Unmarshal(msg.Payload, &request); err != nil || len(request.ClientTag) == 0 {
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", "client_tag is required"), nil
		}

		hostTag, clientFP, err := pairMgr.FinishResponder(request.ClientTag, msg.PeerFingerprint)
		if err != nil {
			if onCodeExpired != nil {
				onCodeExpired()
			}
			return protocol.NewErrorResponse(msg.ID, "ERR_PAIR_VERIFY", err.Error()), nil
		}

		state, err := stateStore.Load()
		if err != nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_PAIR_STATE", err.Error()), nil
		}
		state.PairedPeerFingerprint = clientFP
		if err := stateStore.Save(state); err != nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_PAIR_STATE", err.Error()), nil
		}
		pairedClientFingerprint.Store(clientFP)
		if onPaired != nil {
			onPaired(clientFP)
		}
		return protocol.NewResponse(msg.ID, protocol.TypePairVerify, map[string][]byte{
			"host_tag": hostTag,
		})
	})
}

func performClientPairing(client *transport.Client, code, clientFingerprint string) (string, error) {
	hostFingerprint, err := client.PeerFingerprint()
	if err != nil {
		return "", err
	}
	session := pairing.NewClientPairingSession(code, hostFingerprint, clientFingerprint)
	clientMessage, err := session.StartInitiator()
	if err != nil {
		return "", err
	}

	initPayload, err := json.Marshal(map[string][]byte{"client_message": clientMessage})
	if err != nil {
		return "", fmt.Errorf("encode pairing initialization: %w", err)
	}
	initResponse, err := client.RoundTrip(&protocol.Message{
		ID:      fmt.Sprintf("pair-init-%d", time.Now().UnixNano()),
		Type:    protocol.TypePairInit,
		Payload: initPayload,
	}, 10*time.Second)
	if err != nil {
		return "", fmt.Errorf("send pairing initialization: %w", err)
	}
	if err := validatePairingResponse(initResponse, protocol.TypePairInit); err != nil {
		return "", err
	}
	var initResult struct {
		HostMessage []byte `json:"host_message"`
	}
	if err := json.Unmarshal(initResponse.Payload, &initResult); err != nil || len(initResult.HostMessage) == 0 {
		return "", errors.New("host returned an invalid pairing message")
	}

	clientTag, err := session.FinishInitiator(initResult.HostMessage)
	if err != nil {
		return "", err
	}
	verifyPayload, err := json.Marshal(map[string][]byte{"client_tag": clientTag})
	if err != nil {
		return "", fmt.Errorf("encode pairing confirmation: %w", err)
	}
	verifyResponse, err := client.RoundTrip(&protocol.Message{
		ID:      fmt.Sprintf("pair-verify-%d", time.Now().UnixNano()),
		Type:    protocol.TypePairVerify,
		Payload: verifyPayload,
	}, 10*time.Second)
	if err != nil {
		return "", fmt.Errorf("send pairing confirmation: %w", err)
	}
	if err := validatePairingResponse(verifyResponse, protocol.TypePairVerify); err != nil {
		return "", err
	}
	var verifyResult struct {
		HostTag []byte `json:"host_tag"`
	}
	if err := json.Unmarshal(verifyResponse.Payload, &verifyResult); err != nil || len(verifyResult.HostTag) == 0 {
		return "", errors.New("host returned an invalid confirmation tag")
	}
	if err := session.VerifyHostTag(verifyResult.HostTag); err != nil {
		return "", err
	}
	return hostFingerprint, nil
}

func validatePairingResponse(response *protocol.Message, expectedType string) error {
	if response == nil {
		return errors.New("host returned an empty pairing response")
	}
	if response.Error != nil {
		return response.Error
	}
	if response.Type != expectedType {
		return fmt.Errorf("unexpected pairing response type %q, want %q", response.Type, expectedType)
	}
	return nil
}

func forwardAgentMCPTool(client *transport.Client, toolName string, args map[string]any) (any, error) {
	var messageType string
	var payload any = args
	switch toolName {
	case "request_access":
		messageType = protocol.TypePermissionReq
	case "list_dir":
		messageType = protocol.TypeFileList
	case "file_info":
		messageType = protocol.TypeFileInfo
	case "read_file":
		messageType = protocol.TypeFileRead
	case "sync_files_batch":
		messageType = protocol.TypeFileSync
	case "run_command":
		messageType = protocol.TypeExecStart
	case "command_result":
		messageType = protocol.TypeExecResult
	default:
		messageType = protocol.TypeMCPCall
		payload = map[string]any{
			"tool": toolName,
			"args": args,
		}
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode MCP tool arguments: %w", err)
	}
	timeout, err := agentmcp.ToolCallTimeout(toolName, args)
	if err != nil {
		return nil, err
	}
	response, err := client.RoundTrip(&protocol.Message{
		ID:      fmt.Sprintf("mcp-%d", time.Now().UnixNano()),
		Type:    messageType,
		Payload: payloadBytes,
	}, timeout)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, response.Error
	}
	expectedType := messageType
	if messageType == protocol.TypePermissionReq {
		expectedType = protocol.TypePermissionRes
	} else if messageType == protocol.TypeExecStart {
		expectedType = protocol.TypeExecResult
	}
	if response.Type != expectedType {
		return nil, fmt.Errorf("unexpected response type %q for MCP tool %q", response.Type, toolName)
	}
	if len(response.Payload) == 0 {
		return nil, nil
	}
	var result any
	if err := json.Unmarshal(response.Payload, &result); err != nil {
		return nil, fmt.Errorf("decode MCP tool response: %w", err)
	}
	return result, nil
}

func isQualifiedProjectTool(toolName string) bool {
	separator := strings.Index(toolName, "__")
	return separator > 0 && separator+2 < len(toolName)
}

func saveClientPairingState(stateStore *store.StateStore, fingerprint string, pendingUnpair bool) error {
	state, err := stateStore.Load()
	if err != nil {
		return err
	}
	state.PairedPeerFingerprint = fingerprint
	state.PendingUnpair = pendingUnpair
	return stateStore.Save(state)
}

func syncPendingClientUnpair(client *transport.Client, stateStore *store.StateStore) error {
	state, err := stateStore.Load()
	if err != nil {
		return err
	}
	if !state.PendingUnpair {
		return nil
	}
	if !client.IsConnected() {
		return errors.New("client is not connected")
	}
	response, err := client.RoundTrip(&protocol.Message{
		ID:   fmt.Sprintf("unpair-sync-%d", time.Now().UnixNano()),
		Type: protocol.TypeUnpair,
	}, 10*time.Second)
	if err != nil {
		return err
	}
	if err := validatePairingResponse(response, protocol.TypeUnpair); err != nil {
		return err
	}
	state.PendingUnpair = false
	if err := stateStore.Save(state); err != nil {
		return fmt.Errorf("clear pending unpair marker: %w", err)
	}
	return nil
}

func syncFilesHandler(gate *permissions.Gatekeeper, cfg *store.HostConfig) transport.HandlerFunc {
 return syncFilesHandlerWithProject(gate,cfg,func(id string)*store.ProjectConfig{return findProject(cfg,id)})
}

// With a selected profile, only the matching active project may Sync.
// The direct target path option is still checked by the Windows actual Grant.
func syncFilesHandlerWithProject(gate *permissions.Gatekeeper,cfg *store.HostConfig,find func(string)*store.ProjectConfig) transport.HandlerFunc {
	return func(msg *protocol.Message) (*protocol.Message, error) {
		var req struct {
			Project string             `json:"project"`
			Target  string             `json:"target"`
			Files   []fileops.SyncItem `json:"files"`
		}
		if err := json.Unmarshal(msg.Payload, &req); err != nil || req.Project == "" || req.Target == "" || len(req.Files) == 0 {
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", "project, target, and a non-empty files list are required"), nil
		}

		project := find(req.Project)
		if project == nil {
			return protocol.NewErrorResponse(msg.ID, "ERR_PROJECT", fmt.Sprintf("project %q is not the active loaded project", req.Project)), nil
		}

		var targetBase string
		switch req.Target {
		case "root":
            targetBase = project.Root // compatibility for legacy static projects
            if targetBase == "" {
                var dirErr error
                targetBase,dirErr=defaultGrantedDirectory(gate.ListGrants(),permissions.CapSync)
                if dirErr!=nil{return protocol.NewErrorResponse(msg.ID,"ERR_PARAMS",dirErr.Error()),nil}
            }
		default:
			if filepath.IsAbs(req.Target){
				targetBase=req.Target
			}else{
				targetBase=project.RelatedDirs[req.Target]
			}
		}
		if targetBase == "" {
			return protocol.NewErrorResponse(msg.ID, "ERR_PARAMS", fmt.Sprintf("target %q is not configured for project %q", req.Target, req.Project)), nil
		}

		results, err := fileops.SyncFiles(gate, targetBase, req.Files, cfg.MaxSyncMB)
		if err != nil {
			return fileOperationErrorResponse(msg.ID, err), nil
		}
		return protocol.NewResponse(msg.ID, protocol.TypeFileSync, map[string]any{"results": results})
	}
}

func fileOperationErrorResponse(id string, err error) *protocol.Message {
	code := "ERR_IO"
	switch {
	case errors.Is(err, permissions.ErrAccessDenied), errors.Is(err, permissions.ErrPathEscape):
		code = "ERR_PERMISSION"
	case strings.HasPrefix(err.Error(), "ERR_PERMISSION_DENIED:"):
		code = "ERR_PERMISSION"
	case strings.HasPrefix(err.Error(), "ERR_NOT_FOUND:"):
		code = "ERR_NOT_FOUND"
	case strings.HasPrefix(err.Error(), "ERR_PARAMS:"):
		code = "ERR_PARAMS"
	case strings.HasPrefix(err.Error(), "ERR_LIMIT:"):
		code = "ERR_LIMIT"
	case strings.HasPrefix(err.Error(), "ERR_TOO_LARGE:"):
		code = "ERR_TOO_LARGE"
	case strings.HasPrefix(err.Error(), "ERR_FILE_LOCKED:"):
		code = "ERR_FILE_LOCKED"
	}
	return protocol.NewErrorResponse(id, code, err.Error())
}

func runClient(args []string) {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	hostAddr := fs.String("host", "", "Host IP address (optional port defaults to 54823)")
	headless := fs.Bool("headless", false, "Run without menu bar icon")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	// Launching the app again (Finder/Spotlight/open) while it is already
	// running just opens its UI; the tray icon may be hidden on crowded menu bars.
	if !*headless {
		if opened, err := openRunningUI(); opened {
			if err != nil {
				slog.Error("Could not open running Bridge UI", "error", err)
				os.Exit(1)
			}
			return
		}
	}

	stateStore, err := store.NewStateStore()
	if err != nil {
		slog.Error("Failed to initialize state store", "error", err)
		os.Exit(1)
	}

	state, err := stateStore.Load()
	if err != nil {
		slog.Error("Failed to load client state", "error", err)
		os.Exit(1)
	}
	certPair, err := loadOrCreateIdentity(stateStore, state, "LAN-Bridge-Client")
	if err != nil {
		slog.Error("Failed to prepare client certificate identity", "error", err)
		os.Exit(1)
	}
	// Mac owns only local blacklist files; it never downloads the Windows
	// permissions file, and agent Open remains existing-only.
	macDir,err:=store.GetConfigDir()
	if err!=nil{slog.Error("Failed to determine Mac config directory","error",err);os.Exit(1)}
	macProfiles,err:=projectconfig.New(macDir)
	if err!=nil{slog.Error("Failed to prepare Mac project configs","error",err);os.Exit(1)}
	var pairedHostFingerprint atomic.Value
	pairedHostFingerprint.Store(state.PairedPeerFingerprint)

	target := strings.TrimSpace(*hostAddr)
	if target == "" {
		target = state.LastConnectedAddr
	}
	if target != "" {
		normalized, err := normalizeHostAddress(target)
		if err != nil {
			slog.Error("Host address must be a valid IP with an optional port", "address", target, "error", err)
			os.Exit(1)
		}
		target = normalized
	}

	fmt.Printf("[LAN Bridge Client] Target host: %s\n", target)

	webUI, err := ui.NewWebServer("Client (macOS)", target)
	if err != nil {
		slog.Error("Failed to create Web UI", "error", err)
		os.Exit(1)
	}

	bridgeExecutable, executableErr := os.Executable()
	var agentConfigManager *agentconfig.Manager
	var agentConfigInitErr error
	if executableErr != nil {
		agentConfigInitErr = fmt.Errorf("resolve Bridge executable for MCP registration: %w", executableErr)
	} else {
		agentConfigManager, agentConfigInitErr = agentconfig.NewManager(bridgeExecutable)
	}
	initialAgentConfigStates := []ui.MCPProviderState{
		{Provider: string(agentconfig.ProviderClaude), Status: string(agentconfig.StatusInvalid)},
		{Provider: string(agentconfig.ProviderCodex), Status: string(agentconfig.StatusInvalid)},
	}
	initialAgentProvider := ""
	if agentConfigManager != nil {
		snapshot := agentConfigManager.Inspect()
		initialAgentConfigStates = toUIAgentConfigStates(snapshot)
		initialAgentProvider = selectedAgentProvider(snapshot)
	}
	if agentConfigInitErr != nil {
		slog.Error("Agent MCP configuration manager is unavailable", "error", agentConfigInitErr)
		initialAgentConfigStates[0].Message = agentConfigInitErr.Error()
		initialAgentConfigStates[1].Message = agentConfigInitErr.Error()
	}

	webUI.UpdateState(func(s *ui.UIState) {
		s.Connected = false
		s.PairedFP = pairedHostFingerprint.Load().(string)
		s.MCPRegistration = "Chọn Claude Code hoặc Codex. Bridge chỉ cho phép một agent MCP hoạt động cùng lúc."
		s.MCPProvider = initialAgentProvider
		s.MCPProviders = initialAgentConfigStates
		s.ChatMessages = []ui.ChatMessage{}
	})
	var tray *ui.TrayManager
	var shutdown func()
	if !*headless {
		tray = ui.NewTrayManager(func() {
			if err := webUI.OpenBrowser(); err != nil {
				slog.Error("Could not open Bridge UI")
			}
		}, func() {
			if shutdown != nil {
				shutdown()
			}
		})
	}

	if !*headless {
		ui.InstallReopenHandler(func() {
			if err := webUI.OpenBrowser(); err != nil {
				slog.Error("Could not open Bridge UI on reopen")
			}
		})
	}

	var clientMu sync.Mutex
	var activeClient *transport.Client
	var activeTarget string
	var clientPairingMu sync.Mutex
	var macProfileMu sync.RWMutex
	var macProjectID string
	var macBlacklist projectconfig.MacBlacklist
	var macProjectReady bool

	getMacProfile:=func() (string,projectconfig.MacBlacklist,bool) {
		macProfileMu.RLock();defer macProfileMu.RUnlock()
		return macProjectID,macBlacklist,macProjectReady
	}
	clearMacProfile:=func() {
		macProfileMu.Lock()
		macProjectID=""
		macBlacklist=projectconfig.MacBlacklist{}
		macProjectReady=false
		macProfileMu.Unlock()
		webUI.UpdateState(func(st *ui.UIState){
			st.ProjectID="";st.ProjectReady=false
			st.ProjectError=projectconfig.ErrNotOpen.Error()
			st.BlacklistEntries=nil
		})
		webUI.NotifyMCPToolsChanged()
	}
	openMacProfile:=func(id string) error {
		// Switching/reloading must fail closed: a failed attempt to open an
		// absent blacklist must never leave a previously loaded project
		// accidentally active on either side of the Bridge.
		clearMacProfile()
		clientMu.Lock();client:=activeClient;clientMu.Unlock()
		if client==nil || !client.IsConnected(){return errors.New("connect to Windows before opening project blacklist")}
		closePayload,_:=json.Marshal(map[string]any{"project_id":"","loaded":false})
		closed,err:=client.RoundTrip(&protocol.Message{
			ID:fmt.Sprintf("profile-unload-%d",time.Now().UnixNano()),
			Type:protocol.TypeProjectStatus,Payload:closePayload,
		},15*time.Second)
		if err!=nil{return err}
		if closed.Error!=nil{return closed.Error}
		// LoadMac is read-only and never creates a missing file.
		loaded,err:=macProfiles.LoadMac(id);if err!=nil{return err}
		payload,_:=json.Marshal(map[string]any{"project_id":id,"loaded":true})
		resp,err:=client.RoundTrip(&protocol.Message{
			ID:fmt.Sprintf("profile-%d",time.Now().UnixNano()),
			Type:protocol.TypeProjectStatus,Payload:payload,
		},15*time.Second)
		if err!=nil{return err}
		if resp.Error!=nil{return resp.Error}
		if resp.Type!=protocol.TypeProjectStatus{return errors.New("unexpected project status response")}
		var status projectconfig.Status
		if err:=json.Unmarshal(resp.Payload,&status);err!=nil{return err}
		macProfileMu.Lock()
		macProjectID=id
		macBlacklist=loaded
		macProjectReady=status.Ready
		macProfileMu.Unlock()
		locked,err:=macProfiles.IsLocked(id);if err!=nil{return err}
		webUI.UpdateState(func(st *ui.UIState){
			st.ProjectID=id;st.ProjectReady=status.Ready
			st.ProjectError=status.Error;st.BlacklistLocked=locked
			st.BlacklistEntries=append([]projectconfig.Rule(nil),loaded.Entries...)
		})
		webUI.NotifyMCPToolsChanged()
		return nil
	}
	webUI.SetProjectHandlers(
		func() ([]string,error){return macProfiles.ListIDs(true)},
		func(id,_,_ string) error {
			// Mac UI First-time Setup only, NOT agent config.load.
			_,err:=macProfiles.InitializeMacEmpty(id)
			if err!=nil{return err}
			ids,_:=macProfiles.ListIDs(true)
			webUI.UpdateState(func(st *ui.UIState){st.ProjectConfigs=ids})
			return nil
		},
		openMacProfile,
		func(locked bool) error {
			id,_,_:=getMacProfile()
			if id==""{return projectconfig.ErrNotOpen}
			if err:=macProfiles.SetLocked(id,locked);err!=nil{return err}
			webUI.UpdateState(func(st *ui.UIState){st.BlacklistLocked=locked})
			return nil
		},
		func(entries []projectconfig.Rule) error {
			id,_,_:=getMacProfile()
			if id==""{return projectconfig.ErrNotOpen}
			if err:=macProfiles.UpdateMac(id,entries);err!=nil{return err}
			loaded,err:=macProfiles.LoadMac(id);if err!=nil{return err}
			macProfileMu.Lock();macBlacklist=loaded;macProfileMu.Unlock()
			webUI.UpdateState(func(st *ui.UIState){
				st.BlacklistEntries=append([]projectconfig.Rule(nil),loaded.Entries...)
			})
			webUI.NotifyMCPToolsChanged()
			return nil
		},
	)
	// Renaming/deleting an active Mac blacklist immediately invalidates the
	// active project; notify Windows with ID metadata only, no config bytes.
	invalidateMacProject:=func(id string) {
		current,_,_:=getMacProfile()
		if current!=id{return}
		clearMacProfile()
		clientMu.Lock();client:=activeClient;clientMu.Unlock()
		if client!=nil && client.IsConnected(){
			payload,_:=json.Marshal(map[string]any{"project_id":"","loaded":false})
			_,err:=client.RoundTrip(&protocol.Message{
				ID:fmt.Sprintf("project-close-%d",time.Now().UnixNano()),
				Type:protocol.TypeProjectStatus,Payload:payload,
			},15*time.Second)
			if err!=nil{slog.Warn("Could not notify Windows about closed Mac blacklist","error",err)}
		}
	}
	webUI.SetProjectManagementHandlers(
		func(oldID,newID string)error{
			if err:=macProfiles.SaveAs(oldID,newID,true);err!=nil{return err}
			ids,_:=macProfiles.ListIDs(true)
			webUI.UpdateState(func(st *ui.UIState){st.ProjectConfigs=ids})
			return nil
		},
		func(oldID,newID string)error{
			if err:=macProfiles.Rename(oldID,newID,true);err!=nil{return err}
			invalidateMacProject(oldID)
			ids,_:=macProfiles.ListIDs(true)
			webUI.UpdateState(func(st *ui.UIState){st.ProjectConfigs=ids})
			return nil
		},
		func(id string)error{
			if err:=macProfiles.Delete(id,true);err!=nil{return err}
			invalidateMacProject(id)
			ids,_:=macProfiles.ListIDs(true)
			webUI.UpdateState(func(st *ui.UIState){st.ProjectConfigs=ids})
			return nil
		},
	)
	if ids,err:=macProfiles.ListIDs(true);err==nil{
		webUI.UpdateState(func(st *ui.UIState){st.ProjectConfigs=ids})
	}
	var mcpAuthInfo *agentmcp.AuthInfo
	discoveryCtx, cancelDiscovery := context.WithCancel(context.Background())
	var shutdownOnce sync.Once
	shutdown = func() {
		shutdownOnce.Do(func() {
			cancelDiscovery()
			clientMu.Lock()
			client := activeClient
			activeClient = nil
			clientMu.Unlock()
			if client != nil {
				_ = client.Close()
			}
			if mcpAuthInfo != nil {
				if err := agentmcp.RemoveAuthFile(mcpAuthInfo); err != nil {
					slog.Warn("Failed to remove local MCP auth file", "error", err)
				}
			}
			_ = webUI.Close()
		})
	}
	clearClientPairing := func(hostAlreadyUnpaired bool) error {
		clientPairingMu.Lock()
		defer clientPairingMu.Unlock()
		if err := saveClientPairingState(stateStore, "", !hostAlreadyUnpaired); err != nil {
			return fmt.Errorf("clear paired host fingerprint: %w", err)
		}
		pairedHostFingerprint.Store("")
		clientMu.Lock()
		client := activeClient
		clientMu.Unlock()
		if client != nil {
			if err := client.SetTLSConfig(transport.NewClientTLSConfig(certPair, "")); err != nil {
				return fmt.Errorf("remove host TLS pin: %w", err)
			}
		}
		if !hostAlreadyUnpaired && client != nil && client.IsConnected() {
			go func() {
				if err := syncPendingClientUnpair(client, stateStore); err != nil {
					slog.Warn("Client unpair will be retried after reconnect", "error", err)
				}
			}()
		}
		webUI.UpdateState(func(s *ui.UIState) {
			s.PairedFP = ""
			s.WaitingPair = s.Connected
			s.Permissions = nil
		})
		if tray != nil {
			if client != nil && client.IsConnected() {
				tray.SetState(ui.TrayStateWaiting, "LAN Test Bridge (Chờ ghép nối)")
			} else {
				tray.SetState(ui.TrayStateDisconnected, "LAN Test Bridge (Chưa ghép nối)")
			}
		}
		return nil
	}
	connectTarget := func(address string) {
		if address == "" {
			return
		}
		normalized, err := normalizeHostAddress(address)
		if err != nil {
			slog.Warn("Ignoring invalid host address", "address", address, "error", err)
			return
		}
		address = normalized

		clientMu.Lock()
		if address == activeTarget {
			clientMu.Unlock()
			return
		}
		if activeClient != nil && activeClient.IsConnected() {
			clientMu.Unlock()
			return
		}
		oldClient := activeClient
		activeClient = nil
		activeTarget = address
		clientMu.Unlock()
		webUI.UpdateState(func(s *ui.UIState) {
			s.Connected = false
			s.ListenAddr = address
		})
		if oldClient != nil {
			_ = oldClient.Close()
		}

		client := transport.NewClient(address, transport.NewClientTLSConfig(certPair, pairedHostFingerprint.Load().(string)))
		client.SetCallbacks(func() {
			clearMacProfile()
			slog.Info("Connected to bridge host", "address", address)
			webUI.UpdateState(func(s *ui.UIState) {
				s.Connected = true
				s.ListenAddr = address
				s.PairedFP = pairedHostFingerprint.Load().(string)
				s.WaitingPair = s.PairedFP == ""
			})
			webUI.NotifyMCPToolsChanged()
			if err := persistLastConnectedAddress(stateStore, address); err != nil {
				slog.Error("Failed to save connected host address", "error", err)
			}
			go func() {
				if err := syncPendingClientUnpair(client, stateStore); err != nil {
					slog.Warn("Pending unpair notification will be retried after reconnect", "error", err)
				}
			}()
			if tray != nil {
				if pairedHostFingerprint.Load().(string) == "" {
					tray.SetState(ui.TrayStateWaiting, "LAN Test Bridge (Chờ ghép nối)")
				} else {
					tray.SetState(ui.TrayStateReady, "LAN Test Bridge (Đã kết nối)")
				}
			}
		}, func() {
			clearMacProfile()
			slog.Warn("Bridge host connection dropped", "address", address)
			webUI.UpdateState(func(s *ui.UIState) {
				s.Connected = false
			})
			webUI.NotifyMCPToolsChanged()
			if tray != nil {
				tray.SetState(ui.TrayStateDisconnected, "LAN Test Bridge (Mất kết nối)")
			}
		}, func(msg *protocol.Message) {
			slog.Info("Bridge host notification", "type", msg.Type)
			if msg.Type == protocol.TypeChat {
				var message ui.ChatMessage
				if err := json.Unmarshal(msg.Payload, &message); err != nil {
					slog.Warn("Invalid chat message from host", "error", err)
					return
				}
				message.From = "Windows"
				webUI.AddChatMessage(message)
				return
			}
			if msg.Type == protocol.TypeUIState {
				var update struct {
					Permissions []store.PermissionGrant `json:"permissions"`
					ProjectID string `json:"project_id"`
					ProjectReady bool `json:"project_ready"`
					ProjectError string `json:"project_error"`
				}
				if err := json.Unmarshal(msg.Payload, &update); err != nil {
					slog.Warn("Invalid host UI state update", "error", err)
					return
				}
				id,_,_:=getMacProfile()
				ready:=id!="" && id==update.ProjectID && update.ProjectReady
				macProfileMu.Lock();macProjectReady=ready;macProfileMu.Unlock()
				webUI.UpdateState(func(s *ui.UIState) {
					s.Permissions = update.Permissions
					s.ProjectReady=ready
					if ready {
						s.ProjectError=""
					}else if id!="" && update.ProjectID!="" && id!=update.ProjectID {
						s.ProjectError=projectconfig.ErrMismatch.Error()
					}else {
						s.ProjectError=update.ProjectError
					}
				})
				webUI.NotifyMCPToolsChanged()
				return
			}
			if msg.Type == protocol.TypeUnpair && pairedHostFingerprint.Load().(string) != "" {
				if err := clearClientPairing(true); err != nil {
					slog.Error("Failed to apply host unpair notification", "error", err)
				}
			}
			if msg.Type == protocol.TypeMCPCall {
				var notification struct {
					Tool string `json:"tool"`
				}
				if err := json.Unmarshal(msg.Payload, &notification); err != nil {
					slog.Warn("Invalid MCP notification from host", "error", err)
					return
				}
				if notification.Tool == "__tools_changed" {
					webUI.NotifyMCPToolsChanged()
				}
			}
		})

		clientMu.Lock()
		if activeTarget != address {
			clientMu.Unlock()
			_ = client.Close()
			return
		}
		activeClient = client
		clientMu.Unlock()

		if err := client.Connect(); err != nil {
			slog.Warn("Initial host connection failed; reconnect will continue", "address", address, "error", err)
		}
		client.StartAutoReconnect()
	}

	webUI.SetChatHandler(func(text string) error {
		clientMu.Lock()
		client := activeClient
		clientMu.Unlock()
		if client == nil || !client.IsConnected() {
			return errors.New("connect to the host before using chat")
		}
		if pairedHostFingerprint.Load().(string) == "" {
			return errors.New("pair with the host before using chat")
		}
		message := ui.ChatMessage{Text: text, SenderKind: ui.ChatSenderMacUser}
		payload, err := json.Marshal(message)
		if err != nil {
			return fmt.Errorf("encode chat message: %w", err)
		}
		response, err := client.RoundTrip(&protocol.Message{
			ID:      fmt.Sprintf("chat-%d", time.Now().UnixNano()),
			Type:    protocol.TypeChat,
			Payload: payload,
		}, 15*time.Second)
		if err != nil {
			return fmt.Errorf("send chat message: %w", err)
		}
		if response.Error != nil {
			return response.Error
		}
		if response.Type != protocol.TypeChat {
			return fmt.Errorf("unexpected chat response type %q", response.Type)
		}
		var confirmed ui.ChatMessage
		if err := json.Unmarshal(response.Payload, &confirmed); err != nil {
			return fmt.Errorf("decode chat confirmation: %w", err)
		}
		webUI.AddChatMessage(confirmed)
		return nil
	})
	webUI.SetAgentChatHandler(func(text string) error {
		clientMu.Lock()
		client := activeClient
		clientMu.Unlock()
		if client == nil || !client.IsConnected() {
			return errors.New("connect to the host before using agent chat")
		}
		if pairedHostFingerprint.Load().(string) == "" {
			return errors.New("pair with the host before using agent chat")
		}
		message := ui.ChatMessage{Text: text, SenderKind: ui.ChatSenderMacAgent}
		payload, err := json.Marshal(message)
		if err != nil {
			return fmt.Errorf("encode agent chat message: %w", err)
		}
		response, err := client.RoundTrip(&protocol.Message{
			ID:      fmt.Sprintf("agent-chat-%d", time.Now().UnixNano()),
			Type:    protocol.TypeChat,
			Payload: payload,
		}, 15*time.Second)
		if err != nil {
			return fmt.Errorf("agent chat delivery status is unknown: %w", err)
		}
		if response.Error != nil {
			return response.Error
		}
		if response.Type != protocol.TypeChat {
			return fmt.Errorf("unexpected agent chat response type %q", response.Type)
		}
		var confirmed ui.ChatMessage
		if err := json.Unmarshal(response.Payload, &confirmed); err != nil {
			return fmt.Errorf("decode agent chat confirmation: %w", err)
		}
		webUI.AddChatMessage(confirmed)
		return nil
	})
	webUI.SetMCPProviderHandler(func(ctx context.Context, provider string) ([]ui.MCPProviderState, error) {
		if agentConfigManager == nil {
			return initialAgentConfigStates, agentConfigInitErr
		}
		snapshot, err := agentConfigManager.Select(ctx, agentconfig.Provider(provider))
		if err != nil {
			snapshot = agentConfigManager.Inspect()
			slog.Warn("MCP provider change failed", "provider", provider, "error", err)
		}
		return toUIAgentConfigStates(snapshot), err
	})

	webUI.SetHostConnectionHandler(func(address string) error {
		normalized, err := normalizeHostAddress(address)
		if err != nil {
			return err
		}
		clientMu.Lock()
		connectedToDifferentHost := activeClient != nil && activeClient.IsConnected() && activeTarget != normalized
		clientMu.Unlock()
		if connectedToDifferentHost {
			return errors.New("disconnect from the current host before selecting another address")
		}
		go connectTarget(normalized)
		return nil
	})
	webUI.SetPairCodeHandler(func(code string) error {
		clientPairingMu.Lock()
		defer clientPairingMu.Unlock()
		clientMu.Lock()
		client := activeClient
		clientMu.Unlock()
		if client == nil || !client.IsConnected() {
			return errors.New("connect to the host before entering a pairing code")
		}
		if pairedHostFingerprint.Load().(string) != "" {
			return errors.New("this client is already paired; unpair before pairing again")
		}
		clientState, err := stateStore.Load()
		if err != nil {
			return fmt.Errorf("load client pairing state: %w", err)
		}
		if clientState.PendingUnpair {
			return errors.New("waiting for the host to confirm the previous unpair")
		}
		hostFingerprint, err := performClientPairing(client, code, certPair.Fingerprint)
		if err != nil {
			return err
		}
		if err := client.SetTLSConfig(transport.NewClientTLSConfig(certPair, hostFingerprint)); err != nil {
			return fmt.Errorf("enable host certificate pinning: %w", err)
		}
		if err := saveClientPairingState(stateStore, hostFingerprint, false); err != nil {
			_ = client.SetTLSConfig(transport.NewClientTLSConfig(certPair, ""))
			return fmt.Errorf("save paired host fingerprint: %w", err)
		}
		pairedHostFingerprint.Store(hostFingerprint)
		webUI.UpdateState(func(s *ui.UIState) {
			s.PairedFP = hostFingerprint
			s.WaitingPair = false
		})
		if tray != nil {
			tray.SetState(ui.TrayStateReady, "LAN Test Bridge (Đã kết nối, sẵn sàng)")
		}
		return nil
	})
	webUI.SetMCPHandlers(func(toolName string, args map[string]any) (any, error) {
		clientMu.Lock()
		client := activeClient
		address := activeTarget
		clientMu.Unlock()
		if toolName == "bridge_status" {
			id,_,ready:=getMacProfile()
			return map[string]any{
				"connected": client != nil && client.IsConnected(),
				"host":      address,
				"paired":    pairedHostFingerprint.Load().(string) != "",
				"project_id":id,
				"project_ready":ready,
			}, nil
		}
		// Local agent Config tools have no Windows file-system access.
		// Open is read-only: only the Mac app Setup can create empty files.
		switch toolName {
		case "windows_config":
			if client==nil || !client.IsConnected(){return nil,errors.New("chưa kết nối tới host Windows")}
			payload,err:=json.Marshal(args);if err!=nil{return nil,err}
			resp,err:=client.RoundTrip(&protocol.Message{
				ID:fmt.Sprintf("remote-%d",time.Now().UnixNano()),
				Type:protocol.TypeRemoteConfig,Payload:payload,
			},30*time.Second)
			if err!=nil{return nil,err}
			if resp.Error!=nil{return nil,resp.Error}
			var out any
			if err:=json.Unmarshal(resp.Payload,&out);err!=nil{return nil,err}
			return out,nil
		case "config_list":
			ids,err:=macProfiles.ListIDs(true)
			if err!=nil{return nil,err}
			return map[string]any{"projects":ids},nil
		case "config_current":
			id,b,ready:=getMacProfile()
			return map[string]any{"project_id":id,"loaded":id!="","ready":ready,"entries":len(b.Entries)},nil
		case "config_load":
			id,_:=args["project_id"].(string)
			if err:=openMacProfile(id);err!=nil{return nil,err}
			_,b,ready:=getMacProfile()
			return map[string]any{"project_id":id,"loaded":true,"ready":ready,"entries":len(b.Entries)},nil
		case "blacklist_edit":
			id,_,_:=getMacProfile()
			if id=="" {return nil,projectconfig.ErrNotOpen}
			raw,ok:=args["entries"]
			if !ok {return nil,errors.New("blacklist entries required")}
			encoded,err:=json.Marshal(raw);if err!=nil{return nil,err}
			var entries []projectconfig.Rule
			if err:=json.Unmarshal(encoded,&entries);err!=nil{return nil,err}
			// The human must have manually Unlocked in Mac UI.
			if err:=macProfiles.UpdateMac(id,entries);err!=nil{return nil,err}
			updated,err:=macProfiles.LoadMac(id);if err!=nil{return nil,err}
			macProfileMu.Lock();macBlacklist=updated;macProfileMu.Unlock()
			webUI.UpdateState(func(st *ui.UIState){st.BlacklistEntries=append([]projectconfig.Rule(nil),updated.Entries...)})
			return map[string]any{"updated":true,"entries":len(updated.Entries)},nil
		}
		// A missing/unloaded blacklist MUST NOT silently become an empty
		// deny list. Project tools and operations need both config files.
		if toolName!="request_access" && toolName!="chat_send" {
			id,blacklist,ready:=getMacProfile()
			if err:=authorizeMacProjectCall(id,blacklist,ready,toolName,args);err!=nil{
				return nil,err
			}
		}
		switch toolName {
		case "request_access", "list_dir", "file_info", "read_file", "sync_files_batch", "run_command", "command_result":
		default:
			if !isQualifiedProjectTool(toolName) {
				return nil, fmt.Errorf("MCP tool %q is not available", toolName)
			}
		}
		if client == nil || !client.IsConnected() {
			return nil, errors.New("LAN Bridge trên Mac chưa mở hoặc chưa kết nối tới host")
		}
		return forwardAgentMCPTool(client, toolName, args)
	}, func() ([]map[string]any, error) {
		_,_,ready:=getMacProfile()
		if !ready {return nil,projectconfig.ErrNotOpen}
		clientMu.Lock()
		client := activeClient
		clientMu.Unlock()
		if client == nil || !client.IsConnected() {
			return nil, errors.New("LAN Bridge trên Mac chưa mở hoặc chưa kết nối tới host")
		}
		result, err := forwardAgentMCPTool(client, "__list_tools", nil)
		if err != nil {
			return nil, err
		}
		resultMap, ok := result.(map[string]any)
		if !ok {
			return nil, errors.New("host returned an invalid project tool list")
		}
		var warnings []string
		if rawErrors, ok := resultMap["errors"].([]any); ok {
			for _, rawError := range rawErrors {
				if message, ok := rawError.(string); ok {
					warnings = append(warnings, message)
					slog.Warn("Project MCP tool list is incomplete", "error", message)
				}
			}
		}
		rawTools, ok := resultMap["tools"].([]any)
		if !ok {
			return nil, errors.New("host returned an invalid project tool list")
		}
		webUI.UpdateState(func(s *ui.UIState) {
			s.MCPWarnings = warnings
		})
		tools := make([]map[string]any, 0, len(rawTools))
		for _, rawTool := range rawTools {
			tool, ok := rawTool.(map[string]any)
			if !ok {
				return nil, errors.New("host returned a malformed project tool")
			}
			tools = append(tools, tool)
		}
		return tools, nil
	})

	webUI.SetCallbacks(nil, func() error {
		return clearClientPairing(false)
	}, func() {
		if tray != nil {
			tray.Quit()
			return
		}
		shutdown()
		os.Exit(0)
	})
	mcpAuthInfo, err = agentmcp.WriteAuthFile(webUI.Port(), webUI.Token())
	if err != nil {
		slog.Error("Failed to publish local MCP endpoint; bridge mcp will not be able to connect", "error", err)
	}
	webUI.Start()
	fmt.Printf("[LAN Bridge Client] Web UI: %s\n", webUI.URL())

	if target != "" {
		connectTarget(target)
	}
	if *hostAddr == "" {
		go func() {
			err := transport.BrowseHosts(discoveryCtx, func(address, hostname string) {
				clientMu.Lock()
				client := activeClient
				currentTarget := activeTarget
				clientMu.Unlock()
				if address != currentTarget && (client == nil || !client.IsConnected()) {
					slog.Info("Discovered bridge host", "hostname", hostname, "address", address)
					connectTarget(address)
				}
			})
			if err != nil {
				slog.Warn("mDNS discovery stopped; enter the host IP:port in the client UI or use --host", "error", err)
			}
		}()
	}

	if tray != nil {
		tray.Run()
		shutdown()
		return
	}
	select {}
}

func normalizeHostAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	if ip := net.ParseIP(address); ip != nil {
		return net.JoinHostPort(ip.String(), strconv.Itoa(DefaultPort)), nil
	}
	if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
		host := strings.TrimSuffix(strings.TrimPrefix(address, "["), "]")
		if ip := net.ParseIP(host); ip != nil {
			return net.JoinHostPort(ip.String(), strconv.Itoa(DefaultPort)), nil
		}
	}

	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("expected an IP address with an optional :port: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "", fmt.Errorf("host %q is not an IP address", host)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("port %q must be between 1 and 65535", portText)
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(port)), nil
}

func hostListenAddresses(port int) string {
	interfaces, err := net.Interfaces()
	if err != nil {
		slog.Warn("Failed to inspect LAN interfaces", "error", err)
		return formatHostAddresses(nil, port)
	}

	var ips []net.IP
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			slog.Warn("Failed to read interface addresses", "interface", iface.Name, "error", err)
			continue
		}
		for _, addr := range addrs {
			switch address := addr.(type) {
			case *net.IPNet:
				ips = append(ips, address.IP)
			case *net.IPAddr:
				ips = append(ips, address.IP)
			}
		}
	}
	return formatHostAddresses(ips, port)
}

func formatHostAddresses(ips []net.IP, port int) string {
	addresses := make(map[string]struct{})
	for _, ip := range ips {
		if ip == nil || !ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		addresses[net.JoinHostPort(ip.String(), strconv.Itoa(port))] = struct{}{}
	}
	formatted := make([]string, 0, len(addresses))
	for address := range addresses {
		formatted = append(formatted, address)
	}
	sort.Strings(formatted)
	if len(formatted) == 0 {
		return fmt.Sprintf("Chưa tìm thấy IP LAN (cổng %d)", port)
	}
	return strings.Join(formatted, ", ")
}

func persistLastConnectedAddress(stateStore *store.StateStore, address string) error {
	state, err := stateStore.Load()
	if err != nil {
		return err
	}
	state.LastConnectedAddr = address
	return stateStore.Save(state)
}

func runMCP(args []string) {
	if err := runMCPWithLock(args, agentlock.Acquire, os.Stdin, os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}

func runMCPWithLock(
	args []string,
	acquire func() (*agentlock.Lock, error),
	input io.Reader,
	output io.Writer,
	errorOutput io.Writer,
) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(errorOutput)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse MCP flags: %w", err)
	}

	lock, err := acquire()
	if err != nil {
		fmt.Fprintf(errorOutput, "[LAN Bridge MCP] Không thể khởi động: %v\n", err)
		return err
	}
	fmt.Fprintln(errorOutput, "[LAN Bridge MCP] Stdio bridge starting for agent...")
	server := agentmcp.NewStdioServer(input, output)
	runErr := server.Run()
	releaseErr := lock.Close()
	if runErr != nil {
		fmt.Fprintf(errorOutput, "[LAN Bridge MCP] Stdio loop error: %v\n", runErr)
	}
	if releaseErr != nil {
		fmt.Fprintf(errorOutput, "[LAN Bridge MCP] Failed to release process lock: %v\n", releaseErr)
	}
	return errors.Join(runErr, releaseErr)
}

func toUIAgentConfigStates(snapshot agentconfig.Snapshot) []ui.MCPProviderState {
	return []ui.MCPProviderState{
		{Provider: string(snapshot.Claude.Provider), Status: string(snapshot.Claude.Status), Message: snapshot.Claude.Message},
		{Provider: string(snapshot.Codex.Provider), Status: string(snapshot.Codex.Status), Message: snapshot.Codex.Message},
	}
}

func selectedAgentProvider(snapshot agentconfig.Snapshot) string {
	if snapshot.Claude.Status == agentconfig.StatusManagedActive && snapshot.Codex.Status == agentconfig.StatusAbsent {
		return string(agentconfig.ProviderClaude)
	}
	if snapshot.Codex.Status == agentconfig.StatusManagedActive && snapshot.Claude.Status == agentconfig.StatusAbsent {
		return string(agentconfig.ProviderCodex)
	}
	return ""
}

// defaultGrantedDirectory never guesses when several folders share a capability.
func defaultGrantedDirectory(grants []store.PermissionGrant, capability string) (string,error) {
 var selected string
 for _,grant:=range grants {
  for _,cap:=range grant.Capabilities {
   if cap!=capability {continue}
   if selected!="" && selected!=grant.Path {return "",fmt.Errorf("nhiều folder có quyền %s; hãy chỉ định đường dẫn cụ thể",capability)}
   selected=grant.Path
   break
  }
 }
 if selected=="" {return "",fmt.Errorf("chưa có folder được cấp quyền %s",capability)}
 return selected,nil
}

// Prefer the unique granted directory containing the configured MCP executable.
// If it cannot be identified, use a single unambiguous MCP grant.
func mcpGrantedDirectory(grants []store.PermissionGrant, command []string) (string,error) {
 if len(command)>0 && filepath.IsAbs(command[0]) {
  matching:=make([]store.PermissionGrant,0)
  for _,grant:=range grants {
   rel,err:=filepath.Rel(grant.Path,command[0])
   if err==nil && rel!=".." && !strings.HasPrefix(rel,".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
    matching=append(matching,grant)
   }
  }
  if dir,err:=defaultGrantedDirectory(matching,permissions.CapMCP);err==nil{return dir,nil}
 }
 return defaultGrantedDirectory(grants,permissions.CapMCP)
}
