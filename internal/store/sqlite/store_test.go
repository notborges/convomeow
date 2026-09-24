package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
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

func TestMediaSendRecoveryKeepsReadyFiles(t *testing.T) {
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
	conversation, _, err := store.GetOrCreateConversation(ctx, "account-1", "123@s.whatsapp.net")
	if err != nil {
		t.Fatal(err)
	}
	var ids [2]string
	for i, phase := range []string{"uploading", "sending"} {
		uploadID := "upload-" + phase
		upload := core.Upload{ID: uploadID, AccountID: "account-1", ProfileID: "local", ObjectKey: "object-" + phase,
			MIMEType: "image/png", FileName: "photo.png", Size: 10, SHA256: []byte(strings.Repeat("x", 32)),
			ExpiresAt: now.Add(time.Hour)}
		if err := store.CreateUpload(ctx, upload); err != nil {
			t.Fatal(err)
		}
		if err := store.MarkUploadReady(ctx, uploadID); err != nil {
			t.Fatal(err)
		}
		intent := core.Message{AccountID: "account-1", ConversationID: conversation.ID, ChatID: conversation.ProviderChatID,
			ProviderMessageID: "prepared-" + phase, Kind: core.MessageKindImage, IngestedAt: now}
		message, created, err := store.ReserveMediaSend(ctx, intent, "control", "key-"+phase, "hash-"+phase, uploadID)
		if err != nil || !created || len(message.Attachments) != 1 || message.Attachments[0].ID != uploadID {
			t.Fatalf("reserve %s: %+v, %v", phase, message, err)
		}
		ids[i] = message.ID
		if _, _, err := store.ReserveMediaSend(ctx, intent, "control", "other-key-"+phase, "other-hash-"+phase, uploadID); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("claimed upload reused: %v", err)
		}
		if _, err := store.ClaimMediaSend(ctx, message.ID); err != nil {
			t.Fatal(err)
		}
		if phase == "sending" {
			if err := store.BeginMediaSend(ctx, message.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	queued, err := store.GetMessage(ctx, ids[0])
	if err != nil || queued.State != "queued" || len(queued.Attachments) != 1 || queued.Attachments[0].Availability != "ready" {
		t.Fatalf("upload phase recovery: %+v, %v", queued, err)
	}
	uncertain, err := store.GetMessage(ctx, ids[1])
	if err != nil || uncertain.State != "outcome_unknown" || len(uncertain.Attachments) != 1 || uncertain.Attachments[0].Availability != "ready" {
		t.Fatalf("sending phase recovery: %+v, %v", uncertain, err)
	}
	pending, err := store.ListPendingMediaSends(ctx, now.Add(time.Minute), 10)
	if err != nil || len(pending) != 1 || pending[0] != ids[0] {
		t.Fatalf("resumable sends: %v, %v", pending, err)
	}
	used, err := store.StoredMediaBytes(ctx)
	if err != nil || used != 20 {
		t.Fatalf("stored bytes after claims: %d, %v", used, err)
	}
	if err := store.FailMediaSendsForAccount(ctx, "account-1"); err != nil {
		t.Fatal(err)
	}
	failed, err := store.GetMessage(ctx, ids[0])
	if err != nil || failed.State != "failed" {
		t.Fatalf("queued send after logout: %+v, %v", failed, err)
	}
	pending, err = store.ListPendingMediaSends(ctx, now.Add(time.Minute), 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("send jobs after logout: %v, %v", pending, err)
	}
	upload := core.Upload{ID: "upload-echo", AccountID: "account-1", ProfileID: "local", ObjectKey: "object-echo",
		MIMEType: "image/png", FileName: "photo.png", Size: 10, SHA256: []byte(strings.Repeat("x", 32)),
		ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateUpload(ctx, upload); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkUploadReady(ctx, upload.ID); err != nil {
		t.Fatal(err)
	}
	intent := core.Message{AccountID: "account-1", ConversationID: conversation.ID, ChatID: conversation.ProviderChatID,
		ProviderMessageID: "prepared-echo", Kind: core.MessageKindImage, IngestedAt: now}
	reserved, _, err := store.ReserveMediaSend(ctx, intent, "control", "key-echo", "hash-echo", upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	echo, err := store.SaveMessage(ctx, core.Message{AccountID: "account-1", ChatID: conversation.ProviderChatID,
		ProviderMessageID: "prepared-echo", Direction: "outbound", Kind: core.MessageKindImage, OccurredAt: now,
		Attachments: []core.Attachment{{Kind: core.MessageKindImage, MIMEType: "image/png", Availability: "remote", ProviderRef: []byte("private")}}})
	if err != nil || echo.ID != reserved.ID || echo.State != "sent" || len(echo.Attachments) != 1 ||
		echo.Attachments[0].ID != upload.ID || echo.Attachments[0].Availability != "ready" {
		t.Fatalf("outbound echo: %+v, %v", echo, err)
	}
	pending, err = store.ListPendingMediaSends(ctx, now.Add(time.Minute), 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("send job after echo: %v, %v", pending, err)
	}
}
