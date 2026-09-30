package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/core"
	"github.com/notborges/convomeow/internal/notifications"
)

func TestNotificationIngestionRecoveryAndSessionRevocation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "app.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	now := time.Now().UTC()
	if err := store.CreateAccount(ctx, core.Account{ID: "account", Provider: "whatsapp", ConnectionKind: "linked_device", Label: "test", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	sub := notifications.Subscription{Endpoint: "https://push.example/subscription", Locale: "en", Preview: true,
		Session: notifications.Session{ID: "session", Generation: "generation", ExpiresAt: now.Add(time.Hour)}}
	sub.Keys.P256DH, sub.Keys.Auth = "key", "auth"
	sub, err = store.SaveSubscription(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	message := core.Message{AccountID: "account", ChatID: "contact", ProviderMessageID: "live", Direction: "inbound", Text: "hello"}
	saved, err := store.SaveIncomingMessage(ctx, message, "generation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveIncomingMessage(ctx, message, "generation"); err != nil {
		t.Fatal(err)
	}
	outbound := message
	outbound.ProviderMessageID = "outbound"
	outbound.Direction = "outbound"
	if _, err := store.SaveIncomingMessage(ctx, outbound, "generation"); err != nil {
		t.Fatal(err)
	}
	history := message
	history.ProviderMessageID = "history"
	if err := store.ImportHistory(ctx, core.HistoryBatch{AccountID: "account", Chat: &core.HistoryChat{ID: message.ChatID}, Messages: []core.Message{history}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveIncomingMessage(ctx, history, "generation"); err != nil {
		t.Fatal(err)
	}
	invalid := message
	invalid.AccountID = "missing"
	invalid.ProviderMessageID = "invalid"
	if _, err := store.SaveIncomingMessage(ctx, invalid, "generation"); err == nil {
		t.Fatal("invalid transaction succeeded")
	}
	job, err := store.ClaimNotification(ctx, "generation", now.Add(time.Second))
	if err != nil || job == nil || job.MessageID != saved.ID || job.Attempts != 1 {
		t.Fatalf("claim: %+v %v", job, err)
	}
	if extra, err := store.ClaimNotification(ctx, "generation", now.Add(time.Second)); err != nil || extra != nil {
		t.Fatalf("unexpected extra notification: %+v %v", extra, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	job, err = store.ClaimNotification(ctx, "generation", now.Add(32*time.Second))
	if err != nil || job == nil || job.Attempts != 2 {
		t.Fatalf("restart recovery: %+v %v", job, err)
	}
	if err := store.FinishNotification(ctx, *job, now.Add(40*time.Second)); err != nil {
		t.Fatal(err)
	}
	if early, err := store.ClaimNotification(ctx, "generation", now.Add(35*time.Second)); err != nil || early != nil {
		t.Fatalf("retry ignored deadline: %+v %v", early, err)
	}
	if err := store.DeleteNotificationSession(ctx, sub.Session); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveSubscription(ctx, sub); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("late registration after logout: %v", err)
	}
	if pending, err := store.ClaimNotification(ctx, "generation", now.Add(time.Minute)); err != nil || pending != nil {
		t.Fatalf("logout retained job: %+v %v", pending, err)
	}
}

func TestNotificationOwnershipRenewalAndAuthorizationExpiry(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now()
	sub := notifications.Subscription{Endpoint: "https://push.example/owner", Locale: "en", Session: notifications.Session{ID: "old", Generation: "generation", ExpiresAt: now.Add(time.Hour)}}
	sub.Keys.P256DH, sub.Keys.Auth = "key", "auth"
	sub, err = store.SaveSubscription(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	other := sub.Session
	other.ID = "other"
	if err := store.DeleteSubscription(ctx, sub.ID, other); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("other owner deleted subscription: %v", err)
	}
	rebound := sub
	rebound.Session = other
	rebound.Keys.Auth = "wrong"
	if _, err := store.SaveSubscription(ctx, rebound); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("endpoint alone transferred subscription: %v", err)
	}
	rebound.Keys = sub.Keys
	renewed, err := store.SaveSubscription(ctx, rebound)
	if err != nil || renewed.ID != sub.ID {
		t.Fatalf("renewal: %+v %v", renewed, err)
	}
	if _, err := store.GetSubscription(ctx, sub.ID, sub.Session); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("old owner retained access: %v", err)
	}
	if _, err := store.ClaimNotification(ctx, "rotated", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSubscription(ctx, sub.ID, other); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("token rotation retained subscription: %v", err)
	}
	rebound.Endpoint = "https://push.example/expired"
	rebound.Session.ExpiresAt = now.Add(time.Second)
	expiring, err := store.SaveSubscription(ctx, rebound)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNotification(ctx, "generation", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSubscription(ctx, expiring.ID, other); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expired subscription retained: %v", err)
	}
}
