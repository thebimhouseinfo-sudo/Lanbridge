package permissions

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

func TestUnpairClearsPeerFingerprintAndActiveGrants(t *testing.T) {
	root := t.TempDir()
	stateStore, err := store.NewStateStoreAt(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := stateStore.Save(&store.AppState{PairedPeerFingerprint: "trusted-peer"}); err != nil {
		t.Fatal(err)
	}
	gate, err := NewGatekeeper(stateStore, &store.HostConfig{
		Projects: []store.ProjectConfig{{Name: "test", Root: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.AddGrant(root, []string{CapRead}); err != nil {
		t.Fatal(err)
	}
	if !gate.HasPermission(root, CapRead) {
		t.Fatal("test grant was not active before unpair")
	}

	if err := gate.Unpair(); err != nil {
		t.Fatal(err)
	}
	if gate.HasPermission(root, CapRead) || len(gate.ListGrants()) != 0 {
		t.Fatal("unpair left an in-memory permission grant active")
	}
	state, err := stateStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.PairedPeerFingerprint != "" || len(state.Permissions) != 0 {
		t.Fatalf("unpair did not clear persisted peer and grants: %+v", state)
	}
}

func TestWindowsActualGrantMayExceedRequestAndSyncOutsideProject(t *testing.T) {
	project:=t.TempDir()
	outside:=t.TempDir()
	stateStore,err:=store.NewStateStoreAt(filepath.Join(t.TempDir(),"state.json"))
	if err!=nil{t.Fatal(err)}
	gate,err:=NewGatekeeper(stateStore,&store.HostConfig{Projects: []store.ProjectConfig{{Name:"app",Root:project}}})
	if err!=nil{t.Fatal(err)}
	if err:=gate.BeginRequest("req1",outside,[]string{CapRead});err!=nil{t.Fatal(err)}
	if err:=gate.RespondRequest("req1",true,[]string{CapRead,CapSync});err!=nil{t.Fatal(err)}
	got,approved,err:=gate.WaitForRequest(context.Background(),"req1")
	if err!=nil||!approved||len(got)!=2{t.Fatalf("actual grant: %v %v %v",got,approved,err)}
	if !gate.HasPermission(outside,CapSync){t.Fatal("actual Sync grant should authorize outside project")}
	state,err:=stateStore.Load()
	if err!=nil||len(state.Permissions)!=1||len(state.Permissions[0].Capabilities)!=2{t.Fatalf("not persisted: %+v %v",state,err)}
	if err:=gate.RevokeGrant(outside);err!=nil{t.Fatal(err)}
	if gate.HasPermission(outside,CapSync){t.Fatal("revoke not effective")}
}

func TestWindowsActualGrantMayBeNarrowerAndRejectUnknownCapability(t *testing.T) {
	project:=t.TempDir()
	stateStore,_:=store.NewStateStoreAt(filepath.Join(t.TempDir(),"state.json"))
	gate,err:=NewGatekeeper(stateStore,&store.HostConfig{Projects:[]store.ProjectConfig{{Name:"app",Root:project}}})
	if err!=nil{t.Fatal(err)}
	if err:=gate.BeginRequest("req1",project,[]string{CapRead,CapSync});err!=nil{t.Fatal(err)}
	if err:=gate.RespondRequest("req1",true,[]string{CapRead});err!=nil{t.Fatal(err)}
	got,ok,err:=gate.WaitForRequest(context.Background(),"req1")
	if err!=nil||!ok||len(got)!=1||got[0]!=CapRead{t.Fatalf("actual grant narrow: %v %v %v",got,ok,err)}
	if gate.HasPermission(project,CapSync){t.Fatal("Sync was not granted")}
	if err:=gate.BeginRequest("req2",project,[]string{CapRead});err!=nil{t.Fatal(err)}
	if err:=gate.RespondRequest("req2",true,[]string{"unknown-cap"});err==nil{t.Fatal("unknown grant should fail")}
}

func TestActiveWindowsProjectExecAndMCPDoNotRequireProjectRootPath(t *testing.T){
 storePath:=filepath.Join(t.TempDir(),"state.json")
 stateStore,err:=store.NewStateStoreAt(storePath);if err!=nil{t.Fatal(err)}
 project:=t.TempDir()
 other:=t.TempDir()
 gate,err:=NewGatekeeper(stateStore,&store.HostConfig{Projects:[]store.ProjectConfig{{Name:"app",Root:project}}})
 if err!=nil{t.Fatal(err)}
 if err:=gate.AddGrant(other,[]string{CapExecute,CapMCP});err!=nil{t.Fatal(err)}
 if !gate.HasCapability(CapExecute)||!gate.HasCapability(CapMCP){t.Fatal("Windows project grants not effective outside root")}
 if gate.HasCapability(CapRead){t.Fatal("Read was not granted")}
 if err:=gate.RevokeGrant(other);err!=nil{t.Fatal(err)}
 if gate.HasCapability(CapExecute)||gate.HasCapability(CapMCP){t.Fatal("revoked project grants still active")}
}
