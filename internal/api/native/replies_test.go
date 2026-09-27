package native_test

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/api/native"
	"github.com/notborges/convomeow/internal/core"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestReplySendValidationIdempotencyAndMessageWindow(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.sqlite")
	account := uuid.NewString()
	createAvatarTestAccount(t, path, account)
	connector := &fakeConnector{preparedMessages: make(chan core.PreparedMessage, 4)}
	service, store := avatarTestService(t, path, filepath.Join(root, "media"), connector)
	defer service.Close()
	ctx := context.Background()
	var saved []core.Message
	now := time.Now().UTC()
	for i := 0; i < 9; i++ {
		m, err := store.SaveMessage(ctx, core.Message{AccountID: account, ChatID: "123@s.whatsapp.net", ProviderMessageID: fmt.Sprint(i), Direction: "inbound", Text: fmt.Sprintf("message %d", i), SenderID: "123@s.whatsapp.net", OccurredAt: now.Add(time.Duration(i) * time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		saved = append(saved, m)
	}
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	url := server.URL + "/api/v1/conversations/" + saved[0].ConversationID + "/messages"
	body := map[string]any{"kind": "text", "content": map[string]string{"text": "answer"}, "reply_to_message_id": saved[0].ID}
	sent := decode[core.Message](t, request(t, server.Client(), "POST", url, body, "reply-send-key"), http.StatusCreated)
	if sent.Reply == nil || sent.Reply.MessageID != saved[0].ID || sent.Reply.Text != "message 0" || sent.Reply.ProviderMessageID != "" {
		t.Fatalf("reply response: %+v", sent.Reply)
	}
	select {
	case prepared := <-connector.preparedMessages:
		if prepared.Reply == nil || prepared.Reply.ProviderMessageID != saved[0].ProviderMessageID {
			t.Fatal("provider reply missing")
		}
	default:
		t.Fatal("send was not forwarded")
	}
	repeated := decode[core.Message](t, request(t, server.Client(), "POST", url, body, "reply-send-key"), http.StatusCreated)
	if repeated.ID != sent.ID {
		t.Fatal("idempotency lost")
	}
	body["reply_to_message_id"] = saved[1].ID
	response := request(t, server.Client(), "POST", url, body, "reply-send-key")
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("changed target status: %d", response.StatusCode)
	}
	other, _, err := store.GetOrCreateConversation(ctx, account, "456@s.whatsapp.net")
	if err != nil {
		t.Fatal(err)
	}
	response = request(t, server.Client(), "POST", server.URL+"/api/v1/conversations/"+other.ID+"/messages", body, "other-reply-key")
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("cross-chat status: %d", response.StatusCode)
	}

	upload := uploadTestFile(t, server, account, testPNG(t), "reply.png", "image/png", http.StatusCreated)
	media := map[string]any{"kind": "image", "content": map[string]string{"upload_id": upload.ID, "caption": "photo reply"}, "reply_to_message_id": saved[0].ID}
	queued := decode[core.Message](t, request(t, server.Client(), "POST", url, media, "reply-media-key"), http.StatusAccepted)
	if queued.Reply == nil || queued.Reply.MessageID != saved[0].ID {
		t.Fatal("queued media quote missing")
	}
	select {
	case prepared := <-connector.preparedMessages:
		if prepared.Reply == nil || prepared.Reply.ProviderMessageID != saved[0].ProviderMessageID {
			t.Fatal("media provider reply missing")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("media send did not complete")
	}
	type page struct {
		Items    []core.Message `json:"items"`
		Next     string         `json:"next_cursor"`
		Previous string         `json:"previous_cursor"`
	}
	around := decode[page](t, request(t, server.Client(), "GET", url+"?limit=3&around_message_id="+saved[4].ID, nil, ""), 200)
	if len(around.Items) != 3 || around.Items[0].ID != saved[5].ID || around.Items[1].ID != saved[4].ID || around.Items[2].ID != saved[3].ID || around.Next == "" || around.Previous == "" {
		t.Fatalf("window: %+v", around)
	}
	newer := decode[page](t, request(t, server.Client(), "GET", url+"?limit=2&after_cursor="+around.Previous, nil, ""), 200)
	if len(newer.Items) != 2 || newer.Items[0].ID != saved[7].ID || newer.Items[1].ID != saved[6].ID || newer.Previous == "" || newer.Next == "" {
		t.Fatalf("newer: %+v", newer)
	}
	older := decode[page](t, request(t, server.Client(), "GET", url+"?limit=2&cursor="+around.Next, nil, ""), 200)
	if len(older.Items) != 2 || older.Items[0].ID != saved[2].ID || older.Items[1].ID != saved[1].ID || older.Previous == "" {
		t.Fatalf("older: %+v", older)
	}
	back := decode[page](t, request(t, server.Client(), "GET", url+"?limit=2&after_cursor="+older.Previous, nil, ""), 200)
	if len(back.Items) != 2 || back.Items[0].ID != saved[4].ID || back.Items[1].ID != saved[3].ID {
		t.Fatalf("reverse older page: %+v", back)
	}
	reverse := decode[page](t, request(t, server.Client(), "GET", url+"?limit=2&cursor="+newer.Next, nil, ""), 200)
	if len(reverse.Items) != 2 || reverse.Items[0].ID != saved[5].ID || reverse.Items[1].ID != saved[4].ID {
		t.Fatalf("reverse newer page: %+v", reverse)
	}
}
