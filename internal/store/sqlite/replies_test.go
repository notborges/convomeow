package sqlite

import (
	"context"
	"github.com/notborges/convomeow/internal/core"
	"testing"
)

func TestRepliesResolveAfterHistoryAndAliasMerge(t *testing.T) {
	ctx := context.Background()
	store, path := historyTestStore(t)
	save := func(m core.Message) core.Message {
		t.Helper()
		saved, err := store.SaveMessage(ctx, m)
		if err != nil {
			t.Fatal(err)
		}
		return saved
	}
	quote := &core.Reply{ProviderMessageID: "original", SenderID: "sender", Kind: core.MessageKindText, Text: "quoted preview"}
	reply := save(core.Message{AccountID: "account-1", ChatID: "one@lid", ProviderMessageID: "reply", Direction: "inbound", Reply: quote})
	if reply.Reply == nil || reply.Reply.MessageID != "" {
		t.Fatalf("missing-original quote: %+v", reply.Reply)
	}
	save(core.Message{AccountID: "account-2", ChatID: "one@lid", ProviderMessageID: "original", Direction: "inbound"})
	save(core.Message{AccountID: "account-1", ChatID: "other@lid", ProviderMessageID: "original", Direction: "inbound"})
	unresolved, err := store.GetMessage(ctx, reply.ID)
	if err != nil || unresolved.Reply.MessageID != "" {
		t.Fatal("resolved across account or conversation", err)
	}
	original := save(core.Message{AccountID: "account-1", ChatID: "123@s.whatsapp.net", ProviderMessageID: "original", Direction: "inbound"})
	// Duplicate without context must not erase the saved quote during the merge.
	save(core.Message{AccountID: "account-1", ChatID: "123@s.whatsapp.net", ProviderMessageID: "reply", Direction: "inbound"})
	if err := store.LinkChats(ctx, "account-1", core.ChatLink{First: "one@lid", Second: "123@s.whatsapp.net"}); err != nil {
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
	messages, err := store.ListMessages(ctx, "account-1", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range messages {
		if m.ProviderMessageID == "reply" {
			if m.Reply == nil || m.Reply.MessageID != original.ID || m.Reply.Text != "quoted preview" {
				t.Fatalf("resolved quote after reopen: %+v", m.Reply)
			}
			return
		}
	}
	t.Fatal("reply missing")
}
