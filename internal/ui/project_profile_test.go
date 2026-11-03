package ui

import (
 "bytes"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "testing"

 "github.com/thebimhouseinfo-sudo/testbridge/internal/projectconfig"
)

func TestMacProjectUIRequiresExistingFileAndManualUnlock(t *testing.T) {
 store,_:=projectconfig.New(t.TempDir())
 ws,err:=NewWebServer("Client (macOS)","")
 if err!=nil{t.Fatal(err)}
 defer ws.Close()
 var active string
 ws.SetProjectHandlers(
  func()([]string,error){return store.ListIDs(true)},
  func(id,_,_ string)error{_,err:=store.InitializeMacEmpty(id);return err},
  func(id string)error{_,err:=store.LoadMac(id);if err==nil{active=id};return err},
  func(locked bool)error{return store.SetLocked(active,locked)},
  func(entries []projectconfig.Rule)error{return store.UpdateMac(active,entries)},
 )
 ws.Start()
 post:=func(body any,authorized bool)(int,string){
  t.Helper()
  raw,_:=json.Marshal(body)
  url:=fmt.Sprintf("http://127.0.0.1:%d/api/project/action",ws.Port())
  if authorized{url+="?t="+ws.Token()}
  req,err:=http.NewRequest(http.MethodPost,url,bytes.NewReader(raw));if err!=nil{t.Fatal(err)}
  req.Header.Set("Content-Type","application/json")
  resp,err:=http.DefaultClient.Do(req);if err!=nil{t.Fatal(err)}
  defer resp.Body.Close()
  buf,_:=io.ReadAll(resp.Body)
  return resp.StatusCode,string(buf)
 }
 action:=func(kind string,extra map[string]any) map[string]any {
  m:=map[string]any{"action":kind}
  for k,v:=range extra{m[k]=v}
  return m
 }
 if code,_:=post(action("setup",map[string]any{"project_id":"safe-app"}),false);code!=403{t.Fatal("missing UI token accepted")}
 if code,_:=post(action("open",map[string]any{"project_id":"safe-app"}),true);code==200{t.Fatal("Open created a missing blacklist")}
 if _,err:=store.LoadMac("safe-app");!errors.Is(err,projectconfig.ErrNotFound){t.Fatal("file unexpectedly created")}
 if code,msg:=post(action("setup",map[string]any{"project_id":"safe-app"}),true);code!=200{t.Fatalf("setup %d %s",code,msg)}
 if code,msg:=post(action("open",map[string]any{"project_id":"safe-app"}),true);code!=200{t.Fatalf("open %d %s",code,msg)}
 entries:=[]map[string]string{{"kind":"tool","tool":"safe-app__danger"}}
 if code,_:=post(action("edit",map[string]any{"entries":entries}),true);code==200{t.Fatal("default Locked blacklist allowed editing")}
 if code,msg:=post(action("unlock",nil),true);code!=200{t.Fatalf("manual Unlock: %d %s",code,msg)}
 if code,msg:=post(action("edit",map[string]any{"entries":entries}),true);code!=200{t.Fatalf("unlocked user edit: %d %s",code,msg)}
 if code,msg:=post(action("lock",nil),true);code!=200{t.Fatalf("Lock: %d %s",code,msg)}
 if code,_:=post(action("edit",map[string]any{"entries":[]any{}}),true);code==200{t.Fatal("Locked overwrite accepted")}
 b,err:=store.LoadMac("safe-app")
 if err!=nil||len(b.Entries)!=1{t.Fatalf("saved rules changed under Lock: %+v %v",b,err)}
}
