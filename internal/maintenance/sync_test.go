package maintenance

import (
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "github.com/thebimhouseinfo-sudo/testbridge/internal/fileops"
)

func TestMaintenanceSyncWorksWithNoProjectGrantAndRevoke(t *testing.T){
 dir:=t.TempDir();root:=makeSource(t)
 service,_:=New(filepath.Join(dir,"maintenance.json"),dir)
 if err:=service.Configure(root,"peer");err!=nil{t.Fatal(err)}
 items:=[]fileops.SyncItem{{RelPath:"internal/example.go",Data:[]byte("package example")}}
 result,err:=service.Sync("peer",items,5)
 if err!=nil||len(result)!=1||result[0].Status!="ok"{t.Fatalf("%+v %v",result,err)}
 data,err:=os.ReadFile(filepath.Join(root,"internal","example.go"))
 if err!=nil||string(data)!="package example"{t.Fatal(err)}
 if err:=service.SetEnabled(false);err!=nil{t.Fatal(err)}
 if _,err:=service.Sync("peer",items,5);!errors.Is(err,ErrDenied){t.Fatal("sync after revoke allowed")}
}
func TestMaintenanceSyncProtectedAndOversized(t *testing.T){
 dir:=t.TempDir();root:=makeSource(t);service,_:=New(filepath.Join(dir,"maintenance.json"),dir)
 if err:=service.Configure(root,"peer");err!=nil{t.Fatal(err)}
 for _,rel:=range []string{".env","cmd/bridge/bridge.exe","../escape",".git/config","config.json"}{
  if _,err:=service.Sync("peer",[]fileops.SyncItem{{RelPath:rel,Data:[]byte("BAD")}},5);err==nil{t.Fatalf("%s allowed",rel)}
 }
 if _,err:=service.Sync("peer",[]fileops.SyncItem{{RelPath:"big.go",Data:[]byte(strings.Repeat("x",5*1024*1024+1))}},5);err==nil||!strings.Contains(err.Error(),"ERR_TOO_LARGE"){t.Fatalf("limit: %v",err)}
}
