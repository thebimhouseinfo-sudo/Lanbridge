package agentmcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/protocol"
)

const maxLocalSyncBytes = 5 * 1024 * 1024

// JSONRPCRequest represents an incoming stdio JSON-RPC request
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents an outgoing stdio JSON-RPC response
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents a standard JSON-RPC error
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Tool describes an MCP Tool
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// StdioServer handles the stdio MCP protocol for Claude Code
type StdioServer struct {
	inReader       *bufio.Reader
	outWriter      io.Writer
	outMu          sync.Mutex
	auth           *AuthInfo
	httpClient     *http.Client
	commandMu      sync.Mutex
	commandOffsets map[string]commandOutputOffsets
	activityLabel  string
	activityFailed bool
}

type commandOutputOffsets struct {
	stdout int64
	stderr int64
}

// NewStdioServer creates an instance with custom in and out
func NewStdioServer(in io.Reader, out io.Writer) *StdioServer {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	return &StdioServer{
		inReader:       bufio.NewReader(in),
		outWriter:      out,
		httpClient:     &http.Client{},
		commandOffsets: make(map[string]commandOutputOffsets),
	}
}

// Run starts the read-eval-print loop on stdin/stdout
func (s *StdioServer) Run() error {
	// Try reading auth info to connect to local bridge client
	auth, err := ReadAuthFile()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[bridge mcp] Cảnh báo: %v\n", err)
	}
	s.auth = auth
	watchCtx, stopWatching := context.WithCancel(context.Background())
	defer stopWatching()
	go s.watchMCPNotifications(watchCtx)

	for {
		line, err := s.inReader.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(nil, -32700, "Parse error")
			continue
		}

		s.handleRequest(&req)
	}
}

