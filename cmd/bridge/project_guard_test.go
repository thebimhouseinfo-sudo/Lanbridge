package main

import (
 "errors"
 "strings"
 "testing"

 "github.com/thebimhouseinfo-sudo/testbridge/internal/projectconfig"
)

func TestMacProjectDispatchFailsClosedWithoutLoadedBlacklist(t *testing.T) {
 empty:=projectconfig.MacBlacklist{SchemaVersion:1,ProjectID:"my-app",Entries:[]projectconfig.Rule{}}
 if err:=authorizeMacProjectCall("my-app",empty,false,"my-app__build",nil);!errors.Is(err,projectconfig.ErrNotOpen){
  t.Fatalf("not-loaded file bypassed: %v",err)
 }
 if err:=authorizeMacProjectCall("my-app",projectconfig.MacBlacklist{},true,"my-app__build",nil);!errors.Is(err,projectconfig.ErrNotOpen){
  t.Fatalf("missing file bypassed: %v",err)
 }
 if err:=authorizeMacProjectCall("my-app",empty,true,"my-app__build",nil);err!=nil{t.Fatalf("valid loaded empty deny list blocked tool: %v",err)}
 if err:=authorizeMacProjectCall("my-app",empty,true,"other-app__open",nil);!errors.Is(err,projectconfig.ErrMismatch){
  t.Fatalf("wrong project tool accepted: %v",err)
 }
}

func TestMacProjectDispatchBlocksOnlyUserSelectedRules(t *testing.T) {
 b:=projectconfig.MacBlacklist{SchemaVersion:1,ProjectID:"windows-app",
  Entries:[]projectconfig.Rule{{Kind:"tool",Tool:"windows-app__danger"}}}
 if err:=authorizeMacProjectCall("windows-app",b,true,"windows-app__danger",map[string]any{});err==nil||!strings.Contains(err.Error(),"ERR_COMMAND_BLACKLISTED"){
  t.Fatalf("configured deny missing: %v",err)
 }
 if err:=authorizeMacProjectCall("windows-app",b,true,"windows-app__normal",map[string]any{});err!=nil{
  t.Fatalf("unrelated app tool blocked: %v",err)
 }
}
