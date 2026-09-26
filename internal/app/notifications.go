package app

import (
	"context"
	"sync"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

type ChangeType string

const (
	AccountsChanged      ChangeType = "accounts.changed"
	ConversationsChanged ChangeType = "conversations.changed"
	AttachmentChanged    ChangeType = "attachment.changed"
	ContactsChanged      ChangeType = "contacts.changed"
	AvatarsChanged       ChangeType = "avatars.changed"
)

// Notifications invalidate snapshots; consumers recover missed changes by reading the API.
type Notification struct {
	Type           ChangeType `json:"type"`
	AccountID      string     `json:"account_id"`
	ConversationID string     `json:"conversation_id,omitempty"`
	AttachmentID   string     `json:"attachment_id,omitempty"`
}

type notifications struct {
	mu          sync.Mutex
	closed      bool
	subscribers map[chan Notification]struct{}
}

func (s *Service) Subscribe() (<-chan Notification, func()) {
	n := &s.notifications
	n.mu.Lock()
	defer n.mu.Unlock()
	ch := make(chan Notification, 64)
	if n.closed {
		close(ch)
		return ch, func() {}
	}
	if n.subscribers == nil {
		n.subscribers = make(map[chan Notification]struct{})
	}
	n.subscribers[ch] = struct{}{}
	return ch, func() {
		n.mu.Lock()
		defer n.mu.Unlock()
		if _, ok := n.subscribers[ch]; ok {
			delete(n.subscribers, ch)
			close(ch)
		}
	}
}

func (s *Service) publish(change Notification) {
	n := &s.notifications
	n.mu.Lock()
	defer n.mu.Unlock()
	for ch := range n.subscribers {
		select {
		case ch <- change:
		default:
			// A slow client must resync rather than silently miss an invalidation.
			delete(n.subscribers, ch)
			close(ch)
		}
	}
}

func (s *Service) closeNotifications() {
	n := &s.notifications
	n.mu.Lock()
	defer n.mu.Unlock()
	n.closed = true
	for ch := range n.subscribers {
		close(ch)
		delete(n.subscribers, ch)
	}
}

func (s *Service) notifyConversation(accountID, conversationID string) {
	s.publish(Notification{Type: ConversationsChanged, AccountID: accountID, ConversationID: conversationID})
}

func (s *Service) notifyMessage(id string) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	if message, err := s.repo.GetMessage(ctx, id); err == nil {
		s.notifyConversation(message.AccountID, message.ConversationID)
	}
}

func (s *Service) notifyMediaIfChanged(before core.MediaRecord) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	after, err := s.repo.GetMedia(ctx, before.AttachmentID)
	if err == nil && (before.Version != after.Version || before.Availability != after.Availability || before.AttemptCount != after.AttemptCount || before.FailureCode != after.FailureCode) {
		s.publish(Notification{Type: AttachmentChanged, AccountID: after.AccountID, AttachmentID: after.AttachmentID})
	}
}

func (s *Service) Done() <-chan struct{} { return s.ctx.Done() }
