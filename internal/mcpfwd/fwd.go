// Package mcpfwd maintains persistent MCP sessions to project-local MCP servers
// and forwards tools/list + tools/call on behalf of the Mac agent.
//
// Stdio and Streamable HTTP share the same per-project lifecycle and tool list
// notification boundary; opaque HTTP endpoints never leave Windows.
package mcpfwd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MCPTool describes a tool exposed by a project's MCP server.
type MCPTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	Annotations map[string]any `json:"annotations,omitempty"`
	Meta map[string]any `json:"_meta,omitempty"`
}

// Session is a live connection to one project's MCP stdio server.
type Session struct {
	ProjectName string
	tools       []MCPTool
	mu          sync.Mutex
	cmd         *exec.Cmd
	stream      *mcpStream
	http        *HTTPMCP
	transport string
	endpoint string
	refreshMu sync.Mutex
	nextID      atomic.Int64
	closed      bool
}

// Tools returns the tool list (may be empty if not yet initialized).
func (s *Session) Tools() []MCPTool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]MCPTool, len(s.tools))
	copy(cp, s.tools)
	return cp
}

// CallTool sends a tools/call request and returns the raw result JSON.
func (s *Session) CallTool(toolName string, args map[string]any) (json.RawMessage, error) {
	id := s.nextID.Add(1)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}
	raw, err := s.roundTrip(id, req)
	if err != nil {
		return nil, err
	}
	return responseResult(raw)
}

// Close terminates the session.
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	cmd := s.cmd
	stream := s.stream
	h:=s.http
	s.mu.Unlock()
	if h!=nil{h.close()}

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if stream != nil {
		stream.close()
	}
}

func (s *Session) isClosed() bool {
	s.mu.Lock()
	closed := s.closed
	stream := s.stream
	h:=s.http
	s.mu.Unlock()
	if closed{return true}
	if h!=nil{return h.isClosed()}
	if stream==nil{return true}
	select {
	case <-stream.done:
		return true
	default:
		return false
	}
}

// roundTrip sends a JSON-RPC request and waits for the matching response.
func (s *Session) roundTrip(id int64, req map[string]any) (json.RawMessage, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, fmt.Errorf("session đã đóng")
	}
	stream := s.stream
	h:=s.http
	s.mu.Unlock()
	if h!=nil{return h.request(req,id)}
	if stream == nil {
		return nil, fmt.Errorf("session MCP không có transport đang hoạt động")
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	respCh := stream.registerWaiter(id)
	defer stream.unregisterWaiter(id)

	if err := stream.writeLine(data); err != nil {
		return nil, fmt.Errorf("ghi request thất bại: %w", err)
	}

	select {
	case raw := <-respCh:
		return raw, nil
	case <-stream.done:
		if err := stream.Err(); err != nil {
			return nil, fmt.Errorf("MCP server connection closed: %w", err)
		}
		return nil, fmt.Errorf("MCP server connection closed")
	case <-time.After(120 * time.Second):
		return nil, fmt.Errorf("timeout chờ phản hồi từ MCP server")
	}
}

// Manager manages one Session per project MCP config.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session // project name → session
	onChange func()              // called when tool list changes (triggers list_changed)
}

// NewManager creates an empty Manager.
// onChange is called whenever the tool list changes (nil = no-op).
func NewManager(onChange func()) *Manager {
	if onChange == nil {
		onChange = func() {}
	}
	return &Manager{
		sessions: make(map[string]*Session),
		onChange: onChange,
	}
}

// Session retains legacy stdio callers and is not a URL transport bypass.
func (m *Manager) Session(projName,projectRoot,transport string,argv []string)(*Session,error){
 return m.SessionWithEndpoint(projName,projectRoot,transport,argv,"","")
}

