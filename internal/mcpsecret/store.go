// Package mcpsecret stores MCP HTTP endpoints and optional Authorization
// material in Windows-user DPAPI-protected blobs. Secrets never enter the
// project permissions JSON, Mac transport or user-visible diagnostics.
package mcpsecret

import (
 "encoding/json"
 "errors"
 "os"
 "path/filepath"

 "github.com/thebimhouseinfo-sudo/testbridge/internal/projectconfig"
)

var ErrUnavailable=errors.New("ERR_MCP_SECRET_STORE_UNAVAILABLE")
type Value struct {
 Endpoint string `json:"endpoint"`
 Authorization string `json:"authorization,omitempty"`
}
type Store struct{root string}
func New(appDataRoot string)(*Store,error){
 if appDataRoot==""{return nil,ErrUnavailable}
 return &Store{root:filepath.Join(appDataRoot,"mcp-secrets")},nil
}
func (s *Store) path(id string)(string,error){
 if err:=projectconfig.ValidateID(id);err!=nil{return "",err}
 return filepath.Join(s.root,id+".bin"),nil
}
func (s *Store) Save(id string,val Value)error{
 p,err:=s.path(id);if err!=nil{return err}
 raw,err:=json.Marshal(val);if err!=nil{return ErrUnavailable}
 protected,err:=protect(raw);if err!=nil{return ErrUnavailable}
 if err:=os.MkdirAll(s.root,0700);err!=nil{return ErrUnavailable}
 tmp,err:=os.CreateTemp(s.root,".mcp-secret-*");if err!=nil{return ErrUnavailable}
 defer os.Remove(tmp.Name())
 if e:=tmp.Chmod(0600);e!=nil{_ = tmp.Close();return ErrUnavailable}
 if _,err=tmp.Write(protected);err!=nil{_ = tmp.Close();return ErrUnavailable}
 if err=tmp.Sync();err!=nil{_ = tmp.Close();return ErrUnavailable}
 if err=tmp.Close();err!=nil{return ErrUnavailable}
 if err=os.Rename(tmp.Name(),p);err!=nil{return ErrUnavailable}
 return nil
}
func (s *Store) Load(id string)(Value,error){
 p,err:=s.path(id);if err!=nil{return Value{},err}
 encrypted,err:=os.ReadFile(p);if err!=nil{return Value{},ErrUnavailable}
 plain,err:=unprotect(encrypted);if err!=nil{return Value{},ErrUnavailable}
 var v Value
 if json.Unmarshal(plain,&v)!=nil||v.Endpoint==""{return Value{},ErrUnavailable}
 return v,nil
}
func (s *Store) Exists(id string)bool{
 p,err:=s.path(id);if err!=nil{return false}
 _,err=os.Stat(p);return err==nil
}
func (s *Store) Delete(id string)error{
 p,err:=s.path(id);if err!=nil{return err}
 if err:=os.Remove(p);err!=nil&&!errors.Is(err,os.ErrNotExist){return ErrUnavailable}
 return nil
}
