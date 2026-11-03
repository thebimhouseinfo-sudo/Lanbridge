//go:build darwin

package awake

import (
 "fmt"
 "os/exec"
)

// caffeinate -i is a system idle-sleep assertion; unlike -d it permits
// display sleep/lock. The helper is owned only by this Bridge process.
func acquirePlatform() (func(),error){
 cmd:=exec.Command("/usr/bin/caffeinate","-i")
 if err:=cmd.Start();err!=nil{return nil,fmt.Errorf("macOS idle sleep assertion unavailable: %w",err)}
 return func(){
  if cmd.Process!=nil{_ = cmd.Process.Kill()}
  _ = cmd.Wait()
 },nil
}
