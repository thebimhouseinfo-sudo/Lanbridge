// Package projectconfig owns the two *local* per-project Bridge configuration
// files. Windows grants and Mac blacklist are never copied between hosts.
package projectconfig

import (
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "regexp"
 "sync"
)

const SchemaVersion = 1

var (
 ErrNotFound = errors.New("CONFIG_NOT_FOUND")
 ErrInvalid = errors.New("CONFIG_INVALID")
 ErrLocked = errors.New("CONFIG_LOCKED")
 ErrNotOpen = errors.New("CONFIG_NOT_OPEN")
 ErrMismatch = errors.New("CONFIG_MISMATCH")
 validID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
)

type FolderGrant struct {
 Path string `json:"path"`
 Capabilities []string `json:"capabilities"`
}
type ProgramGrant struct {
 Name string `json:"name"`
 Executable string `json:"executable"`
 Start bool `json:"start"`
 Stop bool `json:"stop"`
}
type MCPConfig struct {
 Transport string `json:"transport"`
 Command []string `json:"command,omitempty"`
 URL string `json:"url,omitempty"`
}
type WindowsPermissions struct {
 SchemaVersion int `json:"schema_version"`
 ProjectID string `json:"project_id"`
 DisplayName string `json:"display_name,omitempty"`
 Workspace string `json:"workspace,omitempty"`
 Folders []FolderGrant `json:"folders"`
 Capabilities []string `json:"capabilities,omitempty"`
 Programs []ProgramGrant `json:"programs,omitempty"`
 NamedCommands map[string][]string `json:"named_commands,omitempty"`
 MCP *MCPConfig `json:"mcp,omitempty"`
}
type Rule struct {
 Kind string `json:"kind"` // tool or argument
 Tool string `json:"tool"`
 Path string `json:"path,omitempty"`
 Equals string `json:"equals,omitempty"`
}
type MacBlacklist struct {
 SchemaVersion int `json:"schema_version"`
 ProjectID string `json:"project_id"`
 Entries []Rule `json:"entries"`
}

// Store uses a platform-local App Data base directory. Callers choose their
// own machine's base; no transport or remote filesystem is involved.
type Store struct {
 base string
 mu sync.Mutex
}

func New(base string) (*Store,error) {
 if base=="" {return nil,errors.New("config base directory required")}
 return &Store{base:base},nil
}
func ValidateID(id string) error {
 if !validID.MatchString(id) {return fmt.Errorf("%w: invalid project ID",ErrInvalid)}
 return nil
}
func (s *Store) directory(id string) (string,error) {
 if err:=ValidateID(id);err!=nil{return "",err}
 return filepath.Join(s.base,"profiles",id),nil
}
func (s *Store) FilePath(id string, mac bool) (string,error) {
 dir,err:=s.directory(id);if err!=nil{return "",err}
 if mac{return filepath.Join(dir,"command-blacklist.json"),nil}
 return filepath.Join(dir,"permissions.json"),nil
}
func writeAtomic(path string, data []byte) error {
 dir:=filepath.Dir(path)
 if err:=os.MkdirAll(dir,0700);err!=nil{return err}
 tmp,err:=os.CreateTemp(dir,".bridge-config-*")
 if err!=nil{return err}
 defer os.Remove(tmp.Name())
 if err=tmp.Chmod(0600);err==nil {_,err=tmp.Write(data)}
 if err==nil {err=tmp.Sync()}
 cerr:=tmp.Close()
 if err!=nil{return err}
 if cerr!=nil{return cerr}
 return os.Rename(tmp.Name(),path)
}
func loadJSON(path string, into any) error {
 bytes,err:=os.ReadFile(path)
 if errors.Is(err,os.ErrNotExist){return fmt.Errorf("%w: %s",ErrNotFound,path)}
 if err!=nil{return err}
 if err=json.Unmarshal(bytes,into);err!=nil{return fmt.Errorf("%w: %v",ErrInvalid,err)}
 return nil
}
func validateWindows(id string, p *WindowsPermissions) error {
 if p.SchemaVersion!=SchemaVersion || p.ProjectID!=id {return ErrInvalid}
 return nil
}
func validateMac(id string, b *MacBlacklist) error {
 if b.SchemaVersion!=SchemaVersion || b.ProjectID!=id || b.Entries==nil {return ErrInvalid}
 for _,entry:=range b.Entries {
  if err:=ValidateRule(entry);err!=nil{return fmt.Errorf("%w: %v",ErrInvalid,err)}
 }
 return nil
}

// SaveWindows is for Windows UI actions, never a Mac agent config.load side effect.
func (s *Store) SaveWindows(id string, p WindowsPermissions) error {
 s.mu.Lock();defer s.mu.Unlock()
 path,err:=s.FilePath(id,false);if err!=nil{return err}
 if err=validateWindows(id,&p);err!=nil{return err}
 if p.Folders==nil {p.Folders=[]FolderGrant{}}
 data,err:=json.MarshalIndent(p,"","  ");if err!=nil{return err}
 return writeAtomic(path,append(data,'\n'))
}
func (s *Store) LoadWindows(id string) (WindowsPermissions,error) {
 s.mu.Lock();defer s.mu.Unlock()
 path,err:=s.FilePath(id,false);if err!=nil{return WindowsPermissions{},err}
 var p WindowsPermissions
 if err=loadJSON(path,&p);err!=nil{return p,err}
 return p,validateWindows(id,&p)
}

