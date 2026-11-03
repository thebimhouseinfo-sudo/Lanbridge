package projectconfig

import (
 "errors"
 "os"
 "path/filepath"
 "reflect"
 "testing"
)

func TestFirstTimeEmptyAndExistingNeverOverwritten(t *testing.T) {
 s,err:=New(t.TempDir());if err!=nil {t.Fatal(err)}
 created,err:=s.InitializeMacEmpty("windows-app")
 if err!=nil||!created {t.Fatalf("create: %v %v",created,err)}
 got,err:=s.LoadMac("windows-app")
 if err!=nil||got.Entries==nil||len(got.Entries)!=0 {t.Fatalf("empty load: %#v %v",got,err)}
 original:=[]Rule{{Kind:"tool",Tool:"windows-app__tool"}}
 if err:=s.UpdateMac("windows-app",original);!errors.Is(err,ErrLocked){t.Fatalf("new project must start Locked: %v",err)}
 if err:=s.SetLocked("windows-app",false);err!=nil{t.Fatal(err)}
 if err:=s.UpdateMac("windows-app",original);err!=nil {t.Fatal(err)}
 path,_:=s.FilePath("windows-app",true)
 b1,err:=os.ReadFile(path);if err!=nil{t.Fatal(err)}
 created,err=s.InitializeMacEmpty("windows-app")
 if created||err!=nil{t.Fatalf("overwrite: %v %v",created,err)}
 b2,err:=os.ReadFile(path);if err!=nil{t.Fatal(err)}
 if string(b1)!=string(b2) {t.Fatal("first setup overwrote existing entries")}
 got,err=s.LoadMac("windows-app")
 if err!=nil||!reflect.DeepEqual(got.Entries,original){t.Fatalf("%#v %v",got,err)}
}

func TestMissingAndInvalidAreNeverTreatedAsEmpty(t *testing.T) {
 s,_:=New(t.TempDir())
 if _,err:=s.LoadMac("new-app");!errors.Is(err,ErrNotFound) {t.Fatalf("missing file: %v",err)}
 path,_:=s.FilePath("new-app",true)
 if _,err:=os.Stat(path);!errors.Is(err,os.ErrNotExist){t.Fatal("load created missing file")}
 if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(path,[]byte("{invalid"),0600);err!=nil{t.Fatal(err)}
 if _,err:=s.LoadMac("new-app");!errors.Is(err,ErrInvalid){t.Fatalf("corrupt: %v",err)}
 if err:=os.WriteFile(path,[]byte(`{"schema_version":1,"project_id":"new-app"}`),0600);err!=nil{t.Fatal(err)}
 if _,err:=s.LoadMac("new-app");!errors.Is(err,ErrInvalid){t.Fatalf("null entries: %v",err)}
}

func TestLockPreventsEditAndPersists(t *testing.T) {
 root:=t.TempDir();s,_:=New(root)
 if _,err:=s.InitializeMacEmpty("alpha");err!=nil{t.Fatal(err)}
 if err:=s.SetLocked("alpha",true);err!=nil{t.Fatal(err)}
 afterRestart,_:=New(root)
 locked,err:=afterRestart.IsLocked("alpha")
 if err!=nil||!locked{t.Fatalf("lock persistence: %v %v",locked,err)}
 if err:=afterRestart.UpdateMac("alpha",[]Rule{{Kind:"tool",Tool:"alpha__danger"}});!errors.Is(err,ErrLocked){t.Fatalf("must reject locked edit: %v",err)}
 got,err:=afterRestart.LoadMac("alpha")
 if err!=nil||len(got.Entries)!=0{t.Fatal("locked update modified file")}
 if err:=afterRestart.SetLocked("alpha",false);err!=nil{t.Fatal(err)}
 if err:=afterRestart.UpdateMac("alpha",[]Rule{{Kind:"tool",Tool:"alpha__danger"}});err!=nil{t.Fatal(err)}
 got,err=afterRestart.LoadMac("alpha")
 if err!=nil||len(got.Entries)!=1{t.Fatal("unlocked edit failed")}
}

func TestWindowsPermissionsIndependentAndProjectIDValidation(t *testing.T) {
 root:=t.TempDir()
 windows,_:=New(filepath.Join(root,"windows"))
 mac,_:=New(filepath.Join(root,"mac"))
 err:=windows.SaveWindows("project-a",WindowsPermissions{
  SchemaVersion:1,ProjectID:"project-a",Folders:[]FolderGrant{{Path:`C:\Dev\App`,Capabilities:[]string{"read","sync"}}},
 })
 if err!=nil{t.Fatal(err)}
 if _,err=mac.LoadMac("project-a");!errors.Is(err,ErrNotFound){t.Fatalf("config was synced implicitly: %v",err)}
 if _,err=windows.LoadWindows("project-a");err!=nil{t.Fatal(err)}
 if _,err=windows.LoadWindows("project-b");!errors.Is(err,ErrNotFound){t.Fatalf("project identity mismatch: %v",err)}
 if _,err=mac.InitializeMacEmpty("../escape");!errors.Is(err,ErrInvalid){t.Fatalf("invalid ID accepted: %v",err)}
 if err=windows.SaveWindows("project-a",WindowsPermissions{SchemaVersion:1,ProjectID:"project-b"});!errors.Is(err,ErrInvalid){t.Fatalf("mismatched ID accepted: %v",err)}
}

func TestUnsupportedRuleShapeNeverSaved(t *testing.T) {
 s,_:=New(t.TempDir())
 if _,err:=s.InitializeMacEmpty("sample");err!=nil{t.Fatal(err)}
 if err:=s.SetLocked("sample",false);err!=nil{t.Fatal(err)}
 if err:=s.UpdateMac("sample",[]Rule{{Kind:"argument",Tool:"x"}});!errors.Is(err,ErrInvalid){t.Fatalf("missing arg path %v",err)}
 got,err:=s.LoadMac("sample");if err!=nil||len(got.Entries)!=0{t.Fatal("invalid rule persisted")}
}
