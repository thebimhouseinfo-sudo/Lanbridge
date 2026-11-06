package projectconfig

import (
 "sync"
)

// Session is an in-memory activation handshake, distinct from network pairing.
// It contains only IDs and loaded state, never config contents.
type Session struct {
 mu sync.RWMutex
 windowsID string
 macID string
}

// SetWindowsLoaded is called only after a valid Windows permission file was
// manually opened through the Windows UI.
func (s *Session) SetWindowsLoaded(id string) error {
 if id!="" {if err:=ValidateID(id);err!=nil{return err}}
 s.mu.Lock()
 s.windowsID=id
 s.mu.Unlock()
 return nil
}
// SetMacLoaded must be called only AFTER Mac blacklist LoadMac succeeds.
func (s *Session) SetMacLoaded(id string) error {
 if id!="" {if err:=ValidateID(id);err!=nil{return err}}
 s.mu.Lock()
 s.macID=id
 s.mu.Unlock()
 return nil
}
// ClearWindows forgets only the Windows side. Re-opening a Windows config must
// not discard the Mac's already completed blacklist handshake.
func (s *Session) ClearWindows() {
 s.mu.Lock()
 s.windowsID=""
 s.mu.Unlock()
}
func (s *Session) Clear() {
 s.mu.Lock()
 s.windowsID=""
 s.macID=""
 s.mu.Unlock()
}
type Status struct {
 WindowsID string `json:"windows_project_id,omitempty"`
 MacID string `json:"mac_project_id,omitempty"`
 Ready bool `json:"ready"`
 Error string `json:"error,omitempty"`
}
func (s *Session) Snapshot() Status {
 s.mu.RLock();defer s.mu.RUnlock()
 out:=Status{WindowsID:s.windowsID,MacID:s.macID}
 switch {
 case s.windowsID=="" || s.macID=="":
  out.Error=ErrNotOpen.Error()
 case s.windowsID!=s.macID:
  out.Error=ErrMismatch.Error()
 default:
  out.Ready=true
 }
 return out
}
func (s *Session) Require(projectID string) error {
 state:=s.Snapshot()
 if !state.Ready {
  if state.Error==ErrMismatch.Error(){return ErrMismatch}
  return ErrNotOpen
 }
 if projectID!="" && state.WindowsID!=projectID {return ErrMismatch}
 return nil
}
func (s *Session) HasProject(projectID string) bool {return s.Require(projectID)==nil}
