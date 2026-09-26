package native_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/api/native"
	"github.com/notborges/convomeow/internal/core"
)

func TestHistoryRequestUsesSavedAnchorAndRejectsOtherConversations(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "app.sqlite")
	accountID := uuid.NewString()
	createAvatarTestAccount(t, dbPath, accountID)
	connector := &fakeConnector{historyRequests: make(chan core.Message, 1)}
	service, store := avatarTestService(t, dbPath, filepath.Join(root, "media"), connector)
	defer service.Close()
	ctx := context.Background()
	anchor, err := store.SaveMessage(ctx, core.Message{AccountID: accountID, ChatID: "111@s.whatsapp.net", ProviderMessageID: "anchor", Direction: "outbound", OccurredAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := store.GetOrCreateConversation(ctx, accountID, "222@s.whatsapp.net")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	for _, test := range []struct {
		conversation  string
		count, status int
	}{
		{other.ID, 50, http.StatusBadRequest},
		{anchor.ConversationID, 51, http.StatusBadRequest},
		{anchor.ConversationID, 50, http.StatusAccepted},
	} {
		response := request(t, server.Client(), http.MethodPost, server.URL+"/api/v1/conversations/"+test.conversation+"/history-requests", map[string]any{"before_message_id": anchor.ID, "count": test.count}, "")
		response.Body.Close()
		if response.StatusCode != test.status {
			t.Fatalf("history request status: %d, want %d", response.StatusCode, test.status)
		}
	}
	select {
	case got := <-connector.historyRequests:
		if got.ProviderMessageID != anchor.ProviderMessageID || got.ChatID != anchor.ChatID || got.Direction != "outbound" {
			t.Fatal("wrong history anchor")
		}
	default:
		t.Fatal("history request was not forwarded")
	}
	messages, err := store.ListMessages(ctx, accountID, nil, 10)
	if err != nil || len(messages) != 1 {
		t.Fatal("history request created a chat message", err)
	}
}
