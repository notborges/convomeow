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

func TestAttachmentDimensionsSurviveReplay(t *testing.T) {
	ctx := context.Background()
	store, _ := historyTestStore(t)
	defer store.Close()
	input := core.Message{AccountID: "account-1", ChatID: "15551112222@s.whatsapp.net",
		ProviderMessageID: "dimensions", Direction: "inbound", Kind: core.MessageKindImage, OccurredAt: time.Now().UTC(),
		Attachments: []core.Attachment{{Kind: core.MessageKindImage, Width: 600, Height: 900}}}
	message, err := store.SaveMessage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Attachments[0].Width, input.Attachments[0].Height = 0, 0
	if _, err := store.SaveMessage(ctx, input); err != nil {
		t.Fatal(err)
	}
	saved, err := store.GetMessage(ctx, message.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Attachments[0].Width != 600 || saved.Attachments[0].Height != 900 {
		t.Fatal("replay lost dimensions")
	}
	record, err := store.GetMedia(ctx, saved.Attachments[0].ID)
	if err != nil || record.Width != 600 || record.Height != 900 {
		t.Fatalf("metadata dimensions: %d x %d, %v", record.Width, record.Height, err)
	}
	if err := store.SetMediaDimensions(ctx, record.AttachmentID, 1200, 300); err != nil {
		t.Fatal(err)
	}
	saved, err = store.GetMessage(ctx, message.ID)
	if err != nil || saved.Attachments[0].Width != 1200 || saved.Attachments[0].Height != 300 {
		t.Fatal("decoded dimensions were not persisted")
	}
}

func TestAttachmentDurationSurvivesReplay(t *testing.T) {
	ctx := context.Background()
	store, _ := historyTestStore(t)
	defer store.Close()
	input := core.Message{AccountID: "account-1", ChatID: "15551112222@s.whatsapp.net", ProviderMessageID: "audio-duration",
		Direction: "inbound", Kind: core.MessageKindAudio, OccurredAt: time.Now().UTC(),
		Attachments: []core.Attachment{{Kind: core.MessageKindAudio, DurationSeconds: 37}}}
	message, err := store.SaveMessage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Attachments[0].DurationSeconds = 0
	if _, err := store.SaveMessage(ctx, input); err != nil {
		t.Fatal(err)
	}
	saved, err := store.GetMessage(ctx, message.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Attachments[0].DurationSeconds != 37 {
		t.Fatal("replay lost audio duration")
	}
	record, err := store.GetMedia(ctx, saved.Attachments[0].ID)
	if err != nil || record.DurationSeconds != 37 {
		t.Fatalf("metadata duration: %d, %v", record.DurationSeconds, err)
	}
}
