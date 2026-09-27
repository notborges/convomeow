package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func TestMessageChangesSurviveHistoryAliasesAndRestart(t *testing.T) {
	ctx := context.Background()
	s, path := historyTestStore(t)
	at := time.Now().UTC().Truncate(time.Millisecond)
	change := core.MessageChange{ChatID: "123@phone", TargetID: "target", Kind: "edit", Text: "edited", At: at, EventID: "edit-1"}
	save := func(c core.MessageChange) {
		t.Helper()
		if _, err := s.SaveMessageChange(ctx, "account-1", c); err != nil {
			t.Fatal(err)
		}
	}
	save(change)
	original := core.Message{AccountID: "account-1", ChatID: change.ChatID, ProviderMessageID: change.TargetID, Direction: "inbound", Text: "original", OccurredAt: at.Add(-time.Minute), Attachments: []core.Attachment{{Kind: core.MessageKindImage, MIMEType: "image/png", Size: 10, ProviderRef: []byte("ref"), AutoFetch: true}}}
	m, err := s.SaveMessage(ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	attachmentID := m.Attachments[0].ID
	if m.Text != "edited" || m.EditedAt == nil || !m.OccurredAt.Equal(original.OccurredAt) {
		t.Fatalf("edit changed chronology or was lost: %+v", m)
	}
	stale := change
	stale.At = at.Add(-time.Second)
	stale.Text = "stale"
	save(stale)
	reply, err := s.SaveMessage(ctx, core.Message{AccountID: "account-1", ChatID: change.ChatID, ProviderMessageID: "reply", Direction: "inbound", Text: "reply", Reply: &core.Reply{ProviderMessageID: change.TargetID, Text: "original", Kind: core.MessageKindText}})
	if err != nil {
		t.Fatal(err)
	}
	change.ChatID = "456@lid"
	change.Kind = "revoke"
	change.Text = ""
	change.EventID = "delete"
	save(change)
	if err := s.LinkChats(ctx, "account-1", core.ChatLink{First: "123@phone", Second: "456@lid"}); err != nil {
		t.Fatal(err)
	}
	stale.At = at.Add(-time.Hour)
	save(stale)
	if _, err := s.SaveMessage(ctx, original); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		m, err := s.GetMessage(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if m.DeletedAt == nil || m.Text != "edited" || len(m.Attachments) != 1 {
			t.Fatalf("deleted content resurfaced: %+v", m)
		}
		if _, err := s.GetMedia(ctx, attachmentID); err != nil {
			t.Fatal("saved media unavailable", err)
		}
		quoted, err := s.GetMessage(ctx, reply.ID)
		if err != nil || quoted.Reply == nil || !quoted.Reply.Deleted || quoted.Reply.Text != "original" {
			t.Fatal("saved quote lost", quoted, err)
		}
	}
	check()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	check()
	original.AccountID = "account-2"
	other, err := s.SaveMessage(ctx, original)
	if err != nil || other.DeletedAt != nil || other.Text != "original" {
		t.Fatal("change crossed account", other, err)
	}
}
