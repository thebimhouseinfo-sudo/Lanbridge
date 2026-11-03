package executor

import (
 "runtime"
 "strings"
 "testing"
 "time"

 "github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

func TestRunShellGenericWindowsOrMacCommandAndWorkdir(t *testing.T) {
 root:=t.TempDir()
 project:=&store.ProjectConfig{Name:"generic-app",Root:root}
 argv:=[]string{"/bin/sh","-c","echo BRIDGE_SHELL_OK && pwd"}
 if runtime.GOOS=="windows"{argv=[]string{"cmd","/c","echo BRIDGE_SHELL_OK && cd"}}
 manager:=NewManager()
 job,err:=manager.RunShell(project,argv,root)
 if err!=nil{t.Fatal(err)}
 deadline:=time.Now().Add(10*time.Second)
 for {
  status,code:=job.State()
  if status!=StatusRunning {
   if status!=StatusDone||code!=0{t.Fatalf("shell status=%s code=%d",status,code)}
   out,stderr:=job.FullOutput()
   if !strings.Contains(string(out),"BRIDGE_SHELL_OK")||!strings.Contains(strings.ToLower(string(out)),strings.ToLower(root)){
    t.Fatalf("shell did not run in granted workdir: stdout=%q stderr=%q",out,stderr)
   }
   break
  }
  if time.Now().After(deadline){job.Cancel();t.Fatal("Shell timeout")}
  time.Sleep(30*time.Millisecond)
 }
}

func TestRunShellRequiresExecutable(t *testing.T){
 m:=NewManager()
 p:=&store.ProjectConfig{Name:"generic-app",Root:t.TempDir()}
 if _,err:=m.RunShell(p,nil,"");err==nil{t.Fatal("empty argv accepted")}
}
