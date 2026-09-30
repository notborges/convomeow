package app

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/notborges/convomeow/internal/core"
	browsernotify "github.com/notborges/convomeow/internal/notifications"
	"github.com/notborges/convomeow/internal/notifications/webpush"
)

type PushOptions struct {
	Sender     browsernotify.Sender
	PublicKey  string
	Generation string
}

type browserView struct {
	SubscriptionID string
	ConversationID string
	ExpiresAt      time.Time
}

type pushState struct {
	options PushOptions
	repo    browsernotify.Repository
	wake    chan struct{}
	mu      sync.Mutex
	views   map[string]browserView
}

func (s *Service) ConfigureNotifications(options PushOptions) error {
	repo, ok := s.repo.(browsernotify.Repository)
	if !ok || options.Sender == nil || options.PublicKey == "" || options.Generation == "" {
		return core.ErrInvalid
	}
	s.push = &pushState{options: options, repo: repo, wake: make(chan struct{}, 2), views: make(map[string]browserView)}
	return nil
}

func (s *Service) NotificationConfig() (bool, string) {
	if s.push == nil {
		return false, ""
	}
	return true, s.push.options.PublicKey
}

func (s *Service) RegisterNotificationSubscription(ctx context.Context, sub browsernotify.Subscription) (browsernotify.Subscription, error) {
	if s.push == nil || sub.Session.Generation != s.push.options.Generation {
		return sub, core.ErrUnsupported
	}
	if err := webpush.Validate(sub); err != nil {
		return sub, core.ErrInvalid
	}
	return s.push.repo.SaveSubscription(ctx, sub)
}

func (s *Service) DeleteNotificationSubscription(ctx context.Context, id string, owner browsernotify.Session) error {
	if s.push == nil {
		return core.ErrNotFound
	}
	return s.push.repo.DeleteSubscription(ctx, id, owner)
}

func (s *Service) RevokeNotificationSession(ctx context.Context, owner browsernotify.Session) error {
	if s.push == nil {
		return nil
	}
	return s.push.repo.DeleteNotificationSession(ctx, owner)
}

func (s *Service) SetBrowserView(ctx context.Context, connectionID, subscriptionID, conversationID string, owner browsernotify.Session) error {
	if s.push == nil {
		return core.ErrUnsupported
	}
	if _, err := s.push.repo.GetSubscription(ctx, subscriptionID, owner); err != nil {
		return err
	}
	if conversationID != "" {
		if _, err := s.repo.GetConversation(ctx, conversationID); err != nil {
			return err
		}
	}
	s.push.mu.Lock()
	defer s.push.mu.Unlock()
	if conversationID == "" {
		delete(s.push.views, connectionID)
	} else {
		s.push.views[connectionID] = browserView{SubscriptionID: subscriptionID, ConversationID: conversationID, ExpiresAt: time.Now().Add(time.Minute)}
	}
	return nil
}

func (s *Service) ClearBrowserView(connectionID string) {
	if s.push == nil {
		return
	}
	s.push.mu.Lock()
	delete(s.push.views, connectionID)
	s.push.mu.Unlock()
}

func (s *Service) viewing(subscriptionID, conversationID string, now time.Time) bool {
	s.push.mu.Lock()
	defer s.push.mu.Unlock()
	viewing := false
	for id, view := range s.push.views {
		if !view.ExpiresAt.After(now) {
			delete(s.push.views, id)
			continue
		}
		if view.SubscriptionID == subscriptionID && view.ConversationID == conversationID {
			viewing = true
		}
	}
	return viewing
}

func (s *Service) startPushWorkers() {
	for range 2 {
		s.workWG.Add(1)
		go func() {
			defer s.workWG.Done()
			timer := time.NewTicker(2 * time.Second)
			defer timer.Stop()
			for {
				if s.ctx.Err() != nil {
					return
				}
				job, err := s.push.repo.ClaimNotification(s.ctx, s.push.options.Generation, time.Now())
				if err != nil && s.ctx.Err() == nil {
					s.logger.Warn("claim browser notification failed", "error", err)
				}
				if job != nil && err == nil {
					s.deliverNotification(*job)
					continue
				}
				select {
				case <-s.ctx.Done():
					return
				case <-timer.C:
				case <-s.push.wake:
				}
			}
		}()
	}
}

func (s *Service) deliverNotification(job browsernotify.Job) {
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	finish := func(next time.Time) {
		if err := s.push.repo.FinishNotification(ctx, job, next); err != nil && ctx.Err() == nil {
			s.logger.Warn("finish browser notification failed", "error", err)
		}
	}
	retry := func(after time.Duration) {
		next := time.Now().Add(max(time.Duration(1<<job.Attempts)*time.Second, after))
		if job.Attempts >= 5 || !next.Before(job.ExpiresAt) {
			next = time.Time{}
		}
		finish(next)
	}
	message, err := s.repo.GetMessage(ctx, job.MessageID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			finish(time.Time{})
		} else {
			retry(0)
		}
		return
	}
	if message.DeletedAt != nil || message.Direction != "inbound" {
		finish(time.Time{})
		return
	}
	account, err := s.Account(message.AccountID)
	if err != nil || account.ProviderIdentity == "" {
		finish(time.Time{})
		return
	}
	conversation, err := s.Conversation(ctx, message.ConversationID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			finish(time.Time{})
		} else {
			retry(0)
		}
		return
	}
	sub, err := s.push.repo.GetSubscription(ctx, job.Subscription.ID, job.Subscription.Session)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			finish(time.Time{})
		} else {
			retry(0)
		}
		return
	}
	if s.viewing(sub.ID, conversation.ID, time.Now()) {
		finish(time.Time{})
		return
	}
	payload := browsernotify.Payload{Version: 1, AccountID: account.ID, ConversationID: conversation.ID, MessageID: message.ID,
		AccountLabel: truncateNotification(account.Label, 64), ConversationName: truncateNotification(conversation.DisplayName, 100), Kind: message.Kind, Locale: sub.Locale}
	if sub.Preview {
		payload.Text = truncateNotification(message.Text, 160)
	} else {
		payload.Kind = ""
	}
	remaining := min(time.Until(job.ExpiresAt), time.Until(sub.Session.ExpiresAt))
	if remaining <= 0 {
		finish(time.Time{})
		return
	}
	result, err := s.push.options.Sender.Send(ctx, sub, payload, remaining)
	if result.Status == http.StatusNotFound || result.Status == http.StatusGone {
		if err := s.push.repo.DeleteSubscription(ctx, sub.ID, sub.Session); err != nil && !errors.Is(err, core.ErrNotFound) {
			retry(0)
		}
		return
	}
	if err == nil && result.Status >= 200 && result.Status < 300 {
		finish(time.Time{})
		return
	}
	transient := result.Status == 0 || result.Status == http.StatusTooManyRequests || result.Status >= 500
	if !transient {
		s.logger.Warn("browser notification rejected", "status", result.Status)
		finish(time.Time{})
		return
	}
	retry(result.RetryAfter)
}

func truncateNotification(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
