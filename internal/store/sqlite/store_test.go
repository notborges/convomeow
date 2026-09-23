package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func TestSendReservationSurvivesRetryEchoAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "app.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.CreateAccount(ctx, core.Account{ID: "account-1", Provider: core.ProviderWhatsApp,
		ConnectionKind: core.ConnectionKindLinkedDevice, Label: "sales", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	conversation, created, err := store.GetOrCreateConversation(ctx, "account-1", "123@s.whatsapp.net")
	if err != nil || !created {
		t.Fatalf("create conversation: %+v, %v", conversation, err)
	}
	intent := core.Message{AccountID: "account-1", ConversationID: conversation.ID, ChatID: conversation.ProviderChatID,
		ProviderMessageID: "prepared-1", Kind: core.MessageKindText, Text: "hello", IngestedAt: now}
	first, created, err := store.ReserveSend(ctx, intent, "control", "request-key-1", "request-hash-1")
	if err != nil || !created || first.State != "queued" {
		t.Fatalf("reserve send: %+v, %v", first, err)
	}
	repeated, created, err := store.ReserveSend(ctx, intent, "control", "request-key-1", "request-hash-1")
	if err != nil || created || repeated.ID != first.ID {
		t.Fatalf("repeat send: %+v, %v", repeated, err)
	}
	if _, _, err := store.ReserveSend(ctx, intent, "control", "request-key-1", "other-hash"); !errors.Is(err, core.ErrIdempotency) {
		t.Fatalf("different request with same key: %v", err)
	}
	echo, err := store.SaveMessage(ctx, core.Message{AccountID: "account-1", ChatID: "different@s.whatsapp.net",
		ProviderMessageID: "prepared-1", Direction: "outbound", Kind: core.MessageKindText, Text: "hello", OccurredAt: now})
	if err != nil || echo.ID != first.ID || echo.State != "sent" {
		t.Fatalf("outbound echo: %+v, %v", echo, err)
	}
	messages, err := store.ListMessages(ctx, "account-1", nil, 10)
	if err != nil || len(messages) != 1 {
		t.Fatalf("duplicate outbound record: %v, %v", messages, err)
	}
	intent.ProviderMessageID = "prepared-2"
	queued, _, err := store.ReserveSend(ctx, intent, "control", "request-key-2", "request-hash-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	recovered, err := store.GetMessage(ctx, queued.ID)
	if err != nil || recovered.State != "outcome_unknown" {
		t.Fatalf("interrupted send: %+v, %v", recovered, err)
	}
}
