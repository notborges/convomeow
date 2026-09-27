package native_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/api/native"
	"github.com/notborges/convomeow/internal/app"
	"github.com/notborges/convomeow/internal/core"
)

func TestReadReceiptsValidationPartialFailureAndReplay(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.sqlite")
	account := uuid.NewString()
	createAvatarTestAccount(t, path, account)
	connector := &fakeConnector{readCalls: make(chan []string, 10), failReadSender: "b"}
	service, store := avatarTestService(t, path, filepath.Join(root, "media"), connector)
	defer service.Close()
	ctx := context.Background()
	save := func(chat, sender, id, direction string) core.Message {
		t.Helper()
		m, err := store.SaveMessage(ctx, core.Message{AccountID: account, ChatID: chat, SenderID: sender, ProviderMessageID: id, Direction: direction, State: "sent", OccurredAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	a := save("group", "a", "one", "inbound")
	b := save("group", "b", "two", "inbound")
	other := save("other", "a", "three", "inbound")
	outbound := save("group", "self", "four", "outbound")
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	url := server.URL + "/api/v1/conversations/" + a.ConversationID + "/read-receipts"
	for _, ids := range [][]string{nil, {a.ID, other.ID}, {a.ID, outbound.ID}} {
		res := request(t, server.Client(), "POST", url, map[string]any{"message_ids": ids}, "")
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatalf("invalid batch: %d", res.StatusCode)
		}
	}
	if len(connector.readCalls) != 0 {
		t.Fatal("invalid batch sent receipts")
	}
	decode[core.Message](t, request(t, server.Client(), "GET", server.URL+"/api/v1/messages/"+a.ID, nil, ""), 200)
	if len(connector.readCalls) != 0 {
		t.Fatal("GET marked read")
	}
	result := decode[app.ReadReceiptResult](t, request(t, server.Client(), "POST", url, map[string]any{"message_ids": []string{a.ID, a.ID, b.ID}}, ""), 200)
	if len(result.ReadIDs) != 1 || result.ReadIDs[0] != a.ID || len(result.Failed) != 1 || result.Failed[0].MessageIDs[0] != b.ID {
		t.Fatalf("partial result: %+v", result)
	}
	if len(connector.readCalls) != 2 {
		t.Fatal("expected sender batches")
	}
	m, err := store.GetMessage(ctx, a.ID)
	if err != nil || m.ReadAt == nil {
		t.Fatalf("successful read not saved: %v", err)
	}
	m, err = store.GetMessage(ctx, b.ID)
	if err != nil || m.ReadAt != nil {
		t.Fatalf("failed read saved: %v", err)
	}
	decode[app.ReadReceiptResult](t, request(t, server.Client(), "POST", url, map[string]any{"message_ids": []string{a.ID}}, ""), 200)
	if len(connector.readCalls) != 2 {
		t.Fatal("replay sent again")
	}

	for _, participant := range []string{"a", "b"} {
		connector.emitEvent(core.Event{Type: core.EventReceipt, Receipt: &core.Receipt{ChatID: "group", ParticipantID: participant, MessageIDs: []string{"four"}, Kind: "read", At: time.Now().UTC(), Group: true}})
	}
	m = decode[core.Message](t, request(t, server.Client(), "GET", server.URL+"/api/v1/messages/"+outbound.ID, nil, ""), 200)
	if m.Delivery == nil || m.Delivery.State != "partial_read" || m.Delivery.ReadCount != 2 {
		t.Fatalf("delivery: %+v", m.Delivery)
	}
	type page struct {
		Items []core.MessageReceipt `json:"items"`
		Next  string                `json:"next_cursor"`
	}
	receiptURL := server.URL + "/api/v1/messages/" + outbound.ID + "/receipts?limit=1"
	first := decode[page](t, request(t, server.Client(), "GET", receiptURL, nil, ""), 200)
	if len(first.Items) != 1 || first.Next == "" {
		t.Fatal("missing receipt page")
	}
	second := decode[page](t, request(t, server.Client(), "GET", receiptURL+"&cursor="+first.Next, nil, ""), 200)
	if len(second.Items) != 1 || second.Items[0].ParticipantID == first.Items[0].ParticipantID || second.Next != "" {
		t.Fatal("receipt pagination")
	}
}
