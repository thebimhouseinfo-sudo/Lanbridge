//go:build windows

package awake

import (
 "fmt"
 "runtime"
 "syscall"
)

const (esContinuous=0x80000000; esSystemRequired=0x00000001)
var kernel32=syscall.NewLazyDLL("kernel32.dll")
var setThreadExecutionState=kernel32.NewProc("SetThreadExecutionState")

func acquirePlatform() (func(),error){
 ready:=make(chan error,1)
 stop:=make(chan struct{})
 done:=make(chan struct{})
 go func(){
  runtime.LockOSThread()
  defer runtime.UnlockOSThread()
  defer close(done)
  ret,_,callErr:=setThreadExecutionState.Call(esContinuous|esSystemRequired)
  if ret==0{
   ready<-fmt.Errorf("SetThreadExecutionState acquisition failed: %v",callErr)
   return
  }
  ready<-nil
  <-stop
  // Must clear on the same OS thread that acquired the request.
  _,_,_=setThreadExecutionState.Call(esContinuous)
 }()
 if err:= <-ready;err!=nil{return nil,err}
 return func(){close(stop);<-done},nil
}