func (s *StdioServer) watchMCPNotifications(ctx context.Context) {
	client := &http.Client{}
	retryDelay := time.Second
	for {
		auth, err := ReadAuthFile()
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(os.Stderr, "[bridge mcp] Không đọc được thông tin nhận notification: %v\n", err)
			}
			if !waitForRetry(ctx, retryDelay) {
				return
			}
			retryDelay = cappedRetryDelay(retryDelay)
			continue
		}

		url := fmt.Sprintf("http://127.0.0.1:%d/api/mcp/events?t=%s", auth.Port, auth.Token)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[bridge mcp] Không tạo được kết nối notification: %v\n", err)
			if !waitForRetry(ctx, retryDelay) {
				return
			}
			retryDelay = cappedRetryDelay(retryDelay)
			continue
		}
		response, err := client.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			fmt.Fprintf(os.Stderr, "[bridge mcp] Kết nối notification chưa sẵn sàng: %v\n", err)
			if !waitForRetry(ctx, retryDelay) {
				return
			}
			retryDelay = cappedRetryDelay(retryDelay)
			continue
		}
		if response.StatusCode != http.StatusOK {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
			fmt.Fprintf(os.Stderr, "[bridge mcp] Kênh notification trả HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
			if readErr != nil {
				fmt.Fprintf(os.Stderr, " (lỗi đọc phản hồi: %v)", readErr)
			}
			fmt.Fprintln(os.Stderr)
			if !waitForRetry(ctx, retryDelay) {
				return
			}
			retryDelay = cappedRetryDelay(retryDelay)
			continue
		}

		retryDelay = time.Second
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			var notification struct {
				Method string `json:"method"`
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if err := json.Unmarshal([]byte(data), &notification); err != nil {
				fmt.Fprintf(os.Stderr, "[bridge mcp] Notification JSON không hợp lệ: %v\n", err)
				continue
			}
			if notification.Method == "notifications/tools/list_changed" {
				s.sendNotification(notification.Method)
			}
		}
		_ = response.Body.Close()
		if ctx.Err() != nil {
			return
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "[bridge mcp] Lỗi đọc kênh notification: %v\n", err)
		}
		if !waitForRetry(ctx, retryDelay) {
			return
		}
		retryDelay = cappedRetryDelay(retryDelay)
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func cappedRetryDelay(delay time.Duration) time.Duration {
	delay *= 2
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func (s *StdioServer) handleRequest(req *JSONRPCRequest) {
	switch req.Method {
	case "initialize":
		res := map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]string{
				"name":    "lanbridge",
				"version": "1.0.0",
			},
			"capabilities": map[string]any{
				"tools": map[string]bool{
					"listChanged": true,
				},
			},
		}
		s.sendResult(req.ID, res)

	case "notifications/initialized":
		// No-op

	case "tools/list":
		s.refreshAuthInfo()
		s.handleToolsList(req)

	case "tools/call":
		s.handleToolsCall(req)

	default:
		if strings.HasPrefix(req.Method, "notifications/") {
			return
		}
		s.sendError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func (s *StdioServer) handleToolsList(req *JSONRPCRequest) {
	tools := []Tool{
		{
			Name:        "bridge_status",
			Description: "Kiểm tra trạng thái kết nối LAN Bridge giữa Mac và Windows",
			InputSchema: map[string]any{
				"type": "object",
			},
		},
		{
			Name:        "request_access",
			Description: "Yêu cầu Windows user cấp quyền đọc hoặc sync thư mục",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]string{"type": "string", "description": "Đường dẫn thư mục trên Windows"},
					"capabilities": map[string]any{
						"type":        "array",
						"items":       map[string]string{"type": "string"},
						"description": "Các quyền yêu cầu (read, sync)",
					},
				},
				"required": []string{"path", "capabilities"},
			},
		},
		{
			Name:        "list_dir",
			Description: "Liệt kê file/thư mục trên Windows (cần quyền read)",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]string{"type": "string", "description": "Đường dẫn thư mục"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "file_info",
			Description: "Xem metadata của 1 file trên Windows (cần quyền read)",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]string{"type": "string", "description": "Đường dẫn file"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "read_file",
			Description: "Đọc nội dung file trên Windows theo đoạn hoặc đọc dòng cuối log (cần quyền read)",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":       map[string]string{"type": "string", "description": "Đường dẫn file"},
					"offset":     map[string]string{"type": "integer", "description": "Offset bắt đầu (byte)"},
					"length":     map[string]string{"type": "integer", "description": "Độ dài đọc tối đa (tối đa 1MB)"},
					"tail_lines": map[string]string{"type": "integer", "description": "Đọc N dòng cuối của log"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "sync_files",
			Description: "Đồng bộ file từ Mac sang Windows (cần quyền sync, tối đa 5MB). File tự đọc từ local_root trên Mac.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project":    map[string]string{"type": "string", "description": "Tên project trong config (ví dụ: CAI)"},
					"target":     map[string]string{"type": "string", "description": "'root' hoặc tên một related_dir"},
					"local_root": map[string]string{"type": "string", "description": "Thư mục gốc cục bộ trên Mac chứa các file"},
					"files": map[string]any{
						"type":        "array",
						"items":       map[string]string{"type": "string"},
						"description": "Danh sách đường dẫn file tương đối so với local_root (ví dụ: ['src/A.cs'])",
					},
				},
				"required": []string{"project", "target", "files"},
			},
		},
		{
			Name:        "run_command",
			Description: "Chạy lệnh build/test đã khai báo trong config trên Windows; trả job_id nếu lệnh chưa xong.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project":      map[string]string{"type": "string", "description": "Tên project trong config"},
					"command":      map[string]string{"type": "string", "description": "ID lệnh được khai báo trong config"},
					"wait_seconds": map[string]any{"type": "integer", "description": "Thời gian chờ tối đa; mặc định 600", "default": 600},
				},
				"required": []string{"project", "command"},
			},
		},
		{
			Name:        "command_result",
			Description: "Lấy trạng thái/output của lệnh. Khi hoàn tất, output đầy đủ được lưu vào file tạm cục bộ và trả đường dẫn.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"job_id":       map[string]string{"type": "string", "description": "job_id từ run_command"},
					"wait_seconds": map[string]any{"type": "integer", "description": "Thời gian chờ thêm, mặc định 0", "default": 0},
				},
				"required": []string{"job_id"},
			},
		},
		{
			Name:        "chat_send",
			Description: "Gửi tin nhắn text cho người dùng trong chat chung trên Mac và Windows. Dùng khi không thể chạy lệnh Windows hoặc cần người dùng tự thao tác; có thể gửi script nhiều dòng để người dùng copy và tự chạy. Bridge không tự thực thi nội dung chat.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]string{"type": "string", "description": "Lời nhắn hoặc script text cần gửi cho người dùng"},
				},
				"required": []string{"text"},
			},
		},
	}
	if s.auth != nil {
		projectTools, err := s.fetchProjectTools()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[bridge mcp] Không lấy được danh sách tool MCP dự án: %v\n", err)
		} else {
			tools = append(tools, projectTools...)
		}
	}

	s.sendResult(req.ID, map[string]any{"tools": tools})
}

