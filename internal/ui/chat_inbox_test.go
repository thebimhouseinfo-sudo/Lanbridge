package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatInboxCursorSkipsAgentMessages(t *testing.T) {
	ws := &WebServer{token: "tok"}
	first := ws.AddChatMessage(ChatMessage{From: "Windows", Text: "Đã mở project", SenderKind: ""})
	agent := ws.AddChatMessage(ChatMessage{Text: "agent: chạy build", SenderKind: ChatSenderMacAgent})
	reply := ws.AddChatMessage(ChatMessage{From: "Windows", Text: "build xong"})

	if first.ID == "" || first.Seq == 0 || first.ID == reply.ID {
		t.Fatalf("messages need unique IDs and sequence numbers: %+v %+v", first, reply)
	}
	if agent.Seq != first.Seq+1 || reply.Seq != agent.Seq+1 {
		t.Fatalf("sequence must be monotonic: %d %d %d", first.Seq, agent.Seq, reply.Seq)
	}

	inbox, cursor := ws.ChatInbox(0, 20)
	if len(inbox) != 2 || inbox[0].Text != "Đã mở project" || inbox[1].Text != "build xong" {
		t.Fatalf("inbox from 0 = %+v", inbox)
	}
	if cursor != reply.Seq {
		t.Fatalf("cursor = %d, want %d", cursor, reply.Seq)
	}

	again, cursorAgain := ws.ChatInbox(cursor, 20)
	if len(again) != 0 || cursorAgain != cursor {
		t.Fatalf("reading at the cursor must return nothing new, got %+v cursor %d", again, cursorAgain)
	}

	next := ws.AddChatMessage(ChatMessage{From: "Windows", Text: "tiếp theo"})
	later, cursorLater := ws.ChatInbox(cursor, 20)
	if len(later) != 1 || later[0].ID != next.ID || cursorLater != next.Seq {
		t.Fatalf("new message after cursor = %+v cursor %d", later, cursorLater)
	}
}

func TestChatInboxLimitAdvancesOnlyOverReturnedMessages(t *testing.T) {
	ws := &WebServer{token: "tok"}
	for i := 0; i < 5; i++ {
		ws.AddChatMessage(ChatMessage{From: "Windows", Text: "m"})
	}
	page, cursor := ws.ChatInbox(0, 2)
	if len(page) != 2 || cursor != 2 {
		t.Fatalf("page=%d cursor=%d, want 2 and 2", len(page), cursor)
	}
	rest, _ := ws.ChatInbox(cursor, 20)
	if len(rest) != 3 {
		t.Fatalf("remaining messages = %d, want 3", len(rest))
	}
}

func TestMCPChatReadEndpointValidatesAndReturnsCursor(t *testing.T) {
	ws := &WebServer{token: "tok"}
	ws.AddChatMessage(ChatMessage{From: "Windows", Text: "hello"})

	get := func(query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/mcp/chat/read?t=tok&"+query, nil)
		rec := httptest.NewRecorder()
		ws.handleMCPChatRead(rec, req)
		return rec
	}
	rec := get("after=0&limit=20")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Messages   []ChatMessage `json:"messages"`
		NextCursor int64         `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) != 1 || body.NextCursor != 1 {
		t.Fatalf("body = %+v", body)
	}
	if get("after=-1").Code != http.StatusBadRequest || get("limit=0").Code != http.StatusBadRequest {
		t.Fatal("invalid cursor or limit must be rejected")
	}
	if get("limit=101").Code != http.StatusBadRequest {
		t.Fatal("limit above 100 must be rejected")
	}

	empty := httptest.NewRecorder()
	ws.handleMCPChatRead(empty, httptest.NewRequest(http.MethodGet, "/api/mcp/chat/read?t=tok&after=1", nil))
	if !strings.Contains(empty.Body.String(), `"messages":[]`) {
		t.Fatalf("empty inbox must return [] not null: %s", empty.Body.String())
	}

	wrong := httptest.NewRecorder()
	ws.handleMCPChatRead(wrong, httptest.NewRequest(http.MethodGet, "/api/mcp/chat/read?t=bad", nil))
	if wrong.Code != http.StatusForbidden {
		t.Fatalf("bad token status = %d", wrong.Code)
	}
}
