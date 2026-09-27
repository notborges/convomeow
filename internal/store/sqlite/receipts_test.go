package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func TestReceiptsBeforeMessagesReplayAliasesAndParticipants(t *testing.T) {
	ctx := context.Background()
	store, path := historyTestStore(t)
	at := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	receipt := core.Receipt{ChatID: "group@g.us", ParticipantID: "a@lid", MessageIDs: []string{"m1"}, Kind: "read", At: at, Group: true}
	if changed, err := store.SaveReceipt(ctx, "account-1", receipt); err != nil || len(changed) != 0 {
		t.Fatal(changed, err)
	}
	messages, err := store.ListMessages(ctx, "account-1", nil, 10)
	if err != nil || len(messages) != 0 {
		t.Fatal("receipt created message", err)
	}
	message, err := store.SaveMessage(ctx, core.Message{AccountID: "account-1", ChatID: receipt.ChatID, ProviderMessageID: "m1", Direction: "outbound"})
	if err != nil {
		t.Fatal(err)
	}
	if message.Delivery == nil || message.Delivery.State != "partial_read" || message.Delivery.ReadCount != 1 || message.Delivery.DeliveredCount != 1 {
		t.Fatalf("read-first summary: %+v", message.Delivery)
	}
	if changed, err := store.SaveReceipt(ctx, "account-1", receipt); err != nil || len(changed) != 0 {
		t.Fatal("duplicate published change", changed, err)
	}
	receipt.ParticipantID = "b@phone"
	receipt.Kind = "delivered"
	receipt.At = at.Add(-time.Second)
	if _, err := store.SaveReceipt(ctx, "account-1", receipt); err != nil {
		t.Fatal(err)
	}
	if err := store.LinkChats(ctx, "account-1", core.ChatLink{First: "a@lid", Second: "b@phone"}); err != nil {
		t.Fatal(err)
	}
	receipt.ParticipantID = "other@phone"
	receipt.Kind = "read"
	receipt.At = at.Add(time.Second)
	if _, err := store.SaveReceipt(ctx, "account-1", receipt); err != nil {
		t.Fatal(err)
	}
	// A read receipt for another account must not contribute to these counts.
	receipt.ParticipantID = "unrelated"
	if _, err := store.SaveReceipt(ctx, "account-2", receipt); err != nil {
		t.Fatal(err)
	}
	batch := core.HistoryBatch{AccountID: "account-1", Chat: &core.HistoryChat{ID: "alias-group", Aliases: []string{"group@g.us"}, Kind: "group"}, Messages: []core.Message{{ChatID: "alias-group", ProviderMessageID: "m1", Direction: "outbound"}}}
	if err := store.ImportHistory(ctx, batch); err != nil {
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
	message, err = store.GetMessage(ctx, message.ID)
	if err != nil {
		t.Fatal(err)
	}
	if message.Delivery.State != "partial_read" || message.Delivery.ReadCount != 2 || message.Delivery.DeliveredCount != 2 {
		t.Fatalf("alias replay summary: %+v", message.Delivery)
	}
	items, err := store.ListMessageReceipts(ctx, message.ID, "", 1)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	if items[0].ReadAt == nil || items[0].DeliveredAt == nil || !items[0].ReadAt.Equal(at) {
		t.Fatalf("merged timestamps: %+v", items[0])
	}
	rest, err := store.ListMessageReceipts(ctx, message.ID, items[0].ParticipantID, 10)
	if err != nil || len(rest) != 1 || rest[0].ParticipantID != "other@phone" {
		t.Fatal(rest, err)
	}
}

func TestDirectReadDoesNotRegressOrChangeSendState(t *testing.T) {
	ctx := context.Background()
	store, _ := historyTestStore(t)
	defer store.Close()
	m, err := store.SaveMessage(ctx, core.Message{AccountID: "account-1", ChatID: "direct", ProviderMessageID: "m", Direction: "outbound", State: "outcome_unknown"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	r := core.Receipt{ChatID: "direct", ParticipantID: "recipient", MessageIDs: []string{"m"}, Kind: "read", At: at}
	if _, err := store.SaveReceipt(ctx, "account-1", r); err != nil {
		t.Fatal(err)
	}
	r.Kind = "delivered"
	r.At = at.Add(time.Minute)
	if _, err := store.SaveReceipt(ctx, "account-1", r); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetMessage(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "outcome_unknown" || got.Delivery.State != "read" {
		t.Fatalf("state regression: %+v", got)
	}
}
