package mcpsecret

import (
 "os"
 "path/filepath"
 "runtime"
 "strings"
 "testing"
)

func TestSecretStoreFailClosedAndNeverWritesPlaintext(t *testing.T){
 root:=t.TempDir()
 s,err:=New(root);if err!=nil{t.Fatal(err)}
 val:=Value{Endpoint:"http://127.0.0.1:5000/private/secret-query?token=secret-query",Authorization:"Bearer secret-header"}
 err=s.Save("generic",val)
 if runtime.GOOS!="windows"{
  if err==nil{t.Fatal("plaintext fallback outside Windows")}
  if s.Exists("generic"){t.Fatal("secret written without native protection")}
  return
 }
 if err!=nil{t.Fatal(err)}
 raw,err:=os.ReadFile(filepath.Join(root,"mcp-secrets","generic.bin"))
 if err!=nil{t.Fatal(err)}
 if strings.Contains(string(raw),"secret-query")||strings.Contains(string(raw),"secret-header"){t.Fatal("secret persisted in plaintext")}
 loaded,err:=s.Load("generic")
 if err!=nil||loaded!=val{t.Fatalf("DPAPI roundtrip failed: %+v %v",loaded,err)}
 if err:=s.Delete("generic");err!=nil{t.Fatal(err)}
 if s.Exists("generic"){t.Fatal("secret file not removed")}
}
