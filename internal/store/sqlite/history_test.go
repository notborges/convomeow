package sqlite

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func historyTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.sqlite")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"account-1", "account-2"} {
		if err := store.CreateAccount(context.Background(), core.Account{ID: id, Provider: core.ProviderWhatsApp,
			ConnectionKind: core.ConnectionKindLinkedDevice, Label: id, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	return store, path
}

func TestHistoryReplayOrderingAliasesAndMedia(t *testing.T) {
	ctx := context.Background()
	store, path := historyTestStore(t)
	pn, lid := "15551234567@s.whatsapp.net", "abc123@lid"
	base := time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC)
	live, err := store.SaveMessage(ctx, core.Message{AccountID: "account-1", ChatID: lid, ProviderMessageID: "new",
		Direction: "inbound", Kind: core.MessageKindText, Text: "latest", OccurredAt: base.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	ref := []byte(`{"direct_path":"/media","media_key":"secret-key"}`)
	batch := core.HistoryBatch{AccountID: "account-1", Chat: &core.HistoryChat{ID: pn, Aliases: []string{lid}}, Messages: []core.Message{
		{ChatID: pn, ProviderMessageID: "old", Direction: "inbound", Kind: core.MessageKindText, Text: "oldest", OccurredAt: base},
		{ChatID: pn, ProviderMessageID: "photo", Direction: "inbound", Kind: core.MessageKindImage, Text: "caption", OccurredAt: base.Add(time.Hour),
			Attachments: []core.Attachment{{Kind: core.MessageKindImage, MIMEType: "image/jpeg", Size: 13, ProviderRef: ref}}},
		{ChatID: pn, ProviderMessageID: "new", Direction: "inbound", Kind: core.MessageKindText, Text: "latest", OccurredAt: base.Add(2 * time.Hour)},
	}}
	for range 2 {
		if err := store.ImportHistory(ctx, batch); err != nil {
			t.Fatal(err)
		}
	}
	check := func() {
		messages, err := store.ListMessages(ctx, "account-1", nil, 10)
		if err != nil || len(messages) != 3 {
			t.Fatalf("messages: %+v, %v", messages, err)
		}
		if messages[0].ID != live.ID || messages[1].ProviderMessageID != "photo" || messages[2].ProviderMessageID != "old" {
			t.Fatalf("history order or identity: %+v", messages)
		}
		if len(messages[1].Attachments) != 1 || messages[1].Attachments[0].MIMEType != "image/jpeg" || len(messages[1].Attachments[0].ProviderRef) != 0 {
			t.Fatalf("attachment metadata: %+v", messages[1].Attachments)
		}
		var storedRef []byte
		err = store.db.QueryRowContext(ctx, `SELECT provider_ref FROM attachments WHERE id = ?`, messages[1].Attachments[0].ID).Scan(&storedRef)
		if err != nil || !bytes.Equal(storedRef, ref) {
			t.Fatalf("private media ref: %q, %v", storedRef, err)
		}
		page, err := store.ListMessages(ctx, "account-1", &core.PageCursor{Time: messages[0].OccurredAt, ID: messages[0].ID}, 1)
		if err != nil || len(page) != 1 || page[0].ID != messages[1].ID {
			t.Fatalf("history cursor: %+v, %v", page, err)
		}
		conversations, err := store.ListConversations(ctx, "account-1", nil, 10)
		if err != nil || len(conversations) != 1 || conversations[0].ID != live.ConversationID || conversations[0].LastMessage.ID != live.ID {
			t.Fatalf("conversation after import: %+v, %v", conversations, err)
		}
	}
	check()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ImportHistory(ctx, batch); err != nil {
		t.Fatal(err)
	}
	check()
	other, err := store.SaveMessage(ctx, core.Message{AccountID: "account-2", ChatID: pn, ProviderMessageID: "new", Direction: "inbound",
		Kind: core.MessageKindText, Text: "other account", OccurredAt: base})
	if err != nil || other.ConversationID == live.ConversationID {
		t.Fatalf("account isolation: %+v, %v", other, err)
	}
}

func TestHistoryMappingMergesExistingChatsAndEmptyConversation(t *testing.T) {
	ctx := context.Background()
	store, _ := historyTestStore(t)
	defer store.Close()
	pn, lid := "15557654321@s.whatsapp.net", "def456@lid"
	base := time.Date(2025, 5, 6, 7, 8, 9, 0, time.UTC)
	first, _, err := store.GetOrCreateConversation(ctx, "account-1", pn)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := store.GetOrCreateConversation(ctx, "account-1", lid)
	if err != nil || first.ID == second.ID {
		t.Fatalf("separate conversations: %+v %+v %v", first, second, err)
	}
	for _, chatID := range []string{pn, lid} {
		if _, err := store.SaveMessage(ctx, core.Message{AccountID: "account-1", ChatID: chatID, ProviderMessageID: "same",
			Direction: "inbound", Kind: core.MessageKindText, Text: "duplicate", OccurredAt: base}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ImportHistory(ctx, core.HistoryBatch{AccountID: "account-1", Links: []core.ChatLink{{First: pn, Second: lid}}}); err != nil {
		t.Fatal(err)
	}
	conversations, err := store.ListConversations(ctx, "account-1", nil, 10)
	if err != nil || len(conversations) != 1 {
		t.Fatalf("merged conversations: %+v, %v", conversations, err)
	}
	messages, err := store.ListMessages(ctx, "account-1", nil, 10)
	if err != nil || len(messages) != 1 {
		t.Fatalf("merged messages: %+v, %v", messages, err)
	}
	if _, created, err := store.GetOrCreateConversation(ctx, "account-1", lid); err != nil || created {
		t.Fatalf("linked JID created duplicate: %t %v", created, err)
	}
	empty := core.HistoryBatch{AccountID: "account-1", Chat: &core.HistoryChat{ID: "15550001111@s.whatsapp.net", LastActivityAt: base.Add(-time.Hour)}}
	if err := store.ImportHistory(ctx, empty); err != nil {
		t.Fatal(err)
	}
	conversations, err = store.ListConversations(ctx, "account-1", nil, 10)
	if err != nil || len(conversations) != 2 || !conversations[1].UpdatedAt.Equal(empty.Chat.LastActivityAt) || conversations[1].LastMessage != nil {
		t.Fatalf("empty history conversation: %+v, %v", conversations, err)
	}
}

func TestAliasMergeKeepsOutboundSendID(t *testing.T) {
	ctx := context.Background()
	store, _ := historyTestStore(t)
	defer store.Close()
	pn, lid := "15558889999@s.whatsapp.net", "outbound@lid"
	base := time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)
	if _, err := store.SaveMessage(ctx, core.Message{AccountID: "account-1", ChatID: pn, ProviderMessageID: "same",
		Direction: "inbound", Kind: core.MessageKindText, Text: "copy", OccurredAt: base}); err != nil {
		t.Fatal(err)
	}
	other, _, err := store.GetOrCreateConversation(ctx, "account-1", lid)
	if err != nil {
		t.Fatal(err)
	}
	send, created, err := store.ReserveSend(ctx, core.Message{AccountID: "account-1", ConversationID: other.ID,
		ChatID: lid, ProviderMessageID: "same", Kind: core.MessageKindText, Text: "copy", OccurredAt: base},
		"control", "request-key", "request-hash")
	if err != nil || !created {
		t.Fatalf("reserve outbound: %+v, %v", send, err)
	}
	if err := store.ImportHistory(ctx, core.HistoryBatch{AccountID: "account-1", Links: []core.ChatLink{{First: pn, Second: lid}}}); err != nil {
		t.Fatal(err)
	}
	replayed, found, err := store.LookupSend(ctx, "control", "request-key", "request-hash")
	if err != nil || !found || replayed.ID != send.ID || replayed.Direction != "outbound" {
		t.Fatalf("outbound identity changed: %+v, %t, %v", replayed, found, err)
	}
	messages, err := store.ListMessages(ctx, "account-1", nil, 10)
	if err != nil || len(messages) != 1 || messages[0].ID != send.ID {
		t.Fatalf("alias merge duplicated outbound message: %+v, %v", messages, err)
	}
}

func TestHistoryNewJIDKeepsConversationIDAndUpdatesSendTarget(t *testing.T) {
	ctx := context.Background()
	store, _ := historyTestStore(t)
	defer store.Close()
	oldJID, newJID := "old-name@g.us", "new-name@g.us"
	before, _, err := store.GetOrCreateConversation(ctx, "account-1", oldJID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ImportHistory(ctx, core.HistoryBatch{AccountID: "account-1", Chat: &core.HistoryChat{
		ID: oldJID, PreferredID: newJID, LastActivityAt: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetConversation(ctx, before.ID)
	if err != nil || after.ProviderChatID != newJID {
		t.Fatalf("new send target: %+v, %v", after, err)
	}
	linked, created, err := store.GetOrCreateConversation(ctx, "account-1", newJID)
	if err != nil || created || linked.ID != before.ID {
		t.Fatalf("new JID created another chat: %+v, %t, %v", linked, created, err)
	}
}