func (s *StdioServer) handleToolsCall(req *JSONRPCRequest) {
	var callParams struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &callParams); err != nil {
		s.sendError(req.ID, -32602, "Invalid params")
		return
	}

	switch callParams.Name {
	case "bridge_status", "request_access", "list_dir", "file_info", "read_file", "sync_files", "run_command", "command_result", "chat_send":
	default:
		if !isProjectToolName(callParams.Name) {
			s.sendToolError(req.ID, fmt.Sprintf("Tool %q is not available", callParams.Name))
			return
		}
	}

	if err := s.refreshAuthInfo(); err != nil {
		s.sendToolError(req.ID, "LAN Bridge trên Mac chưa mở")
		return
	}
	if label := activityLabel(callParams.Name, callParams.Arguments); label != "" {
		s.beginActivity(label)
		defer s.endActivity()
	}

	// Intercept sync_files: read files locally on Mac before forwarding
	if callParams.Name == "chat_send" {
		s.handleAgentChatSend(req.ID, callParams.Arguments)
		return
	}
	if callParams.Name == "sync_files" {
		s.handleLocalSyncCall(req.ID, callParams.Arguments)
		return
	}
	if callParams.Name == "run_command" || callParams.Name == "command_result" {
		s.forwardCommandTool(req.ID, callParams.Name, callParams.Arguments)
		return
	}

	// Forward the remaining supported tools to the local bridge client.
	s.forwardToBridgeClient(req.ID, callParams.Name, callParams.Arguments)
}

func (s *StdioServer) handleAgentChatSend(reqID any, args map[string]any) {
	text, ok := args["text"].(string)
	if !ok {
		s.sendToolError(reqID, "text is required and must be a string")
		return
	}
	if err := protocol.ValidateChatText(text); err != nil {
		s.sendToolError(reqID, err.Error())
		return
	}
	if err := s.sendChatToBridge(text); err != nil {
		s.sendToolError(reqID, err.Error())
		return
	}
	s.sendToolResult(reqID, "Tin nhắn đã được xác nhận trong chat Bridge.")
}

func (s *StdioServer) sendChatToBridge(text string) error {
	if s.auth == nil {
		return errors.New("LAN Bridge auth information is unavailable")
	}
	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("encode agent chat message: %w", err)
	}
	requestURL := fmt.Sprintf("http://127.0.0.1:%d/api/mcp/chat/send?t=%s", s.auth.Port, s.auth.Token)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create agent chat request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("chat delivery status is unknown; Bridge will not retry automatically: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		if readErr != nil {
			return fmt.Errorf("chat send failed with HTTP %d and response read error: %w", response.StatusCode, readErr)
		}
		return fmt.Errorf("chat send failed with HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func (s *StdioServer) beginActivity(label string) {
	s.activityLabel = label
	s.activityFailed = false
	s.reportActivity("running")
}

