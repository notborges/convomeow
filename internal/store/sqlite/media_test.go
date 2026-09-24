package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func TestStaleMissingObjectCannotClearNewMedia(t *testing.T) {
	ctx := context.Background()
	store, _ := historyTestStore(t)
	defer store.Close()
	message, err := store.SaveMessage(ctx, core.Message{AccountID: "account-1", ChatID: "15551112222@s.whatsapp.net",
		ProviderMessageID: "media-state", Direction: "inbound", Kind: core.MessageKindImage, OccurredAt: time.Now().UTC(),
		Attachments: []core.Attachment{{Kind: core.MessageKindImage, ProviderRef: []byte("private")}}})
	if err != nil {
		t.Fatal(err)
	}
	id := message.Attachments[0].ID
	if err := store.MarkMediaReady(ctx, id, "local", "object", 5, make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	first, err := store.GetMedia(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkMediaRemote(ctx, id, first.Version); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkMediaReady(ctx, id, "local", "object", 5, make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkMediaRemote(ctx, id, first.Version); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("stale missing-object report: %v", err)
	}
	current, err := store.GetMedia(ctx, id)
	if err != nil || current.Availability != "ready" || current.Version <= first.Version {
		t.Fatalf("current media state: %+v, %v", current, err)
	}
}
