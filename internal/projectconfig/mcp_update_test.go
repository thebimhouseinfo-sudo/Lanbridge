package projectconfig

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestUpdateMCPPreservesUnknownFieldsAndGrants(t *testing.T){
 root:=t.TempDir();store,err:=New(root);if err!=nil{t.Fatal(err)}
 old:=WindowsPermissions{SchemaVersion:1,ProjectID:"app",Workspace:"C:/Source",Folders:[]FolderGrant{{Path:"C:/Logs",Capabilities:[]string{"read","sync"}}},
 Programs:[]ProgramGrant{{Name:"CAD",Executable:"C:/Tool.exe",Start:true}},MCP:&MCPConfig{Transport:"stdio",Command:[]string{"old.exe"}}}
 if _,err:=store.InitializeWindows("app",old);err!=nil{t.Fatal(err)}
 path,_:=store.FilePath("app",false)
 data,err:=os.ReadFile(path);if err!=nil{t.Fatal(err)}
 before:=strings.Replace(string(data),"\"schema_version\": 1","\"schema_version\": 1, \"future_field\": {\"keep\": true}",1)
 if err:=os.WriteFile(path,[]byte(before),0600);err!=nil{t.Fatal(err)}
 if err:=store.UpdateMCP("app",MCPConfig{Transport:"streamable_http"});err!=nil{t.Fatal(err)}
 updated,err:=os.ReadFile(path);if err!=nil{t.Fatal(err)}
 if !strings.Contains(string(updated),"\"future_field\""){t.Fatal("unknown field removed")}
 if strings.Contains(string(updated),"secret-query")||strings.Contains(string(updated),"\"url\""){t.Fatal("HTTP endpoint may not be saved in profile")}
 p,err:=store.LoadWindows("app")
 if err!=nil||p.MCP==nil||p.MCP.Transport!="streamable_http"||len(p.Folders)!=1||len(p.Programs)!=1||p.Folders[0].Path!="C:/Logs"{t.Fatalf("grants/profile changed: %+v %v",p,err)}
 oldData:=string(updated)
 if err:=store.UpdateMCP("app",MCPConfig{Transport:"stdio"});err==nil{t.Fatal("invalid config accepted")}
 after,_:=os.ReadFile(path)
 if string(after)!=oldData{t.Fatal("invalid update changed file")}
 if _,err:=os.Stat(filepath.Join(root,"profiles","app","permissions.json"));err!=nil{t.Fatal(err)}
}