func (s *StdioServer) endActivity() {
	status := "completed"
	if s.activityFailed {
		status = "failed"
	}
	s.reportActivity(status)
	s.activityLabel = ""
	s.activityFailed = false
}

func (s *StdioServer) reportActivity(status string) {
	if s.auth == nil || s.activityLabel == "" {
		return
	}
	body, err := json.Marshal(map[string]string{"label": truncateUTF8(s.activityLabel, 160), "status": status})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[bridge mcp] Không mã hóa được trạng thái activity: %v\n", err)
		return
	}
	requestURL := fmt.Sprintf("http://127.0.0.1:%d/api/mcp/activity?t=%s", s.auth.Port, s.auth.Token)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[bridge mcp] Không tạo được request activity: %v\n", err)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.httpClient.Do(request)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[bridge mcp] Không cập nhật được activity Bridge: %v\n", err)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "[bridge mcp] Activity trả HTTP %d; đọc phản hồi lỗi: %v\n", response.StatusCode, readErr)
			return
		}
		fmt.Fprintf(os.Stderr, "[bridge mcp] Activity trả HTTP %d: %s\n", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
}

func activityLabel(toolName string, args map[string]any) string {
	switch toolName {
	case "bridge_status":
		return ""
	case "request_access":
		return "Yêu cầu quyền"
	case "list_dir":
		return "Liệt kê thư mục"
	case "file_info":
		return "Xem thông tin file"
	case "read_file":
		return "Đọc file"
	case "sync_files", "sync_files_batch":
		return "Đồng bộ file"
	case "run_command":
		project, _ := args["project"].(string)
		project = activityPart(project)
		if project != "" {
			return project + " · Chạy lệnh"
		}
		return "Chạy lệnh"
	case "command_result":
		return "Lấy kết quả lệnh"
	case "app_start":
		return "Mở ứng dụng"
	case "app_stop":
		return "Đóng ứng dụng"
	case "chat_send":
		return "Gửi tin nhắn cho user"
	default:
		if separator := strings.Index(toolName, "__"); separator > 0 && separator+2 < len(toolName) {
			return activityPart(toolName[:separator]) + " · " + activityPart(toolName[separator+2:])
		}
		return activityPart(toolName)
	}
}

func activityPart(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	if len(value) > 64 {
		value = string([]rune(value)[:64])
	}
	return value
}

