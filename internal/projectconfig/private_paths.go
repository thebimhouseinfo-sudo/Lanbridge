package projectconfig

import (
 "os"
 "path/filepath"
 "runtime"
 "strings"
)

// IsBridgePrivatePath identifies a path within Bridge App Data. The two
// project config files must never be read/transferred by the generic Bridge
// file tools, even if user grants a broader parent folder for regular work.
// This is about Bridge-owned config transport, NOT an OS sandbox.
func IsBridgePrivatePath(candidate, appDataRoot string) bool {
 if candidate==""||appDataRoot=="" {return false}
 resolved:=func(path string) (string,error) {
  path,err:=filepath.Abs(filepath.Clean(path))
  if err!=nil{return "",err}
  remaining:=[]string{}
  for {
   _,e:=os.Lstat(path)
   if e==nil {break}
   if !os.IsNotExist(e){return "",e}
   parent:=filepath.Dir(path)
   if parent==path{return "",e}
   remaining=append(remaining,filepath.Base(path))
   path=parent
  }
  path,err=filepath.EvalSymlinks(path)
  if err!=nil{return "",err}
  for i:=len(remaining)-1;i>=0;i--{path=filepath.Join(path,remaining[i])}
  return filepath.Clean(path),nil
 }
 root,err:=resolved(appDataRoot);if err!=nil{return false}
 child,err:=resolved(candidate);if err!=nil{return false}
 if runtime.GOOS=="windows" {root=strings.ToLower(root);child=strings.ToLower(child)}
 rel,err:=filepath.Rel(root,child)
 if err!=nil||rel=="."||rel==".."||filepath.IsAbs(rel)||strings.HasPrefix(rel,".."+string(os.PathSeparator)){return false}
 // Restrict only Bridge configuration and pairing state, not arbitrary
 // logs/files a Windows user may intentionally grant access to.
 parts:=strings.Split(rel,string(os.PathSeparator))
 if len(parts)>0 && (strings.EqualFold(parts[0],"profiles")||strings.EqualFold(parts[0],"mcp-secrets")){return true}
 if len(parts)!=1{return false}
 switch strings.ToLower(parts[0]) {
 case "config.json","state.json","blacklist-locks.json","maintenance.json":
  return true
 }
 return false
}
