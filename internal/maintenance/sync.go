package maintenance

import (
 "fmt"
 "strings"

 "github.com/thebimhouseinfo-sudo/testbridge/internal/fileops"
)

// Sync accepts Mac-originated source files and reuses the same limit,
// lock detection and atomic replacement as ordinary project sync. It does
// not require the Windows project's config to be open; it never grants Exec.
func (s *Service) Sync(peerFP string, files []fileops.SyncItem, maxMB int)([]fileops.SyncResult,error){
 results,err:=fileops.SyncFilesWithResolver(files,maxMB,func(rel string)(string,error){
  if rel==""||rel=="."||strings.HasPrefix(rel,".")&&protected(rel){
   return "",ErrDenied
  }
  return s.Resolve(peerFP,rel)
 })
 if err!=nil{
  // Do not expose host absolute paths or library errors to Mac. Preserve
  // actionable, bounded error categories and already completed item results.
  switch {
  case strings.Contains(err.Error(),"ERR_TOO_LARGE"):
   return results,fmt.Errorf("ERR_TOO_LARGE: maintenance batch exceeds limit")
  case strings.Contains(err.Error(),"ERR_FILE_LOCKED"):
   return results,fmt.Errorf("ERR_FILE_LOCKED: maintenance target locked")
  case strings.Contains(err.Error(),"ERR_CONFIG_PROTECTED"):
   return results,ErrDenied
  default:
   return results,fmt.Errorf("ERR_MAINTENANCE_WRITE_FAILED: %w",ErrDenied)
  }
 }
 return results,nil
}
