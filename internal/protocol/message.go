package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
)

const MaxChatTextBytes = 8 * 1024 * 1024

// Standard message types across LAN Test Bridge
const (
	TypeHeartbeat     = "heartbeat"
	TypeBye           = "bye"
	TypePairInit      = "pair_init"
	TypePairVerify    = "pair_verify"
	TypeUnpair        = "unpair"
	TypePermissionReq = "permission_request"
	TypePermissionRes = "permission_response"
	TypeFileList      = "file_list"
	TypeFileInfo      = "file_info"
	TypeFileRead      = "file_read"
	TypeFileSync      = "file_sync"
	TypeExecStart     = "exec_start"
	TypeExecResult    = "exec_result"
	TypeExecOutput    = "exec_output"
	TypeMCPCall       = "mcp_call"
	TypeAppControl    = "app_control"
	TypeChat          = "chat"
	TypeUIState       = "ui_state"
	TypeProjectStatus = "project_status"
)

// ErrorInfo represents structured error details
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ErrorInfo) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

// Message is the standard single-line JSON frame sent over the TLS connection
type Message struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *ErrorInfo      `json:"error,omitempty"`
	// PeerFingerprint is populated from the TLS session and is never part of the wire format.
	PeerFingerprint string `json:"-"`
}

// TransportStream provides thread-safe newline-delimited JSON reading and writing
type TransportStream struct {
	reader *bufio.Reader
	writer io.Writer
	mu     sync.Mutex
}

// NewTransportStream wraps an io.ReadWriter
func NewTransportStream(rw io.ReadWriter) *TransportStream {
	return &TransportStream{
		reader: bufio.NewReader(rw),
		writer: rw,
	}
}

// ReadMessage reads a single line and decodes it as a Message
func (s *TransportStream) ReadMessage() (*Message, error) {
	line, err := s.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	if len(line) == 0 {
		return nil, errors.New("empty message line")
	}

	var msg Message
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// WriteMessage encodes a Message to JSON and writes it followed by a newline '\n'
func (s *TransportStream) WriteMessage(msg *Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	data = append(data, '\n')
	_, err = s.writer.Write(data)
	return err
}

// NewResponse creates a response message matching a request's ID
func NewResponse(requestID string, msgType string, payload any) (*Message, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}

		raw = b
	}

	return &Message{
		ID:      requestID,
		Type:    msgType,
		Payload: raw,
	}, nil
}

func ValidateChatText(text string) error {
	if len(text) > MaxChatTextBytes {
		return errors.New("chat text exceeds 8 MiB")
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("chat text cannot be empty")
	}
	return nil
}

// NewErrorResponse creates an error response message matching a request's ID
func NewErrorResponse(requestID string, code string, message string) *Message {
	return &Message{
		ID:   requestID,
		Type: "error",
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
		},
	}
}