// InitializeWindows is a first-setup action initiated only from Windows UI.
// It must NOT overwrite a user's existing saved permissions.
func (s *Store) InitializeWindows(id string, p WindowsPermissions) (bool,error) {
 s.mu.Lock();defer s.mu.Unlock()
 path,err:=s.FilePath(id,false);if err!=nil{return false,err}
 if p.Folders==nil{p.Folders=[]FolderGrant{}}
 if err=validateWindows(id,&p);err!=nil{return false,err}
 if err=os.MkdirAll(filepath.Dir(path),0700);err!=nil{return false,err}
 data,err:=json.MarshalIndent(p,"","  ");if err!=nil{return false,err}
 file,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
 if errors.Is(err,os.ErrExist){return false,fmt.Errorf("CONFIG_ALREADY_EXISTS: use Save on existing profile")}
 if err!=nil{return false,err}
 if _,err=file.Write(append(data,'\n'));err!=nil {_=file.Close();_=os.Remove(path);return false,err}
 if err=file.Sync();err!=nil {_=file.Close();_=os.Remove(path);return false,err}
 if err=file.Close();err!=nil{return false,err}
 return true,nil
}

// InitializeMacEmpty is called by Mac app's FIRST-TIME project setup only,
// never by config.load. O_EXCL ensures existing rules are never reset.
func (s *Store) InitializeMacEmpty(id string) (bool,error) {
 s.mu.Lock();defer s.mu.Unlock()
 path,err:=s.FilePath(id,true);if err!=nil{return false,err}
 if err=os.MkdirAll(filepath.Dir(path),0700);err!=nil{return false,err}
 b:=MacBlacklist{SchemaVersion:SchemaVersion,ProjectID:id,Entries:[]Rule{}}
 data,err:=json.MarshalIndent(b,"","  ");if err!=nil{return false,err}
 f,err:=os.OpenFile(path,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600)
 if errors.Is(err,os.ErrExist){return false,nil}
 if err!=nil{return false,err}
 if _,err=f.Write(append(data,'\n'));err!=nil {_=f.Close();_ = os.Remove(path);return false,err}
 if err=f.Sync();err!=nil {_=f.Close();_ = os.Remove(path);return false,err}
 if err=f.Close();err!=nil{return false,err}
 return true,nil
}

// LoadMac NEVER creates a missing blacklist; absent and loaded-empty differ.
func (s *Store) LoadMac(id string) (MacBlacklist,error) {
 s.mu.Lock();defer s.mu.Unlock()
 path,err:=s.FilePath(id,true);if err!=nil{return MacBlacklist{},err}
 var b MacBlacklist
 if err=loadJSON(path,&b);err!=nil{return b,err}
 return b,validateMac(id,&b)
}
// Locks are local global Mac app metadata, not a third per-project config.
func (s *Store) lockPath() string {return filepath.Join(s.base,"blacklist-locks.json")}
func (s *Store) readLocks() (map[string]bool,error) {
 var locks map[string]bool
 err:=loadJSON(s.lockPath(),&locks)
 if errors.Is(err,ErrNotFound){return map[string]bool{},nil}
 if err!=nil{return nil,err}
 if locks==nil {return nil,ErrInvalid}
 return locks,nil
}
func (s *Store) IsLocked(id string) (bool,error) {
 s.mu.Lock();defer s.mu.Unlock()
 if err:=ValidateID(id);err!=nil{return false,err}
 locks,err:=s.readLocks();if err!=nil{return false,err}
 return locks[id],nil
}
// SetLocked is ONLY wired to a direct human Mac UI action. Never export
// this method as an agent/MCP tool.
func (s *Store) SetLocked(id string, locked bool) error {
 s.mu.Lock();defer s.mu.Unlock()
 if err:=ValidateID(id);err!=nil{return err}
 locks,err:=s.readLocks();if err!=nil{return err}
 locks[id]=locked
 data,err:=json.MarshalIndent(locks,"","  ");if err!=nil{return err}
 return writeAtomic(s.lockPath(),append(data,'\n'))
}
// UpdateMac is for a separate user-requested editing workflow. Caller must
// enforce explicit user intent; store rejects writes while locked.
func (s *Store) UpdateMac(id string, entries []Rule) error {
 s.mu.Lock();defer s.mu.Unlock()
 path,err:=s.FilePath(id,true);if err!=nil{return err}
 // Existing-file-only: never manufacture a blacklist via edit.
 var existing MacBlacklist
 if err=loadJSON(path,&existing);err!=nil{return err}
 if err=validateMac(id,&existing);err!=nil{return err}
 locks,err:=s.readLocks();if err!=nil{return err}
 if locks[id]{return ErrLocked}
 if entries==nil {entries=[]Rule{}}
 update:=MacBlacklist{SchemaVersion:SchemaVersion,ProjectID:id,Entries:entries}
 if err=validateMac(id,&update);err!=nil{return err}
 data,err:=json.MarshalIndent(update,"","  ");if err!=nil{return err}
 return writeAtomic(path,append(data,'\n'))
}

 // ListIDs lists existing local project files only; it never initializes
 // profiles and never queries the other machine.
func (s *Store) ListIDs(mac bool) ([]string,error) {
 s.mu.Lock();defer s.mu.Unlock()
 root:=filepath.Join(s.base,"profiles")
 dirs,err:=os.ReadDir(root)
 if errors.Is(err,os.ErrNotExist){return []string{},nil}
 if err!=nil{return nil,err}
 ids:=make([]string,0)
 for _,entry:=range dirs {
  if !entry.IsDir() || ValidateID(entry.Name())!=nil {continue}
  file:="permissions.json"
  if mac{file="command-blacklist.json"}
  st,e:=os.Stat(filepath.Join(root,entry.Name(),file))
  if e==nil && st.Mode().IsRegular(){ids=append(ids,entry.Name())}
 }
 return ids,nil
}
