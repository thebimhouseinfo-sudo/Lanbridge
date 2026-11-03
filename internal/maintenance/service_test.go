package maintenance

import (
 "errors"
 "os"
 "path/filepath"
 "testing"
)
func makeSource(t *testing.T)string{
 t.Helper()
 root:=t.TempDir()
 if err:=os.MkdirAll(filepath.Join(root,"cmd","bridge"),0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(root,"go.mod"),[]byte("module github.com/thebimhouseinfo-sudo/testbridge\n"),0600);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(root,"cmd","bridge","main.go"),[]byte("package main\n"),0600);err!=nil{t.Fatal(err)}
 return root
}
func TestSourceProvisionAndRevokeIndependentOfProject(t *testing.T){
 dir:=t.TempDir()
 root:=makeSource(t)
 svc,err:=New(filepath.Join(dir,"maintenance.json"),dir)
 if err!=nil{t.Fatal(err)}
 if st:=svc.Status("pair-1");st.Enabled{t.Fatal("no guessed default source")}
 if err:=svc.Configure(root,"pair-1");err!=nil{t.Fatal(err)}
 if !svc.Status("pair-1").Enabled || svc.Status("other-peer").Enabled{t.Fatal("peer scope")}
 if _,err:=svc.Resolve("other-peer","cmd/bridge/main.go");!errors.Is(err,ErrDenied){t.Fatal("wrong peer allowed")}
 if got,more,err:=svc.Read("pair-1","cmd/bridge/main.go",0,50);err!=nil||more||string(got)!="package main\n"{t.Fatalf("%q %v %v",got,more,err)}
 svc2,err:=New(filepath.Join(dir,"maintenance.json"),dir)
 if err!=nil||!svc2.Status("pair-1").Enabled{t.Fatal("failed persistence")}
 if err:=svc2.SetEnabled(false);err!=nil{t.Fatal(err)}
 if _,_,err:=svc2.Read("pair-1","cmd/bridge/main.go",0,100);!errors.Is(err,ErrDenied){t.Fatal("revoke ineffective")}
}
func TestSourceProtectsAliasesAndSecrets(t *testing.T){
 dir:=t.TempDir();root:=makeSource(t)
 svc,_:=New(filepath.Join(dir,"maintenance.json"),dir)
 if err:=svc.Configure(root,"peer");err!=nil{t.Fatal(err)}
 for _,rel:=range []string{"../outside",".git/config",".env",".env.production","secret.key","credentials.json","bin/bridge.exe","dist/app.zip","foo/token.json"} {
  if _,err:=svc.Resolve("peer",rel);!errors.Is(err,ErrDenied){t.Fatalf("%q allowed (%v)",rel,err)}
 }
 if err:=os.Symlink(dir,filepath.Join(root,"jump"));err==nil {
  if _,err:=svc.Resolve("peer","jump/state.json");!errors.Is(err,ErrDenied){t.Fatal("symlink escaped source")}
 }
 entries,err:=svc.List("peer",".");if err!=nil{t.Fatal(err)}
 if len(entries)==0{t.Fatal("normal source hidden")}
}
func TestCannotConfigureUnknownSource(t *testing.T){
 dir:=t.TempDir();s,_:=New(filepath.Join(dir,"maintenance.json"),dir)
 if err:=s.Configure(t.TempDir(),"peer");!errors.Is(err,ErrUnavailable){t.Fatalf("unexpected %v",err)}
}
