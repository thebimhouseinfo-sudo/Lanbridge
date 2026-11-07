package agentmcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Error codes exposed to agents. Each has one concrete next step so an agent
// never has to guess what to do after a failure.
const (
	CodeBridgeNotRunning = "BRIDGE_NOT_RUNNING"
	CodeHostUnreachable  = "HOST_UNREACHABLE"
	CodeEmptyResult      = "EMPTY_RESULT"
	CodeOutsideScope     = "OUTSIDE_SCOPE"
	CodeConfigNotOpen    = "CONFIG_NOT_OPEN"
	CodeConfigExists     = "CONFIG_ALREADY_EXISTS"
	CodeProjectNotFound  = "ERR_PROJECT"
	CodeRemoteConfig     = "ERR_REMOTE_CONFIG"
	CodeConfigProtected  = "ERR_CONFIG_PROTECTED"
	CodeToolUnavailable  = "TOOL_UNAVAILABLE"
	CodeInvalidArgs      = "INVALID_ARGUMENTS"
	CodeInternal         = "INTERNAL_ERROR"
)

// knownErrorCodes are matched by substring in host and bridge error messages.
// Order matters: more specific codes come first.
var knownErrorCodes = []string{
	CodeOutsideScope,
	CodeConfigNotOpen,
	CodeConfigExists,
	CodeConfigProtected,
	CodeRemoteConfig,
	CodeProjectNotFound,
}

var nextSteps = map[string]string{
	CodeBridgeNotRunning: "Mở LAN Bridge trên Mac (ứng dụng hoặc `bridge client`), rồi thử lại.",
	CodeHostUnreachable:  "Mở LAN Bridge trên Windows và kiểm tra cả hai máy cùng mạng. Bridge sẽ tự tìm lại host khi IP đổi.",
	CodeEmptyResult:      "Host không trả nội dung. Chạy `bridge doctor` để kiểm tra từng bước, rồi thử lại.",
	CodeOutsideScope:     "Thư mục nằm ngoài phạm vi đã duyệt. Gọi request_access với đường dẫn này và chờ Windows user duyệt trong LAN Bridge trên Windows.",
	CodeConfigNotOpen:    "Blacklist của project chưa được mở trên Mac. Mở project trong LAN Bridge trên Mac (hoặc gọi config_load với project_id), rồi thử lại. Bước này là bảo vệ có chủ đích, không tự bỏ qua.",
	CodeConfigExists:     "Project đã tồn tại. Dùng project hiện có (windows_config action=status) thay vì tạo mới.",
	CodeProjectNotFound:  "Project không tồn tại trên Windows. Gọi windows_config action=list để xem project hiện có.",
	CodeRemoteConfig:     "Windows từ chối thao tác cấu hình. Xem thông báo lỗi và kiểm tra quyền trên Windows.",
	CodeConfigProtected:  "File cấu hình blacklist của Mac không được đồng bộ sang Windows. Đây là hành vi bảo vệ, không cần sửa.",
	CodeToolUnavailable:  "Tool này không có sẵn. Gọi lại tools/list để lấy danh sách hiện tại.",
	CodeInvalidArgs:      "Kiểm tra tham số theo inputSchema của tool.",
	CodeInternal:         "Lỗi nội bộ. Chạy `bridge doctor` và gửi kết quả cho người phát triển.",
}

// ClassifyError returns the stable error code and next step for a message.
func ClassifyError(message string) (code string, nextStep string) {
	code = CodeInternal
	switch {
	case strings.Contains(message, "chưa mở") || strings.Contains(message, "auth information is unavailable") ||
		strings.Contains(message, "mcp_auth.json"):
		code = CodeBridgeNotRunning
	case strings.Contains(message, "chưa kết nối") || strings.Contains(message, "chưa sẵn sàng") ||
		strings.Contains(message, "Bridge Client trả HTTP") || strings.Contains(message, "connection refused"):
		code = CodeHostUnreachable
	case strings.Contains(message, "not available"):
		code = CodeToolUnavailable
	default:
		for _, candidate := range knownErrorCodes {
			if strings.Contains(message, candidate) {
				code = candidate
				break
			}
		}
	}
	return code, nextSteps[code]
}

// errorStructuredContent is the machine-readable form of a failed tool call.
func errorStructuredContent(message string) map[string]any {
	code, next := ClassifyError(message)
	return map[string]any{
		"ok":         false,
		"state":      "error",
		"error_code": code,
		"message":    message,
		"next_step":  next,
	}
}

// ensureNonEmptyResult guarantees a forwarded result always carries text.
// A host reply without content (for example `{}`) becomes an explicit
// EMPTY_RESULT error that includes the raw reply, never a silent empty result.
func ensureNonEmptyResult(toolName string, result map[string]any) map[string]any {
	if result["isError"] == true {
		return result
	}
	if content, ok := result["content"].([]any); ok && len(content) > 0 {
		return result
	}
	raw, err := json.Marshal(result)
	if err != nil || len(result) == 0 {
		return errorResult(fmt.Sprintf("Host trả kết quả rỗng cho %s", toolName), CodeEmptyResult)
	}
	return map[string]any{
		"content": []map[string]string{{
			"type": "text",
			"text": fmt.Sprintf("Host trả dữ liệu cho %s không có phần nội dung. Dữ liệu thô: %s", toolName, raw),
		}},
		"structuredContent": result,
	}
}

func errorResult(message string, code string) map[string]any {
	return map[string]any{
		"content": []map[string]string{{"type": "text", "text": "LỖI: " + message}},
		"isError": true,
		"structuredContent": map[string]any{
			"ok":         false,
			"state":      "error",
			"error_code": code,
			"message":    message,
			"next_step":  nextSteps[code],
		},
	}
}
