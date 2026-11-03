// Package maintenance provides a deliberately narrow, pairing-scoped
// exception for repairing the Windows LAN Bridge *source*, not its App Data.
package maintenance

import (
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strings"
 "sync"

 "github.com/thebimhouseinfo-sudo/testbridge/internal/permissions"
 "github.com/thebimhouseinfo-sudo/testbridge/internal/projectconfig"
)

var (
 ErrDenied=errors.New("ERR_MAINTENANCE_DENIED")
 ErrUnavailable=errors.New("ERR_MAINTENANCE_UNAVAILABLE")
)
type State struct {
 SourceRoot string `json:"source_root,omitempty"`
 PeerFingerprint string `json:"peer_fingerprint,omitempty"`
 Enabled bool `json:"enabled"`
}
type Status struct {
 Available bool `json:"available"`
 Enabled bool `json:"enabled"`
 SourceRoot string `json:"source_root,omitempty"`
}
type Entry struct {
 Name string `json:"name"`
 Directory bool `json:"directory"`
}
type Service struct {
 mu sync.RWMutex
 path string
 state State
 appData string
}
func New(path,appData string)(*Service,error){
 if path==""||appData=="" {return nil,ErrUnavailable}
 s:=&Service{path:path,appData:appData}
 data,err:=os.ReadFile(path)
 if errors.Is(err,os.ErrNotExist){return s,nil}
 if err!=nil{return s,ErrUnavailable}
 if json.Unmarshal(data,&s.state)!=nil{return s,ErrUnavailable}
 return s,nil
}
func isBridgeSource(root string) bool{
 mod,err:=os.ReadFile(filepath.Join(root,"go.mod"))
 if err!=nil||!strings.Contains(string(mod),"module github.com/thebimhouseinfo-sudo/testbridge"){return false}
 info,err:=os.Stat(filepath.Join(root,"cmd","bridge","main.go"))
 return err==nil&&info.Mode().IsRegular()
}
func (s *Service) writeState(p State)error {
 data,err:=json.MarshalIndent(p,"","  ")
 if err!=nil{return ErrUnavailable}
 if err:=os.MkdirAll(filepath.Dir(s.path),0700);err!=nil{return ErrUnavailable}
 f,err:=os.CreateTemp(filepath.Dir(s.path),".maintenance-*")
 if err!=nil{return ErrUnavailable}
 defer os.Remove(f.Name())
 if err=f.Chmod(0600);err==nil {_,err=f.Write(data)}
 if err==nil{err=f.Sync()}
 if e:=f.Close();err==nil{err=e}
 if err!=nil{return ErrUnavailable}
 if err:=os.Rename(f.Name(),s.path);err!=nil{return ErrUnavailable}
 s.state=p
 return nil
}
// Configure is a Windows-local UI action; it cannot be called through MCP.
func (s *Service) Configure(sourceRoot,peerFingerprint string)error{
 if peerFingerprint=="" {return ErrDenied}
 canon,err:=filepath.EvalSymlinks(sourceRoot)
 if err!=nil{return ErrUnavailable}
 canon,err=filepath.Abs(canon);if err!=nil{return ErrUnavailable}
 if !isBridgeSource(canon)||projectconfig.IsBridgePrivatePath(canon,s.appData) {return ErrUnavailable}
 s.mu.Lock();defer s.mu.Unlock()
 return s.writeState(State{SourceRoot:canon,PeerFingerprint:peerFingerprint,Enabled:true})
}
func (s *Service) SetEnabled(enabled bool)error{
 s.mu.Lock();defer s.mu.Unlock()
 if s.state.SourceRoot==""||s.state.PeerFingerprint==""{return ErrUnavailable}
 next:=s.state;next.Enabled=enabled
 return s.writeState(next)
}
func (s *Service) Status(peerFP string)Status{
 s.mu.RLock();defer s.mu.RUnlock()
 good:=peerFP!="" && s.state.PeerFingerprint==peerFP && s.state.Enabled && isBridgeSource(s.state.SourceRoot)
 if !good{return Status{Available:s.state.SourceRoot!=""}}
 return Status{Available:true,Enabled:true,SourceRoot:s.state.SourceRoot}
}
// IsPrivateSourcePath lets the Mac reject protected names before opening
// local source bytes; Windows enforces the same rule again after resolution.
func IsPrivateSourcePath(rel string)bool{return protected(rel)}

