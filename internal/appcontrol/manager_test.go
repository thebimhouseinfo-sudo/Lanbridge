package appcontrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

type fakeController struct {
	started string
	stopped string
	result  StopResult
}

func (f *fakeController) Start(path string) error {
	f.started = path
	return nil
}

func (f *fakeController) Stop(path string) (StopResult, error) {
	f.stopped = path
	return f.result, nil
}

func TestManagerRequiresSessionPermissionAndConfiguredApp(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "app.exe")
	if err := os.WriteFile(executable, []byte("app"), 0600); err != nil {
		t.Fatal(err)
	}
	controller := &fakeController{}
	manager := newManager([]store.AppConfig{{Name: "CAD", Path: executable}}, controller)

	if _, err := manager.Control("0", "start"); err == nil {
		t.Fatal("Start/Stop permission was not disabled at session start")
	}
	if err := manager.SetPermission("unknown", true); err == nil {
		t.Fatal("unknown app permission was accepted")
	}
	if err := manager.SetPermission("0", true); err != nil {
		t.Fatal(err)
	}
	message, err := manager.Control("0", "start")
	if err != nil {
		t.Fatal(err)
	}
	if controller.started != executable || !strings.Contains(message, "CAD") {
		t.Fatalf("start path=%q, message=%q", controller.started, message)
	}
	controller.result = StopResult{ProcessesFound: 1, MessagesSent: 1, StillRunning: 1}
	message, err = manager.Control("0", "stop")
	if err != nil {
		t.Fatal(err)
	}
	if controller.stopped != executable || !strings.Contains(message, "still running") {
		t.Fatalf("stop path=%q, message=%q", controller.stopped, message)
	}

	restarted := newManager([]store.AppConfig{{Name: "CAD", Path: executable}}, controller)
	if restarted.List()[0].CanStartStop {
		t.Fatal("session permission survived manager restart")
	}
}

func TestLegacyConfigPermissionDoesNotPersist(t *testing.T) {
	var app store.AppConfig
	if err := json.Unmarshal([]byte(`{"name":"CAD","path":"C:\\CAD\\cad.exe","can_start_stop":true}`), &app); err != nil {
		t.Fatal(err)
	}
	manager := NewManager([]store.AppConfig{app})
	if manager.List()[0].CanStartStop {
		t.Fatal("config.json unexpectedly enabled session-only Start/Stop permission")
	}
}

func TestManagerRejectsInvalidActionAndExecutable(t *testing.T) {
	controller := &fakeController{}
	manager := newManager([]store.AppConfig{
		{Name: "Missing path"},
		{Name: "Directory", Path: t.TempDir()},
	}, controller)
	if err := manager.SetPermission("0", true); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Control("0", "start"); err == nil {
		t.Fatal("empty executable path was accepted")
	}
	if err := manager.SetPermission("1", true); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Control("1", "start"); err == nil {
		t.Fatal("directory was accepted as an executable")
	}
	if _, err := manager.Control("1", "restart"); err == nil {
		t.Fatal("unknown app action was accepted")
	}
}

func TestProjectSwitchReplacesAppsAndGranularRights(t *testing.T) {
 exe:=filepath.Join(t.TempDir(),"example.exe")
 if err:=os.WriteFile(exe,[]byte("exe"),0600);err!=nil{t.Fatal(err)}
 controller:=&fakeController{}
 manager:=newManager(nil,controller)
 if err:=manager.AddApp("Example",exe);err!=nil{t.Fatal(err)}
 if err:=manager.SetActionPermissions("0",true,false);err!=nil{t.Fatal(err)}
 info:=manager.List()
 if len(info)!=1||!info[0].CanStart||info[0].CanStop{t.Fatalf("wrong rights: %+v",info)}
 if _,err:=manager.Control("0","start");err!=nil{t.Fatal(err)}
 if _,err:=manager.Control("0","stop");err==nil{t.Fatal("Stop was not granted")}
 if err:=manager.ReplaceApps([]store.AppConfig{{Name:"Other",Path:exe}},[]ActionPermissions{{Start:false,Stop:true}});err!=nil{t.Fatal(err)}
 if _,err:=manager.Control("0","start");err==nil{t.Fatal("Start permission leaked across profile switch")}
 if _,err:=manager.Control("0","stop");err!=nil{t.Fatal(err)}
 if err:=manager.RemoveApp("0");err!=nil{t.Fatal(err)}
 if len(manager.List())!=0{t.Fatal("app not removed")}
}
