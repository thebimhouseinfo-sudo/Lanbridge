package mcpfwd

import (
 "encoding/json"
 "fmt"
 "net/http"
 "net/http/httptest"
 "strings"
 "sync/atomic"
 "testing"
 "time"
)

func TestStreamableHTTPJSONHandshakeAndSession(t *testing.T){
 var n atomic.Int32
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Method=="GET"{w.WriteHeader(http.StatusMethodNotAllowed);return}
  var req struct {ID int64 `json:"id"`; Method string `json:"method"`}
  _=json.NewDecoder(r.Body).Decode(&req)
  if req.Method!="initialize"&&r.Header.Get("Mcp-Session-Id")!="test-session"{
   t.Errorf("MCP session ID not preserved for %s",req.Method)
  }
  w.Header().Set("Content-Type","application/json")
  w.Header().Set("Mcp-Session-Id","test-session")
  switch req.Method {
  case "initialize":
   fmt.Fprintf(w,`{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-03-26","capabilities":{"tools":{}},"serverInfo":{"name":"independent","version":"1"}}}`,req.ID)
  case "notifications/initialized":w.WriteHeader(http.StatusAccepted)
  case "tools/list":fmt.Fprintf(w,`{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"ping","description":"Ping","inputSchema":{"type":"object"}}]}}`,req.ID)
  case "tools/call":
   n.Add(1);fmt.Fprintf(w,`{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"pong"}]}}`,req.ID)
  }
 }))
 defer server.Close()
 s,err:=openHTTPSession("generic",server.URL,"")
 if err!=nil{t.Fatal(err)}
 defer s.Close()
 if len(s.Tools())!=1||s.Tools()[0].Name!="ping"{t.Fatalf("missing tools: %+v",s.Tools())}
 out,err:=s.CallTool("ping",map[string]any{})
 if err!=nil||!strings.Contains(string(out),"pong")||n.Load()!=1{t.Fatalf("%s %v calls=%d",out,err,n.Load())}
}

func TestStreamableHTTPRejectsRedirectAndSecretsInErrors(t *testing.T){
 for _,endpoint:=range []string{"ftp://localhost/path","http://example.com/mcp","https://user:pass@example.com/mcp"} {
  if ValidateHTTPEndpoint(endpoint)==nil{t.Fatalf("unsafe endpoint accepted: %s",endpoint)}
 }
 secret:="secret-query-token"
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Method=="GET"{w.WriteHeader(http.StatusMethodNotAllowed);return}
  http.Redirect(w,r,"http://example.com/"+secret,http.StatusTemporaryRedirect)
 }))
 defer server.Close()
 h,err:=newHTTPMCP(server.URL+"/private/"+secret+"?token="+secret,"Bearer "+secret)
 if err!=nil{t.Fatal(err)}
 _,err=h.request(map[string]any{"jsonrpc":"2.0","id":int64(1),"method":"initialize"},1)
 if err==nil||strings.Contains(err.Error(),secret){t.Fatalf("secret leaked in redirect: %v",err)}
}

func TestStreamableHTTPTimeoutDoesNotReplayCalls(t *testing.T){
 var count atomic.Int32
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  count.Add(1)
  time.Sleep(80*time.Millisecond)
  fmt.Fprint(w,`{"jsonrpc":"2.0","id":1,"result":{}}`)
 }))
 defer server.Close()
 h,_:=newHTTPMCP(server.URL,"")
 h.timeout=10*time.Millisecond
 _,err:=h.request(map[string]any{"jsonrpc":"2.0","id":int64(1),"method":"tools/call"},1)
 if err==nil{t.Fatal("timeout must fail")}
 if count.Load()!=1{t.Fatalf("unexpected replay count %d",count.Load())}
}
