package projectconfig

import (
 "os"
 "path/filepath"
 "testing"
)

func TestPrivateBridgeConfigPaths(t *testing.T){
 root:=t.TempDir()
 file:=filepath.Join(root,"profiles","app","permissions.json")
 if !IsBridgePrivatePath(file,root){t.Fatal("config path not protected")}
 for _,name:=range []string{"config.json","state.json","blacklist-locks.json","maintenance.json"}{
  if !IsBridgePrivatePath(filepath.Join(root,name),root){t.Fatalf("Bridge config %s was not protected",name)}
 }
 if !IsBridgePrivatePath(filepath.Join(root,"mcp-secrets","generic.bin"),root){t.Fatal("DPAPI secret-store directory not protected")}
 if IsBridgePrivatePath(filepath.Join(root,"logs","app.log"),root){t.Fatal("unrelated App Data log must remain readable when granted")}
 if IsBridgePrivatePath(filepath.Join(t.TempDir(),"ordinary.go"),root){t.Fatal("unrelated file blocked")}
 if IsBridgePrivatePath(filepath.Join(root,"..","other","test.go"),root){t.Fatal("traversal outside root blocked")}
}
func TestPrivateConfigThroughSymlink(t *testing.T){
 root:=t.TempDir()
 nested:=filepath.Join(root,"profiles","app")
 if err:=os.MkdirAll(nested,0700);err!=nil{t.Fatal(err)}
 external:=t.TempDir()
 if err:=os.Symlink(nested,filepath.Join(external,"shortcut"));err!=nil{t.Skipf("symlink unavailable: %v",err)}
 if !IsBridgePrivatePath(filepath.Join(external,"shortcut","command-blacklist.json"),root){t.Fatal("symlink into app data bypassed check")}
}
