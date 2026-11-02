package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateChatText(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantErr string
	}{
		{name: "multiline script", text: "echo first\nWrite-Host 'second'"},
		{name: "empty", text: " \t\r\n", wantErr: "chat text cannot be empty"},
		{name: "oversized", text: strings.Repeat("x", MaxChatTextBytes+1), wantErr: "chat text exceeds 8 MiB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateChatText(tt.text)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateChatText returned error: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("ValidateChatText error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestTransportStreamRoundtrip(t *testing.T) {
	buf := new(bytes.Buffer)
	stream := NewTransportStream(buf)

	reqPayload := map[string]string{"file": "test.cs"}
	reqBytes, _ := json.Marshal(reqPayload)

	msg := &Message{
		ID:      "req-1",
		Type:    TypeFileRead,
		Payload: reqBytes,
	}

	if err := stream.WriteMessage(msg); err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	// Verify newline framing
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		t.Errorf("Stream output missing newline delimiter")
	}

	readMsg, err := stream.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}

	if readMsg.ID != "req-1" {
		t.Errorf("Expected ID 'req-1', got '%s'", readMsg.ID)
	}
	if readMsg.Type != TypeFileRead {
		t.Errorf("Expected Type '%s', got '%s'", TypeFileRead, readMsg.Type)
	}

	var parsedPayload map[string]string
	if err := json.Unmarshal(readMsg.Payload, &parsedPayload); err != nil {
		t.Fatalf("Failed to parse payload: %v", err)
	}
	if parsedPayload["file"] != "test.cs" {
		t.Errorf("Expected payload file 'test.cs', got '%s'", parsedPayload["file"])
	}
}

func TestErrorResponse(t *testing.T) {
	errResp := NewErrorResponse("req-99", "ERR_NOT_FOUND", "File not found")
	if errResp.ID != "req-99" {
		t.Errorf("Expected ID req-99, got %s", errResp.ID)
	}
	if errResp.Error == nil || errResp.Error.Code != "ERR_NOT_FOUND" {
		t.Errorf("Expected error code ERR_NOT_FOUND")
	}
}
