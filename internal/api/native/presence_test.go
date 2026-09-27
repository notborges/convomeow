package native_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/api/native"
	"github.com/notborges/convomeow/internal/app"
	"github.com/notborges/convomeow/internal/core"
)

func TestPresenceStreamLifecycleAndCommands(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.sqlite")
	account := uuid.NewString()
	createAvatarTestAccount(t, path, account)
	connector := &fakeConnector{onlineCalls: make(chan bool, 10), presenceCalls: make(chan core.ChatActivity, 10)}
	service, store := avatarTestService(t, path, filepath.Join(root, "media"), connector)
	defer service.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conversation, _, err := store.GetOrCreateConversation(ctx, account, "123@s.whatsapp.net")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.LinkChats(ctx, account, core.ChatLink{First: "123@s.whatsapp.net", Second: "456@lid"}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	dial := func() *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/events?presence_account_id="+account, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer test-token"}}})
		if err != nil {
			t.Fatal(err)
		}
		_, data, err := conn.Read(ctx)
		if err != nil || !strings.Contains(string(data), "ready") {
			t.Fatalf("ready: %s %v", data, err)
		}
		return conn
	}
	one := dial()
	defer one.CloseNow()
	if online := <-connector.onlineCalls; !online {
		t.Fatal("viewer not online")
	}
	two := dial()
	defer two.CloseNow()
	if len(connector.onlineCalls) != 0 {
		t.Fatal("duplicate online update")
	}
	one.CloseNow()
	connector.emitEvent(core.Event{Type: core.EventChatPresence, Presence: &core.ChatPresence{ChatID: "456@lid", ParticipantID: "123@s.whatsapp.net", Activity: core.ActivityRecording}})
	_, data, err := two.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var notification app.Notification
	if err = json.Unmarshal(data, &notification); err != nil {
		t.Fatal(err)
	}
	if notification.Type != app.PresenceChanged || notification.ConversationID != conversation.ID || notification.Presence.Activity != core.ActivityRecording {
		t.Fatalf("presence: %+v", notification)
	}
	connector.emitEvent(core.Event{Type: core.EventChatPresence, Presence: &core.ChatPresence{ChatID: "unknown", ParticipantID: "123", Activity: core.ActivityTyping}})
	conversations, err := store.ListConversations(ctx, account, nil, 50)
	if err != nil || len(conversations) != 1 {
		t.Fatal("presence created conversation")
	}
	url := server.URL + "/api/v1/conversations/" + conversation.ID + "/presence"
	res := request(t, server.Client(), "POST", url, map[string]string{"activity": "online", "client_id": "test"}, "")
	res.Body.Close()
	if res.StatusCode != 400 || len(connector.presenceCalls) != 0 {
		t.Fatal("invalid activity forwarded")
	}
	for _, activity := range []core.ChatActivity{core.ActivityTyping, core.ActivityRecording, core.ActivityPaused} {
		res = request(t, server.Client(), "POST", url, map[string]string{"activity": string(activity), "client_id": "test"}, "")
		res.Body.Close()
		if res.StatusCode != 204 {
			t.Fatalf("command status: %d", res.StatusCode)
		}
		if got := <-connector.presenceCalls; got != activity {
			t.Fatal("wrong activity")
		}
	}
	connector.emitEvent(core.Event{Type: core.EventDisconnected})
	res = request(t, server.Client(), "POST", url, map[string]string{"activity": "typing", "client_id": "test"}, "")
	res.Body.Close()
	if res.StatusCode != 409 {
		t.Fatalf("offline command: %d", res.StatusCode)
	}
	connector.emitEvent(core.Event{Type: core.EventConnected})
	if online := <-connector.onlineCalls; !online {
		t.Fatal("online not restored on reconnect")
	}
	two.CloseNow()
	select {
	case online := <-connector.onlineCalls:
		if online {
			t.Fatal("last viewer not offline")
		}
	case <-ctx.Done():
		t.Fatal("viewer release timed out")
	}
}