func truncateUTF8(value string, maxBytes int) string {
	for len(value) > maxBytes {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}

func isProjectToolName(name string) bool {
	separator := strings.Index(name, "__")
	return separator > 0 && separator+2 < len(name) &&
		name != "__list_tools" && name != "__tools_changed"
}

func (s *StdioServer) handleLocalSyncCall(reqID any, args map[string]any) {
	localRoot := ""
	if rawRoot, exists := args["local_root"]; exists {
		var ok bool
		localRoot, ok = rawRoot.(string)
		if !ok {
			s.sendToolError(reqID, "local_root must be a directory path")
			return
		}
	}
	if localRoot == "" {
		var err error
		localRoot, err = os.Getwd()
		if err != nil {
			s.sendToolError(reqID, fmt.Sprintf("Không thể xác định thư mục làm việc: %v", err))
			return
		}
	}
	project, projectOK := args["project"].(string)
	target, targetOK := args["target"].(string)
	if !projectOK || project == "" || !targetOK || target == "" {
		s.sendToolError(reqID, "project and target are required")
		return
	}
	root, err := resolveLocalSyncRoot(localRoot)
	if err != nil {
		s.sendToolError(reqID, err.Error())
		return
	}

	rawFiles, ok := args["files"].([]any)
	if !ok || len(rawFiles) == 0 {
		s.sendToolError(reqID, "files list cannot be empty")
		return
	}

	var items []map[string]any
	var totalBytes int64

	for _, rf := range rawFiles {
		relPath, ok := rf.(string)
		if !ok || relPath == "" {
			s.sendToolError(reqID, "every files entry must be a non-empty relative path")
			return
		}
		fullLocalPath, err := resolveLocalSyncPath(root, relPath)
		if err != nil {
			s.sendToolError(reqID, err.Error())
			return
		}
		data, err := readLocalSyncFile(fullLocalPath, int64(maxLocalSyncBytes)-totalBytes)
		if err != nil {
			s.sendToolError(reqID, fmt.Sprintf("Không thể đọc file cục bộ %s: %v", relPath, err))
			return
		}
		totalBytes += int64(len(data))
		items = append(items, map[string]any{
			"rel_path": relPath,
			"data":     data,
		})
	}

	forwardPayload := map[string]any{
		"project": project,
		"target":  target,
		"files":   items,
	}

	s.forwardToBridgeClient(reqID, "sync_files_batch", forwardPayload)
}

func (s *StdioServer) forwardToBridgeClient(reqID any, toolName string, args map[string]any) {
	result, err := s.callBridgeClient(toolName, args)
	if err != nil {
		s.sendToolError(reqID, err.Error())
		return
	}
	s.sendResult(reqID, result)
}

func (s *StdioServer) callBridgeClient(toolName string, args map[string]any) (map[string]any, error) {
	if s.auth == nil {
		return nil, errors.New("LAN Bridge auth information is unavailable")
	}
	timeout, err := ToolCallTimeout(toolName, args)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/mcp/forward?t=%s", s.auth.Port, s.auth.Token)

	bodyBytes, err := json.Marshal(map[string]any{
		"tool": toolName,
		"args": args,
	})
	if err != nil {
		return nil, fmt.Errorf("Không thể mã hóa lời gọi MCP: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("Không thể tạo lời gọi MCP: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("LAN Bridge trên Mac chưa mở hoặc chưa sẵn sàng: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Lỗi đọc phản hồi từ Bridge: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Bridge Client trả HTTP %d: %s", resp.StatusCode, string(respData))
	}

	var result map[string]any
	if err := json.Unmarshal(respData, &result); err != nil {
		return nil, fmt.Errorf("Bridge Client trả JSON không hợp lệ: %w", err)
	}
	if result == nil {
		return nil, errors.New("Bridge Client trả JSON rỗng")
	}
	return result, nil
}

type commandToolResult struct {
	JobID           string `json:"job_id"`
	Status          string `json:"status"`
	ExitCode        int    `json:"exit_code"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutOffset    int64  `json:"stdout_offset"`
	StderrOffset    int64  `json:"stderr_offset"`
	StdoutTotal     int64  `json:"stdout_total"`
	StderrTotal     int64  `json:"stderr_total"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
}

func (s *StdioServer) forwardCommandTool(reqID any, toolName string, args map[string]any) {
	if args == nil {
		args = make(map[string]any)
	}
	if toolName == "command_result" {
		jobID, ok := args["job_id"].(string)
		if !ok || jobID == "" {
			s.sendToolError(reqID, "job_id is required")
			return
		}
		if _, hasStdout := args["stdout_offset"]; !hasStdout {
			s.commandMu.Lock()
			args["stdout_offset"] = s.commandOffsets[jobID].stdout
			s.commandMu.Unlock()
		}
		if _, hasStderr := args["stderr_offset"]; !hasStderr {
			s.commandMu.Lock()
			args["stderr_offset"] = s.commandOffsets[jobID].stderr
			s.commandMu.Unlock()
		}
	}

	result, err := s.callBridgeClient(toolName, args)
	if err != nil {
		s.sendToolError(reqID, err.Error())
		return
	}
	if result["isError"] == true {
		s.sendResult(reqID, result)
		return
	}

	var commandResult commandToolResult
	data, err := json.Marshal(result)
	if err != nil {
		s.sendToolError(reqID, fmt.Sprintf("Không thể đọc kết quả lệnh: %v", err))
		return
	}
	if err := json.Unmarshal(data, &commandResult); err != nil {
		s.sendToolError(reqID, fmt.Sprintf("Kết quả lệnh không hợp lệ: %v", err))
		return
	}
	if commandResult.JobID == "" || commandResult.Status == "" {
		s.sendToolError(reqID, "Bridge Client trả thiếu job_id hoặc trạng thái lệnh")
		return
	}

	if commandResult.Status == "running" {
		s.commandMu.Lock()
		s.commandOffsets[commandResult.JobID] = commandOutputOffsets{
			stdout: commandResult.StdoutOffset,
			stderr: commandResult.StderrOffset,
		}
		s.commandMu.Unlock()
		s.sendToolResult(reqID, formatRunningCommand(commandResult))
		return
	}
	if commandResult.Status != "done" && commandResult.Status != "timeout" && commandResult.Status != "canceled" {
		s.sendToolError(reqID, fmt.Sprintf("Trạng thái lệnh không được hỗ trợ: %q", commandResult.Status))
		return
	}

	outputPath, truncated, err := s.collectCommandOutput(commandResult)
	if err != nil {
		s.sendToolError(reqID, fmt.Sprintf("Không thể lưu output của job %s: %v", commandResult.JobID, err))
		return
	}
	s.commandMu.Lock()
	delete(s.commandOffsets, commandResult.JobID)
	s.commandMu.Unlock()
	s.sendToolResult(reqID, fmt.Sprintf(
		"Job %s %s (exit code %d). Output lưu tại: %s%s",
		commandResult.JobID, commandResult.Status, commandResult.ExitCode, outputPath,
		truncationNote(truncated),
	))
}

func formatRunningCommand(result commandToolResult) string {
	var message strings.Builder
	fmt.Fprintf(&message, "Job %s đang chạy.", result.JobID)
	if result.Stdout != "" {
		fmt.Fprintf(&message, "\nstdout:\n%s", result.Stdout)
	}
	if result.Stderr != "" {
		fmt.Fprintf(&message, "\nstderr:\n%s", result.Stderr)
	}
	return message.String()
}

func (s *StdioServer) collectCommandOutput(initial commandToolResult) (string, bool, error) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	current := initial
	if current.StdoutOffset != 0 || current.StderrOffset != 0 {
		result, callErr := s.callBridgeClient("command_result", map[string]any{
			"job_id":        initial.JobID,
			"wait_seconds":  0,
			"stdout_offset": 0,
			"stderr_offset": 0,
		})
		if callErr != nil {
			return "", false, callErr
		}
		if result["isError"] == true {
			return "", false, errors.New(toolResultError(result))
		}
		encoded, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return "", false, fmt.Errorf("encode initial output chunk: %w", marshalErr)
		}
		if err := json.Unmarshal(encoded, &current); err != nil {
			return "", false, fmt.Errorf("decode initial output chunk: %w", err)
		}
	}
	truncated := current.StdoutTruncated || current.StderrTruncated
	complete := false

	for attempt := 0; attempt < 64; attempt++ {
		if current.StdoutTruncated {
			truncated = true
		}
		if current.StderrTruncated {
			truncated = true
		}
		if _, err := stdout.WriteString(current.Stdout); err != nil {
			return "", truncated, fmt.Errorf("buffer stdout: %w", err)
		}
		if _, err := stderr.WriteString(current.Stderr); err != nil {
			return "", truncated, fmt.Errorf("buffer stderr: %w", err)
		}
		if current.StdoutOffset >= current.StdoutTotal && current.StderrOffset >= current.StderrTotal {
			complete = true
			break
		}
		previousStdoutOffset, previousStderrOffset := current.StdoutOffset, current.StderrOffset
		next, err := s.callBridgeClient("command_result", map[string]any{
			"job_id":        initial.JobID,
			"wait_seconds":  0,
			"stdout_offset": current.StdoutOffset,
			"stderr_offset": current.StderrOffset,
		})
		if err != nil {
			return "", truncated, err
		}
		if next["isError"] == true {
			return "", truncated, errors.New(toolResultError(next))
		}
		encoded, err := json.Marshal(next)
		if err != nil {
			return "", truncated, fmt.Errorf("encode output chunk: %w", err)
		}
		if err := json.Unmarshal(encoded, &current); err != nil {
			return "", truncated, fmt.Errorf("decode output chunk: %w", err)
		}
		if current.JobID != initial.JobID {
			return "", truncated, errors.New("Bridge Client returned output for a different job")
		}
		if current.StdoutOffset <= previousStdoutOffset && current.StderrOffset <= previousStderrOffset {
			return "", truncated, errors.New("output collection made no progress")
		}
	}
	if !complete {
		return "", truncated, errors.New("output collection exceeded the chunk limit")
	}

	file, err := os.CreateTemp("", "lanbridge-command-*.log")
	if err != nil {
		return "", truncated, fmt.Errorf("create temporary output file: %w", err)
	}
	path := file.Name()
	removeOnError := true
	defer func() {
		if removeOnError {
			_ = os.Remove(path)
		}
	}()
	if truncated {
		if _, err := io.WriteString(file, "[output truncated; only retained output is available]\n"); err != nil {
			_ = file.Close()
			return "", truncated, fmt.Errorf("write truncation marker: %w", err)
		}
	}
	if _, err := fmt.Fprintf(file, "=== stdout ===\n%s\n=== stderr ===\n%s", stdout.String(), stderr.String()); err != nil {
		_ = file.Close()
		return "", truncated, fmt.Errorf("write command output: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return "", truncated, fmt.Errorf("flush command output: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", truncated, fmt.Errorf("close command output: %w", err)
	}
	removeOnError = false
	return path, truncated, nil
}

func toolResultError(result map[string]any) string {
	content, _ := result["content"].([]any)
	for _, item := range content {
		entry, _ := item.(map[string]any)
		if text, ok := entry["text"].(string); ok && text != "" {
			return text
		}
	}
	return "Bridge Client returned an MCP tool error"
}

func truncationNote(truncated bool) string {
	if truncated {
		return " (output buffer truncated)"
	}
	return ""
}

func (s *StdioServer) refreshAuthInfo() error {
	auth, err := ReadAuthFile()
	if err != nil {
		s.auth = nil
		return err
	}
	s.auth = auth
	return nil
}

func ToolCallTimeout(toolName string, args map[string]any) (time.Duration, error) {
	switch toolName {
	case "__list_tools":
		return 5 * time.Minute, nil
	case "request_access":
		return 5*time.Minute + 15*time.Second, nil
	case "run_command":
		seconds, err := requestedWaitSeconds(args, 600)
		if err != nil {
			return 0, err
		}
		if seconds == 0 {
			seconds = 600
		}
		return time.Duration(seconds)*time.Second + 15*time.Second, nil
	case "command_result":
		seconds, err := requestedWaitSeconds(args, 0)
		if err != nil {
			return 0, err
		}
		return time.Duration(seconds)*time.Second + 15*time.Second, nil
	default:
		if isProjectToolName(toolName) {
			return 2*time.Minute + 15*time.Second, nil
		}
		return 30 * time.Second, nil
	}
}

func requestedWaitSeconds(args map[string]any, defaultValue int) (int, error) {
	value, exists := args["wait_seconds"]
	if !exists {
		return defaultValue, nil
	}
	var seconds int
	switch number := value.(type) {
	case float64:
		if number < 0 || number > 600 {
			return 0, errors.New("wait_seconds must be between 0 and 600")
		}
		seconds = int(number)
		if float64(seconds) != number {
			return 0, errors.New("wait_seconds must be an integer")
		}
	case int:
		seconds = number
	default:
		return 0, errors.New("wait_seconds must be an integer")
	}
	if seconds < 0 || seconds > 600 {
		return 0, errors.New("wait_seconds must be between 0 and 600")
	}
	return seconds, nil
}

func resolveLocalSyncRoot(localRoot string) (string, error) {
	absolute, err := filepath.Abs(localRoot)
	if err != nil {
		return "", fmt.Errorf("invalid local_root: %w", err)
	}
	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("cannot resolve local_root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("cannot inspect local_root: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("local_root must be a directory")
	}
	return root, nil
}

func resolveLocalSyncPath(root, relPath string) (string, error) {
	if !filepath.IsLocal(relPath) {
		return "", fmt.Errorf("sync path %q must stay within local_root", relPath)
	}
	candidate, err := filepath.Abs(filepath.Join(root, relPath))
	if err != nil {
		return "", fmt.Errorf("invalid sync path %q: %w", relPath, err)
	}
	if !isWithinLocalRoot(root, candidate) {
		return "", fmt.Errorf("sync path %q escapes local_root", relPath)
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("cannot resolve sync path %q: %w", relPath, err)
	}
	if !isWithinLocalRoot(root, resolved) {
		return "", fmt.Errorf("sync path %q resolves outside local_root", relPath)
	}
	return resolved, nil
}

func isWithinLocalRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || filepath.IsAbs(relative) || relative == ".." {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func readLocalSyncFile(path string, remaining int64) ([]byte, error) {
	if remaining < 0 {
		return nil, errors.New("total sync payload exceeds 5 MB")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("sync source must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, remaining+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > remaining {
		return nil, errors.New("total sync payload exceeds 5 MB")
	}
	return data, nil
}

func (s *StdioServer) sendResult(id any, res any) {
	if result, ok := res.(map[string]any); ok && result["isError"] == true {
		s.activityFailed = true
	}
	s.outMu.Lock()
	defer s.outMu.Unlock()
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  res,
	}
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	_, _ = s.outWriter.Write(data)
}

func (s *StdioServer) sendNotification(method string) {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	data, _ := json.Marshal(map[string]string{
		"jsonrpc": "2.0",
		"method":  method,
	})
	_, _ = s.outWriter.Write(append(data, '\n'))
}

func (s *StdioServer) sendError(id any, code int, msg string) {
	s.activityFailed = true
	s.outMu.Lock()
	defer s.outMu.Unlock()
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: msg},
	}
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	_, _ = s.outWriter.Write(data)
}

func (s *StdioServer) sendToolError(id any, msg string) {
	s.activityFailed = true
	res := map[string]any{
		"content": []map[string]string{
			{"type": "text", "text": "LỖI: " + msg},
		},
		"isError": true,
	}
	s.sendResult(id, res)
}

func (s *StdioServer) sendToolResult(id any, text string) {
	res := map[string]any{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}
	s.sendResult(id, res)
}

// fetchProjectTools asks the bridge client for project MCP tools.
func (s *StdioServer) fetchProjectTools() ([]Tool, error) {
	if s.auth == nil {
		return nil, errors.New("bridge client auth information is unavailable")
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/mcp/project-tools?t=%s", s.auth.Port, s.auth.Token)
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("local bridge client request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			return nil, fmt.Errorf("local bridge client returned HTTP %d and response read failed: %w", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("local bridge client returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		Tools []Tool `json:"tools"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if err != nil {
		return nil, fmt.Errorf("read project tool list: %w", err)
	}
	if len(body) > 1024*1024 {
		return nil, errors.New("project tool list exceeds 1 MiB")
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode project tool list: %w", err)
	}
	return result.Tools, nil
}
