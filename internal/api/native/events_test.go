package native_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/notborges/convomeow/internal/api/native/v1"
	"github.com/notborges/convomeow/internal/app"
	"github.com/notborges/convomeow/internal/core"
	"github.com/notborges/convomeow/internal/store/sqlite"
)

func TestEventStreamAuthenticationAndCommittedChanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	connector := &fakeConnector{}
	service := app.New(repo, connector, nil)
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	var browserAllowed atomic.Bool
	server := httptest.NewServer(v1.New(service, "test-token", func(*http.Request) bool { return browserAllowed.Load() }))
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/events"
	for _, headers := range []http.Header{{}, {"Authorization": {"Bearer test-token"}, "Origin": {"https://other.example"}}} {
		conn, res, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: headers})
		if conn != nil {
			conn.CloseNow()
			t.Fatal("unauthorized upgrade succeeded")
		}
		if err == nil || res == nil || (res.StatusCode != 401 && res.StatusCode != 403) {
			t.Fatalf("upgrade response: %v, %v", res, err)
		}
	}
	browserAllowed.Store(true)
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	read := func() app.Notification {
		t.Helper()
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var change app.Notification
		if err := json.Unmarshal(data, &change); err != nil {
			t.Fatal(err)
		}
		return change
	}
	if read().Type != "ready" {
		t.Fatal("missing ready frame")
	}
	account, err := service.CreateAccount(ctx, "test", core.ProviderWhatsApp, core.ConnectionKindLinkedDevice)
	if err != nil {
		t.Fatal(err)
	}
	change := read()
	if change.Type != app.AccountsChanged || change.AccountID != account.ID {
		t.Fatalf("account event: %+v", change)
	}
	if saved, err := repo.ListAccounts(ctx); err != nil || len(saved) != 1 {
		t.Fatal("notification preceded commit")
	}
	if _, err := service.StartLogin(account.ID); err != nil {
		t.Fatal(err)
	}
	for {
		change = read()
		status, _ := service.Account(account.ID)
		if status.State == "connected" && change.Type == app.AccountsChanged {
			break
		}
	}
	connector.emitEvent(core.Event{Type: core.EventMessage, Message: &core.Message{ProviderMessageID: "incoming", ChatID: "15551112222@s.whatsapp.net", Direction: "inbound", Kind: core.MessageKindText, Text: "fixture", OccurredAt: time.Now().UTC()}})
	for {
		change = read()
		if change.Type == app.ConversationsChanged {
			break
		}
	}
	messages, err := service.ListConversationMessages(ctx, change.ConversationID, nil, 10)
	if err != nil || len(messages) != 1 {
		t.Fatalf("message not persisted: %v", err)
	}
	connector.emitEvent(core.Event{Type: core.EventHistory, History: &core.HistoryBatch{Chat: &core.HistoryChat{ID: "15551112222@s.whatsapp.net", Kind: "direct"}, Messages: []core.Message{{ProviderMessageID: "history", ChatID: "15551112222@s.whatsapp.net", Direction: "inbound", Kind: core.MessageKindText, Text: "older", OccurredAt: time.Now().Add(-time.Hour)}}}})
	for {
		change = read()
		if change.Type == app.ConversationsChanged {
			break
		}
	}
	if change.AccountID != account.ID {
		t.Fatal("history notification lost account scope")
	}
	if _, err := service.SendText(ctx, messages[0].ConversationID, "reply", "fixture-send-1"); err != nil {
		t.Fatal(err)
	}
	for {
		change = read()
		if change.Type == app.ConversationsChanged {
			break
		}
	}
	if change.ConversationID != messages[0].ConversationID {
		t.Fatal("outgoing notification lost conversation")
	}
	browserAllowed.Store(false)
	if _, err := service.CreateAccount(ctx, "another", core.ProviderWhatsApp, core.ConnectionKindLinkedDevice); err != nil {
		t.Fatal(err)
	}
	for {
		_, _, err = conn.Read(ctx)
		if err != nil {
			break
		}
	}
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("expired session: %v", err)
	}
}

func TestEventStreamClosesOnServiceShutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	repo, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	service := app.New(repo, &fakeConnector{}, nil)
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(v1.New(service, "test-token", nil))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/events", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer test-token"}}})
	if err != nil {
		service.Close()
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if _, _, err := conn.Read(ctx); err != nil {
		service.Close()
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	_, _, err = conn.Read(ctx)
	status := websocket.CloseStatus(err)
	if status != websocket.StatusGoingAway && status != websocket.StatusTryAgainLater {
		t.Fatalf("shutdown close: %v", err)
	}
}