// SessionWithEndpoint opens stdio or Streamable HTTP on Windows. The caller
// resolves the endpoint/auth only from the local OS-protected secret store.
func (m *Manager) SessionWithEndpoint(projName,projectRoot,transport string,argv []string,endpoint,authorization string)(*Session,error){
 m.mu.Lock()
 if old:=m.sessions[projName];old!=nil{
  if !old.isClosed() && old.transport==transport && old.endpoint==endpoint {
   m.mu.Unlock();return old,nil
  }
  delete(m.sessions,projName)
  old.Close()
 }
 var s *Session
 var err error
 switch transport{
 case "stdio":
  s,err=openSession(projName,projectRoot,transport,argv)
 case "streamable_http":
  s,err=openHTTPSession(projName,endpoint,authorization)
 default:
  err=fmt.Errorf("ERR_MCP_TRANSPORT_UNSUPPORTED")
 }
 if err!=nil{m.mu.Unlock();return nil,err}
 m.sessions[projName]=s
 m.mu.Unlock()
 // A server notification invalidates tool discovery. A stale/closed
 // session cannot refresh a newer session for the same project.
 changed:=func(){
  go func(){
   s.refreshMu.Lock();defer s.refreshMu.Unlock()
   if s.isClosed(){return}
   if err:=s.refreshTools();err!=nil{return}
   m.mu.Lock();current:=m.sessions[projName]==s;m.mu.Unlock()
   if current&&!s.isClosed(){m.onChange()}
  }()
 }
 if s.stream!=nil{s.stream.setChangeHandler(changed)}
 if s.http!=nil{
  s.http.mu.Lock();s.http.onToolsChanged=changed;s.http.mu.Unlock()
 }
 var done <-chan struct{}
 if s.http!=nil{done=s.http.done}else{done=s.stream.done}
 go func(){
  <-done
  m.mu.Lock();current:=m.sessions[projName]==s
  if current{delete(m.sessions,projName)}
  m.mu.Unlock()
  if current{m.onChange()}
 }()
 m.onChange()
 return s,nil
}

// Probe initializes and lists tools without calling a project tool or
// mutating the live project manager. It never modifies grants or profiles.
func Probe(projName,root,transport string,argv []string,endpoint,authorization string)([]MCPTool,error){
 var s *Session
 var err error
 switch transport{
 case "stdio":s,err=openSession(projName,root,transport,argv)
 case "streamable_http":s,err=openHTTPSession(projName,endpoint,authorization)
 default:return nil,fmt.Errorf("ERR_MCP_TRANSPORT_UNSUPPORTED")
 }
 if err!=nil{return nil,err}
 defer s.Close()
 return s.Tools(),nil
}

// CloseProject closes the session for the named project, if any.
func (m *Manager) CloseProject(projName string) {
	m.mu.Lock()
	s := m.sessions[projName]
	delete(m.sessions, projName)
	m.mu.Unlock()
	if s != nil {
		s.Close()
		m.onChange()
	}
}

