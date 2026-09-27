package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func TestReactionsOrderingAliasesAndRestart(t *testing.T) {
	ctx := context.Background()
	s, path := historyTestStore(t)
	at := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	r := core.Reaction{ChatID: "123@phone", TargetID: "m1", ParticipantID: "actor@lid", Emoji: "👍", At: at, EventID: "r1"}
	save := func(account string, r core.Reaction) {
		t.Helper()
		if _, err := s.SaveReaction(ctx, account, r); err != nil {
			t.Fatal(err)
		}
	}
	save("account-1", r)
	chats, err := s.ListConversations(ctx, "account-1", nil, 10)
	if err != nil || len(chats) != 0 {
		t.Fatal("reaction created chat", err)
	}
	m, err := s.SaveMessage(ctx, core.Message{AccountID: "account-1", ChatID: r.ChatID, ProviderMessageID: "m1", Direction: "inbound"})
	if err != nil {
		t.Fatal(err)
	}
	check := func(emoji string, count int) {
		t.Helper()
		msg, err := s.GetMessage(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			if len(msg.Reactions) != 0 {
				t.Fatalf("expected removal: %+v", msg.Reactions)
			}
			return
		}
		if len(msg.Reactions) != 1 || msg.Reactions[0].Emoji != emoji || msg.Reactions[0].Count != count {
			t.Fatalf("summary: %+v", msg.Reactions)
		}
	}
	check("👍", 1)
	if ids, err := s.SaveReaction(ctx, "account-1", r); err != nil || len(ids) != 0 {
		t.Fatal("duplicate invalidated", ids, err)
	}
	save("account-2", r)
	check("👍", 1)
	r.ParticipantID = "actor@phone"
	r.Emoji = "❤️"
	r.At = at.Add(time.Second)
	r.EventID = "r2"
	save("account-1", r)
	if err := s.LinkChats(ctx, "account-1", core.ChatLink{First: "actor@lid", Second: "actor@phone"}); err != nil {
		t.Fatal(err)
	}
	check("❤️", 1)
	r.ChatID = "456@lid"
	r.Emoji = ""
	r.At = at.Add(2 * time.Second)
	r.EventID = "r3"
	save("account-1", r)
	if err := s.ImportHistory(ctx, core.HistoryBatch{AccountID: "account-1", Chat: &core.HistoryChat{ID: "123@phone", Aliases: []string{"456@lid"}}}); err != nil {
		t.Fatal(err)
	}
	check("", 0)
	r.ChatID = "123@phone"
	r.Emoji = "👍"
	r.At = at
	r.EventID = "r1"
	save("account-1", r)
	check("", 0)
	// Equal-time replacement cannot resurrect a removal, regardless of event ID.
	r.At = at.Add(2 * time.Second)
	r.EventID = "zz"
	save("account-1", r)
	check("", 0)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	check("", 0)
	r.At = at.Add(3 * time.Second)
	r.EventID = "r4"
	r.Emoji = "🔥"
	save("account-1", r)
	r.IsOwn = true
	r.ParticipantID = "self@phone"
	r.EventID = "r5"
	save("account-1", r)
	check("🔥", 2)
	r.ParticipantID = "self@lid"
	r.EventID = "r6"
	save("account-1", r)
	check("🔥", 2)
	items, err := s.ListMessageReactions(ctx, m.ID, "", 1)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	rest, err := s.ListMessageReactions(ctx, m.ID, items[0].ParticipantID, 10)
	if err != nil || len(rest) != 1 || !rest[0].IsOwn {
		t.Fatal(rest, err)
	}
}
