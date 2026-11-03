package projectconfig

import (
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "path/filepath"
)

// SaveAs copies one local config to a new Project ID. On Mac, user must have
// manually Unlocked the source blacklist. It never transfers data across LAN.
func (s *Store) SaveAs(oldID,newID string,mac bool) error {
 s.mu.Lock();defer s.mu.Unlock()
 return s.copyProfile(oldID,newID,mac)
}
func (s *Store) copyProfile(oldID,newID string,mac bool) error {
 if oldID==newID{return errors.New("new project ID must differ")}
 src,err:=s.FilePath(oldID,mac);if err!=nil{return err}
 dst,err:=s.FilePath(newID,mac);if err!=nil{return err}
 var data []byte
 if mac {
  locks,err:=s.readLocks();if err!=nil{return err}
  if locked,exists:=locks[oldID];!exists||locked{return ErrLocked}
  var b MacBlacklist
  if err:=loadJSON(src,&b);err!=nil{return err}
  if err:=validateMac(oldID,&b);err!=nil{return err}
  b.ProjectID=newID
  data,err=json.MarshalIndent(b,"","  ");if err!=nil{return err}
 }else{
  var p WindowsPermissions
  if err:=loadJSON(src,&p);err!=nil{return err}
  if err:=validateWindows(oldID,&p);err!=nil{return err}
  p.ProjectID=newID
  data,err=json.MarshalIndent(p,"","  ");if err!=nil{return err}
 }
 if err:=os.MkdirAll(filepath.Dir(dst),0700);err!=nil{return err}
 file,err:=os.OpenFile(dst,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
 if errors.Is(err,os.ErrExist){return fmt.Errorf("CONFIG_ALREADY_EXISTS: %s",newID)}
 if err!=nil{return err}
 if _,err=file.Write(append(data,'\n'));err!=nil{_ = file.Close();_ = os.Remove(dst);return err}
 if err=file.Sync();err!=nil{_ = file.Close();_ = os.Remove(dst);return err}
 if err=file.Close();err!=nil{return err}
 return nil
}
// Rename locally preserves all values other than the Project ID. A Mac
// blacklist must be manually Unlocked; the renamed profile defaults Locked.
func (s *Store) Rename(oldID,newID string,mac bool) error {
 s.mu.Lock();defer s.mu.Unlock()
 if err:=s.copyProfile(oldID,newID,mac);err!=nil{return err}
 src,_:=s.FilePath(oldID,mac)
 if err:=os.Remove(src);err!=nil{return err}
 if mac {
  locks,err:=s.readLocks();if err!=nil{return err}
  delete(locks,oldID)
  b,err:=json.MarshalIndent(locks,"","  ");if err!=nil{return err}
  if err:=writeAtomic(s.lockPath(),append(b,'\n'));err!=nil{return err}
 }
 _=os.Remove(filepath.Dir(src))
 return nil
}
// Delete removes a local project config only. It never deletes any project
// workspace, source file, executable or remote counterpart config.
func (s *Store) Delete(id string,mac bool) error {
 s.mu.Lock();defer s.mu.Unlock()
 src,err:=s.FilePath(id,mac);if err!=nil{return err}
 if mac {
  locks,err:=s.readLocks();if err!=nil{return err}
  if locked,exists:=locks[id];!exists||locked{return ErrLocked}
 }
 if err:=os.Remove(src);err!=nil{
  if errors.Is(err,os.ErrNotExist){return ErrNotFound}
  return err
 }
 if mac{
  locks,err:=s.readLocks();if err!=nil{return err}
  delete(locks,id)
  data,err:=json.MarshalIndent(locks,"","  ");if err!=nil{return err}
  if err:=writeAtomic(s.lockPath(),append(data,'\n'));err!=nil{return err}
 }
 _=os.Remove(filepath.Dir(src))
 return nil
}