// AllTools returns all tools from all open sessions, prefixed "projName__toolName".
// Tools whose qualified name would exceed 64 characters are skipped with a logged warning.
func (m *Manager) AllTools() ([]MCPTool, []string) {
	m.mu.Lock()
	sessions := make(map[string]*Session, len(m.sessions))
	for k, v := range m.sessions {
		sessions[k] = v
	}
	m.mu.Unlock()

	var out []MCPTool
	var warnings []string
	for projName, s := range sessions {
		for _, t := range s.Tools() {
			qualName := projName + "__" + t.Name
			if len(qualName) > 64 {
				warnings = append(warnings, fmt.Sprintf("MCP tool %q exceeds the 64-character name limit", qualName))
				continue
			}
			out = append(out, MCPTool{
				Name:        qualName,
				Description: fmt.Sprintf("[%s] %s", projName, t.Description),
				InputSchema: t.InputSchema,
				Title:t.Title,
				OutputSchema:t.OutputSchema,
				Annotations:t.Annotations,
				Meta:t.Meta,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	sort.Strings(warnings)
	return out, warnings
}

// CallTool routes a qualified "project__tool" call to the correct session.
func (m *Manager) CallTool(qualName string, args map[string]any) (json.RawMessage, error) {
	projName := ExtractProject(qualName)
	if projName == "" {
		return nil, fmt.Errorf("tên tool không hợp lệ (thiếu tiền tố project__): %s", qualName)
	}
	toolName := strings.TrimPrefix(qualName, projName+"__")
	if toolName == "" {
		return nil, fmt.Errorf("tên tool không hợp lệ (thiếu tên tool): %s", qualName)
	}

	m.mu.Lock()
	s := m.sessions[projName]
	m.mu.Unlock()

	if s == nil || s.isClosed() {
		return nil, fmt.Errorf("không có phiên MCP mở cho project '%s'", projName)
	}
	return s.CallTool(toolName, args)
}

// ExtractProject returns the project prefix from a qualified project__tool name.
func ExtractProject(qualName string) string {
	idx := strings.Index(qualName, "__")
	if idx <= 0 {
		return ""
	}
	return qualName[:idx]
}

// openSession launches the stdio MCP server and runs MCP initialize.
func openSession(projName, projectRoot, transport string, argv []string) (*Session, error) {
	if transport != "stdio" {
		return nil, fmt.Errorf("transport '%s' chưa được hỗ trợ; chỉ hỗ trợ stdio", transport)
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("mcp.command không được để trống cho project '%s'", projName)
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = projectRoot
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	// Suppress child console window on Windows.
	applyNoWindow(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("không khởi động được MCP server: %w", err)
	}

	stream := newMCPStream(stdinPipe, stdoutPipe)

	s := &Session{
		ProjectName: projName,
		cmd:         cmd,
		stream:      stream,
		transport:transport,
	}

	// Run MCP initialize handshake.
	if err := s.initialize(); err != nil {
		s.Close()
		<-stream.done
		_ = cmd.Wait()
		return nil, fmt.Errorf("initialize MCP server thất bại: %w", err)
	}

	// Fetch initial tool list.
	if err := s.refreshTools(); err != nil {
		s.Close()
		<-stream.done
		_ = cmd.Wait()
		return nil, fmt.Errorf("tools/list failed: %w", err)
	}

	// Watch child process for unexpected exit.
	go func() {
		_ = cmd.Wait()
		stream.close()
	}()

	return s, nil
}

func (s *Session) initialize() error {
	id := s.nextID.Add(1)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo":      map[string]string{"name": "lanbridge", "version": "1.0.0"},
			"capabilities":    map[string]any{},
		},
	}
	raw, err := s.roundTrip(id, req)
	if err != nil {
		return err
	}
	initResult, err := responseResult(raw)
	if err!=nil{return fmt.Errorf("initialize response: %w",err)}
	if s.http!=nil{
		var response struct{ProtocolVersion string `json:"protocolVersion"`}
		if json.Unmarshal(initResult,&response)!=nil||response.ProtocolVersion=="" {
			return fmt.Errorf("MCP_HTTP_NEGOTIATION_INVALID")
		}
		s.http.mu.Lock();s.http.version=response.ProtocolVersion;s.http.mu.Unlock()
	}
	// Send initialized notification (no response expected).
	notif := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	data, _ := json.Marshal(notif)
	if s.http!=nil{_,err:=s.http.request(notif,0);return err}
	return s.stream.writeLine(data)
}

func (s *Session) refreshTools() error {
 var all []MCPTool
 cursor:=""
 seen:=map[string]bool{}
 for page:=0;page<32;page++{
  id:=s.nextID.Add(1)
  req:=map[string]any{"jsonrpc":"2.0","id":id,"method":"tools/list"}
  if cursor!=""{req["params"]=map[string]any{"cursor":cursor}}
  raw,err:=s.roundTrip(id,req)
  if err!=nil{return err}
  resultJSON,err:=responseResult(raw)
  if err!=nil{return err}
  var result struct{Tools []MCPTool `json:"tools"`;NextCursor string `json:"nextCursor"`}
  if err:=json.Unmarshal(resultJSON,&result);err!=nil{return fmt.Errorf("decode tools/list result: %w",err)}
  for _,tool:=range result.Tools{
   if tool.Name==""{return fmt.Errorf("tools/list returned unnamed tool")}
   all=append(all,tool)
  }
  if result.NextCursor==""{
   s.mu.Lock();s.tools=all;s.mu.Unlock()
   return nil
  }
  if seen[result.NextCursor]{return fmt.Errorf("MCP tools/list cursor loop")}
  seen[result.NextCursor]=true
  cursor=result.NextCursor
 }
 return fmt.Errorf("MCP tools/list pagination exceeds 32 pages")
}

func responseResult(raw json.RawMessage) (json.RawMessage, error) {
	var response struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("decode MCP response: %w", err)
	}
	if response.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", response.Error.Code, response.Error.Message)
	}
	if len(response.Result) == 0 || string(response.Result) == "null" {
		return nil, fmt.Errorf("MCP response has no result")
	}
	return response.Result, nil
}

// ---------------------------------------------------------------------------
// mcpStream — thread-safe newline-delimited JSON transport over stdio
// ---------------------------------------------------------------------------

type mcpStream struct {
	wMu       sync.Mutex
	writer    io.WriteCloser
	waiters   map[int64]chan json.RawMessage
	wMu2      sync.Mutex
	done      chan struct{}
	closeOnce sync.Once
	errMu     sync.Mutex
	err       error
	changeMu sync.RWMutex
	onChange func()
}

func newMCPStream(w io.WriteCloser, r io.Reader) *mcpStream {
	s := &mcpStream{
		writer:  w,
		waiters: make(map[int64]chan json.RawMessage),
		done:    make(chan struct{}),
	}
	go s.readLoop(r)
	return s
}

func (s *mcpStream) writeLine(data []byte) error {
	s.wMu.Lock()
	defer s.wMu.Unlock()
	n, err := s.writer.Write(append(data, '\n'))
	if err == nil && n != len(data)+1 {
		err = io.ErrShortWrite
	}
	return err
}

func (s *mcpStream) registerWaiter(id int64) chan json.RawMessage {
	ch := make(chan json.RawMessage, 1)
	s.wMu2.Lock()
	s.waiters[id] = ch
	s.wMu2.Unlock()
	return ch
}

func (s *mcpStream) unregisterWaiter(id int64) {
	s.wMu2.Lock()
	delete(s.waiters, id)
	s.wMu2.Unlock()
}

func (s *mcpStream) close() {
	s.closeOnce.Do(func() {
		close(s.done)
		_ = s.writer.Close()
	})
}

func (s *mcpStream) setError(err error) {
	s.errMu.Lock()
	s.err = err
	s.errMu.Unlock()
}

func (s *mcpStream) Err() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.err
}

func (s *mcpStream) setChangeHandler(handler func()){
 s.changeMu.Lock();s.onChange=handler;s.changeMu.Unlock()
}

func (s *mcpStream) readLoop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Extract the "id" field to route to the right waiter.
		var envelope struct {
			ID *int64 `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			s.setError(fmt.Errorf("invalid JSON-RPC response: %w", err))
			s.close()
			return
		}
		if envelope.ID == nil {
			if envelope.Method=="notifications/tools/list_changed"{
				s.changeMu.RLock();notify:=s.onChange;s.changeMu.RUnlock()
				if notify!=nil{notify()}
			}
			continue
		}

		cp := make([]byte, len(line))
		copy(cp, line)

		s.wMu2.Lock()
		ch := s.waiters[*envelope.ID]
		s.wMu2.Unlock()

		if ch != nil {
			select {
			case ch <- cp:
			default:
			}
		}
		// Notifications are dispatched without blocking the response reader.
	}
	if err := scanner.Err(); err != nil {
		s.setError(fmt.Errorf("read MCP server output: %w", err))
	} else {
		s.setError(io.EOF)
	}
	s.close()
}