func protected(rel string) bool {
 if rel=="" {return false}
 // Both slash styles are treated as separators even on non-Windows tests.
 parts:=strings.FieldsFunc(strings.ToLower(rel),func(r rune)bool{return r=='/'||r=='\\'})
 for i,p:=range parts {
  if p==""||p=="."{continue}
  if p==".."||p==".git"||p=="node_modules"||p=="dist"||p==".env"||strings.HasPrefix(p,".env.")||strings.HasPrefix(p,".env-") {return true}
  switch p {
  case "config.json","state.json","permissions.json","command-blacklist.json","blacklist-locks.json","maintenance.json","credentials.json","secrets.json","id_rsa","id_ed25519","bridge.exe","lan bridge.app":
   return true
  }
  // "bridge" is a legitimate source directory (cmd/bridge), but not
  // a syncable executable leaf.
  if p=="bridge" && i==len(parts)-1{return true}
  if strings.HasSuffix(p,".pem")||strings.HasSuffix(p,".p12")||strings.HasSuffix(p,".pfx")||strings.HasSuffix(p,".key")||strings.HasSuffix(p,".dll")||strings.HasSuffix(p,".exe")||strings.Contains(p,"token.")||strings.Contains(p,"secret."){return true}
 }
 return false
}
func (s *Service) Resolve(peerFP,relative string)(string,error){
 s.mu.RLock()
 st:=s.state
 s.mu.RUnlock()
 if peerFP==""||!st.Enabled||peerFP!=st.PeerFingerprint{return "",ErrDenied}
 if st.SourceRoot==""||!isBridgeSource(st.SourceRoot){return "",ErrUnavailable}
 if relative==""{relative="."}
 if protected(relative){return "",ErrDenied}
 path,err:=permissions.Resolve(st.SourceRoot,relative)
 if err!=nil{return "",ErrDenied}
 if projectconfig.IsBridgePrivatePath(path,s.appData){return "",ErrDenied}
 return path,nil
}
func (s *Service) List(peerFP,relative string)([]Entry,error){
 path,err:=s.Resolve(peerFP,relative);if err!=nil{return nil,err}
 entries,err:=os.ReadDir(path);if err!=nil{return nil,ErrUnavailable}
 out:=make([]Entry,0,len(entries))
 for _,e:=range entries{
  child:=filepath.Join(relative,e.Name())
  if protected(child){continue}
  if _,err=s.Resolve(peerFP,child);err!=nil{continue}
  out=append(out,Entry{Name:e.Name(),Directory:e.IsDir()})
 }
 return out,nil
}
func (s *Service) Read(peerFP,relative string,offset int64,limit int)([]byte,bool,error){
 if offset<0||limit<0||limit>1024*1024{return nil,false,fmt.Errorf("ERR_PARAMS")}
 if limit==0{limit=64*1024}
 path,err:=s.Resolve(peerFP,relative);if err!=nil{return nil,false,err}
 f,err:=os.Open(path);if err!=nil{return nil,false,ErrUnavailable}
 defer f.Close()
 info,err:=f.Stat();if err!=nil||!info.Mode().IsRegular(){return nil,false,ErrUnavailable}
 data:=make([]byte,limit)
 n,err:=f.ReadAt(data,offset)
 if err!=nil&&!errors.Is(err,io.EOF){return nil,false,ErrUnavailable}
 return data[:n],offset+int64(n)<info.Size(),nil
}
