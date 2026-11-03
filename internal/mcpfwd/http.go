package mcpfwd

import (
 "bufio"
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "net/url"
 "strings"
 "sync"
 "time"
)

const protocolVersion="2025-03-26"
const maxHTTPBody=8<<20

// HTTPMCP owns one project MCP Streamable HTTP session. Endpoint contents,
// Authorization header and upstream response text must remain Windows-local.
type HTTPMCP struct {
 endpoint string
 client *http.Client
 authorization string
 mu sync.Mutex
 sessionID string
 version string
 closed bool
 done chan struct{}
 closeOnce sync.Once
 streamCancel context.CancelFunc
 onToolsChanged func()
 timeout time.Duration
}

func ValidateHTTPEndpoint(raw string)error{
 u,err:=url.Parse(raw)
 if err!=nil||u==nil||u.Host==""||u.Fragment!=""||u.User!=nil{return errors.New("ERR_MCP_ENDPOINT_INVALID")}
 if u.Scheme!="http"&&u.Scheme!="https"{return errors.New("ERR_MCP_ENDPOINT_INVALID")}
 host:=u.Hostname()
 if u.Scheme=="http" {
  ip:=net.ParseIP(host)
  if !strings.EqualFold(host,"localhost")&&(ip==nil||!ip.IsLoopback()){return errors.New("ERR_MCP_TLS_REQUIRED")}
 }
 return nil
}
func newHTTPMCP(endpoint,authorization string)(*HTTPMCP,error){
 if err:=ValidateHTTPEndpoint(endpoint);err!=nil{return nil,err}
 client:=&http.Client{CheckRedirect:func(_ *http.Request,_ []*http.Request)error{return http.ErrUseLastResponse}}
 return &HTTPMCP{endpoint:endpoint,client:client,authorization:authorization,done:make(chan struct{}),timeout:120*time.Second},nil
}
func (h *HTTPMCP) close(){
 h.closeOnce.Do(func(){
  h.mu.Lock();h.closed=true;cancel:=h.streamCancel;h.mu.Unlock()
  if cancel!=nil{cancel()}
  close(h.done)
 })
}
func (h *HTTPMCP) isClosed()bool{
 h.mu.Lock();defer h.mu.Unlock();return h.closed
}
func (h *HTTPMCP) request(req any,expectedID int64)(json.RawMessage,error){
 h.mu.Lock()
 if h.closed{h.mu.Unlock();return nil,errors.New("MCP_HTTP_SESSION_CLOSED")}
 sessionID,version,timeout:=h.sessionID,h.version,h.timeout
 h.mu.Unlock()
 if timeout<=0{timeout=120*time.Second}
 ctx,cancel:=context.WithTimeout(context.Background(),timeout)
 defer cancel()
 b,err:=json.Marshal(req)
 if err!=nil{return nil,errors.New("MCP_HTTP_REQUEST_INVALID")}
 request,err:=http.NewRequestWithContext(ctx,http.MethodPost,h.endpoint,bytes.NewReader(b))
 if err!=nil{return nil,errors.New("MCP_HTTP_REQUEST_INVALID")}
 request.Header.Set("Content-Type","application/json")
 request.Header.Set("Accept","application/json, text/event-stream")
 if sessionID!=""{request.Header.Set("Mcp-Session-Id",sessionID)}
 if version!=""{request.Header.Set("MCP-Protocol-Version",version)}
 if h.authorization!=""{request.Header.Set("Authorization",h.authorization)}
 response,err:=h.client.Do(request)
 if err!=nil{h.close();return nil,errors.New("MCP_HTTP_NETWORK_OR_TIMEOUT")}
 defer response.Body.Close()
 if response.StatusCode==http.StatusTemporaryRedirect||response.StatusCode==http.StatusPermanentRedirect||response.StatusCode==http.StatusFound||response.StatusCode==http.StatusMovedPermanently||response.StatusCode==http.StatusSeeOther{
  return nil,errors.New("MCP_HTTP_REDIRECT_BLOCKED")
 }
 if response.StatusCode<200||response.StatusCode>299{
  if response.StatusCode==http.StatusNotFound || response.StatusCode==http.StatusMethodNotAllowed{return nil,errors.New("MCP_HTTP_ENDPOINT_UNSUPPORTED")}
  if response.StatusCode==http.StatusUnauthorized||response.StatusCode==http.StatusForbidden{return nil,errors.New("MCP_HTTP_AUTH_FAILED")}
  return nil,fmt.Errorf("MCP_HTTP_STATUS_%d",response.StatusCode)
 }
 if sid:=response.Header.Get("Mcp-Session-Id");sid!=""{
  if len(sid)>512||strings.ContainsAny(sid,"\r\n"){return nil,errors.New("MCP_HTTP_SESSION_INVALID")}
  h.mu.Lock()
  if !h.closed {h.sessionID=sid}
  h.mu.Unlock()
 }
 if expectedID==0 || response.StatusCode==http.StatusAccepted{return nil,nil}
 contentType:=strings.ToLower(response.Header.Get("Content-Type"))
 if strings.Contains(contentType,"text/event-stream"){
  return h.decodeEvents(response.Body,expectedID)
 }
 payload,err:=io.ReadAll(io.LimitReader(response.Body,maxHTTPBody+1))
 if err!=nil||len(payload)>maxHTTPBody{return nil,errors.New("MCP_HTTP_RESPONSE_INVALID")}
 return h.sanitizeResponse(payload,expectedID)
}
func (h *HTTPMCP) sanitizeResponse(payload []byte,expectedID int64)(json.RawMessage,error){
 var doc struct{
  JSONRPC string `json:"jsonrpc"`
  ID json.RawMessage `json:"id"`
  Method string `json:"method"`
  Error *struct{Code int `json:"code"`} `json:"error"`
 }
 if json.Unmarshal(payload,&doc)!=nil{return nil,errors.New("MCP_HTTP_RESPONSE_INVALID")}
 if doc.Method=="notifications/tools/list_changed"{h.notifyChanged();return nil,errors.New("MCP_HTTP_RESPONSE_INVALID")}
 if len(doc.ID)==0{return nil,errors.New("MCP_HTTP_RESPONSE_INVALID")}
 var id int64
 if json.Unmarshal(doc.ID,&id)!=nil||id!=expectedID{return nil,errors.New("MCP_HTTP_RESPONSE_ID_MISMATCH")}
 if doc.Error!=nil{
  return json.Marshal(map[string]any{"jsonrpc":"2.0","id":id,"error":map[string]any{"code":doc.Error.Code,"message":"MCP HTTP server error"}})
 }
 return json.RawMessage(payload),nil
}
func (h *HTTPMCP) notifyChanged(){
 h.mu.Lock();fn:=h.onToolsChanged;closed:=h.closed;h.mu.Unlock()
 if !closed&&fn!=nil{fn()}
}
func (h *HTTPMCP) decodeEvents(body io.Reader,expectedID int64)(json.RawMessage,error){
 scanner:=bufio.NewScanner(io.LimitReader(body,maxHTTPBody+1))
 scanner.Buffer(make([]byte,4096),maxHTTPBody)
 var data []string
 dispatch:=func()(json.RawMessage,bool,error){
  if len(data)==0{return nil,false,nil}
  raw:=[]byte(strings.Join(data,"\n"));data=nil
  var env struct{Method string `json:"method"`;ID json.RawMessage `json:"id"`}
  if json.Unmarshal(raw,&env)!=nil{return nil,false,errors.New("MCP_HTTP_SSE_INVALID")}
  if env.Method=="notifications/tools/list_changed"{h.notifyChanged();return nil,false,nil}
  if len(env.ID)==0{return nil,false,nil}
  result,err:=h.sanitizeResponse(raw,expectedID)
  if err!=nil{return nil,false,err}
  return result,true,nil
 }
 for scanner.Scan(){
  line:=strings.TrimSuffix(scanner.Text(),"\r")
  if line==""{
   result,done,err:=dispatch()
   if err!=nil{return nil,err}
   if done{return result,nil}
   continue
  }
  if strings.HasPrefix(line,"data:"){data=append(data,strings.TrimSpace(strings.TrimPrefix(line,"data:")))}
 }
 if scanner.Err()!=nil{return nil,errors.New("MCP_HTTP_SSE_READ_FAILED")}
 result,done,err:=dispatch()
 if err!=nil{return nil,err}
 if done{return result,nil}
 return nil,errors.New("MCP_HTTP_SSE_RESPONSE_MISSING")
}
func (h *HTTPMCP) startEvents(){
 h.mu.Lock()
 if h.closed{h.mu.Unlock();return}
 ctx,cancel:=context.WithCancel(context.Background())
 h.streamCancel=cancel
 id:=h.sessionID;v:=h.version
 h.mu.Unlock()
 go func(){
  defer cancel()
  req,err:=http.NewRequestWithContext(ctx,http.MethodGet,h.endpoint,nil)
  if err!=nil{return}
  req.Header.Set("Accept","text/event-stream")
  if id!=""{req.Header.Set("Mcp-Session-Id",id)}
  if v!=""{req.Header.Set("MCP-Protocol-Version",v)}
  if h.authorization!=""{req.Header.Set("Authorization",h.authorization)}
  resp,err:=h.client.Do(req)
  if err!=nil{return}
  defer resp.Body.Close()
  if resp.StatusCode!=http.StatusOK||!strings.Contains(resp.Header.Get("Content-Type"),"text/event-stream"){return}
  scanner:=bufio.NewScanner(io.LimitReader(resp.Body,maxHTTPBody+1))
  scanner.Buffer(make([]byte,4096),maxHTTPBody)
  var data []string
  for scanner.Scan(){
   line:=strings.TrimSuffix(scanner.Text(),"\r")
   if line!="" {
    if strings.HasPrefix(line,"data:"){data=append(data,strings.TrimSpace(strings.TrimPrefix(line,"data:")))}
    continue
   }
   if len(data)==0{continue}
   var env struct{Method string `json:"method"`}
   if json.Unmarshal([]byte(strings.Join(data,"\n")),&env)==nil&&env.Method=="notifications/tools/list_changed"{h.notifyChanged()}
   data=nil
  }
 }()
}
