package projectconfig

import (
 "errors"
 "os"
 "testing"
)

func TestWindowsProfileSaveAsRenameDeleteAreLocal(t *testing.T){
 s,_:=New(t.TempDir())
 original:=WindowsPermissions{SchemaVersion:1,ProjectID:"origin",Workspace:"C:/Dev/App",Folders:[]FolderGrant{}}
 if _,err:=s.InitializeWindows("origin",original);err!=nil{t.Fatal(err)}
 if err:=s.SaveAs("origin","copy",false);err!=nil{t.Fatal(err)}
 p,err:=s.LoadWindows("copy");if err!=nil||p.ProjectID!="copy"||p.Workspace!="C:/Dev/App"{t.Fatalf("%+v %v",p,err)}
 if err:=s.Rename("copy","renamed",false);err!=nil{t.Fatal(err)}
 if _,err:=s.LoadWindows("copy");!errors.Is(err,ErrNotFound){t.Fatalf("old ID still exists %v",err)}
 if _,err:=s.LoadWindows("renamed");err!=nil{t.Fatal(err)}
 if err:=s.Delete("renamed",false);err!=nil{t.Fatal(err)}
 if _,err:=s.LoadWindows("origin");err!=nil{t.Fatal("original unexpectedly modified",err)}
}

func TestMacLockedCannotRenameSaveAsDelete(t *testing.T) {
 s,_:=New(t.TempDir())
 if _,err:=s.InitializeMacEmpty("app");err!=nil{t.Fatal(err)}
 if err:=s.SaveAs("app","other",true);!errors.Is(err,ErrLocked){t.Fatalf("Locked Save As allowed: %v",err)}
 if err:=s.Rename("app","new",true);!errors.Is(err,ErrLocked){t.Fatalf("Locked rename allowed: %v",err)}
 if err:=s.Delete("app",true);!errors.Is(err,ErrLocked){t.Fatalf("Locked delete allowed: %v",err)}
 if err:=s.SetLocked("app",false);err!=nil{t.Fatal(err)}
 if err:=s.SaveAs("app","copy",true);err!=nil{t.Fatal(err)}
 locked,err:=s.IsLocked("copy")
 if err!=nil||!locked{t.Fatal("new copied profile must be Locked")}
 if err:=s.Rename("app","new",true);err!=nil{t.Fatal(err)}
 if err:=s.Delete("new",true);!errors.Is(err,ErrLocked){t.Fatalf("renamed profile must be Locked by default: %v",err)}
 if err:=s.SetLocked("new",false);err!=nil{t.Fatal(err)}
 if err:=s.Delete("new",true);err!=nil{t.Fatal(err)}
 ids,err:=s.ListIDs(true);if err!=nil{t.Fatal(err)}
 if len(ids)!=1||ids[0]!="copy"{t.Fatalf("unexpected remaining config IDs: %v",ids)}
 path,_:=s.FilePath("new",true)
 if _,err:=os.Stat(path);!errors.Is(err,os.ErrNotExist){t.Fatal("deletion did not remove local file")}
}
