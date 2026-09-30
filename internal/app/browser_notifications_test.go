package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/core"
	browsernotify "github.com/notborges/convomeow/internal/notifications"
	"github.com/notborges/convomeow/internal/store/sqlite"
)

type pushResponseSender struct {
	result browsernotify.Result
	calls  int
}

func (s *pushResponseSender) Send(context.Context, browsernotify.Subscription, browsernotify.Payload, time.Duration) (browsernotify.Result, error) {
	s.calls++
	return s.result, nil
}

func TestBrowserNotificationRetryDeadlineAndExpiredRecipient(t *testing.T) {
	ctx := context.Background()
	repo, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	now := time.Now().UTC()
	a := core.Account{ID: "account", Provider: "whatsapp", ConnectionKind: "linked_device", Label: "test", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateAccount(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.ProviderIdentity = "fake"
	sender := &pushResponseSender{result: browsernotify.Result{Status: 429, RetryAfter: 20 * time.Second}}
	service := New(repo, nil, nil)
	defer service.cancel()
	service.accounts[a.ID] = &runtimeAccount{account: a}
	if err := service.ConfigureNotifications(PushOptions{Sender: sender, PublicKey: "key", Generation: "generation"}); err != nil {
		t.Fatal(err)
	}
	sub, err := repo.SaveSubscription(ctx, browsernotify.Subscription{Endpoint: "https://push.example/recipient", Locale: "en", Preview: true,
		Session: browsernotify.Session{ID: "session", Generation: "generation", ExpiresAt: now.Add(time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	message := core.Message{AccountID: a.ID, ChatID: "person", ProviderMessageID: "first", Direction: "inbound", Text: "hello"}
	if _, err := repo.SaveIncomingMessage(ctx, message, "generation"); err != nil {
		t.Fatal(err)
	}
	claim := func(at time.Time) *browsernotify.Job {
		t.Helper()
		job, err := repo.ClaimNotification(ctx, "generation", at)
		if err != nil {
			t.Fatal(err)
		}
		return job
	}
	job := claim(now.Add(time.Second))
	if job == nil {
		t.Fatal("missing notification")
	}
	service.deliverNotification(*job)
	if claim(now.Add(19*time.Second)) != nil {
		t.Fatal("retry ignored Retry-After")
	}
	job = claim(now.Add(21 * time.Second))
	if job == nil || job.Attempts != 2 {
		t.Fatal("missing retry")
	}
	sender.result = browsernotify.Result{Status: 201}
	service.deliverNotification(*job)
	if claim(now.Add(time.Minute)) != nil || sender.calls != 2 {
		t.Fatal("successful delivery remained queued")
	}
	message.ProviderMessageID = "expired-recipient"
	if _, err := repo.SaveIncomingMessage(ctx, message, "generation"); err != nil {
		t.Fatal(err)
	}
	sender.result = browsernotify.Result{Status: 410}
	job = claim(now.Add(time.Second))
	if job == nil {
		t.Fatal("missing second notification")
	}
	service.deliverNotification(*job)
	if _, err := repo.GetSubscription(ctx, sub.ID, sub.Session); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expired recipient retained: %v", err)
	}
	if claim(now.Add(time.Minute)) != nil {
		t.Fatal("expired recipient retained pending work")
	}
}
