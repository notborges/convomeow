package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

type PresenceUpdate struct {
	ParticipantID   string            `json:"participant_id"`
	DisplayName     string            `json:"display_name,omitempty"`
	Activity        core.ChatActivity `json:"activity"`
	TTLMilliseconds int               `json:"ttl_ms"`
}

func (s *Service) receivePresence(accountID string, presence *core.ChatPresence) {
	if presence == nil || !presence.Activity.Valid() || presence.ParticipantID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
	defer cancel()
	id, err := s.repo.FindConversationID(ctx, accountID, presence.ChatID)
	if errors.Is(err, core.ErrNotFound) {
		return
	}
	if err != nil {
		s.logger.Warn("resolve chat presence failed", "account_id", accountID, "error", err)
		return
	}
	update := &PresenceUpdate{ParticipantID: presence.ParticipantID, Activity: presence.Activity, TTLMilliseconds: 10000}
	if presence.Activity == core.ActivityPaused {
		update.TTLMilliseconds = 0
	}
	if contact, err := s.Contact(ctx, accountID, presence.ParticipantID); err == nil {
		update.DisplayName = contact.Name
	}
	s.publish(Notification{Type: PresenceChanged, AccountID: accountID, ConversationID: id, Presence: update})
}

// Each opted-in event stream owns one viewer; only the last release marks the account offline.
func (s *Service) WatchPresence(accountID string) (func(), error) {
	rt, err := s.runtime(accountID)
	if err != nil {
		return nil, err
	}
	rt.presenceMu.Lock()
	rt.presenceViewers++
	if rt.presenceViewers == 1 {
		s.setOnlineLocked(rt, true)
	}
	rt.presenceMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			rt.presenceMu.Lock()
			defer rt.presenceMu.Unlock()
			rt.presenceViewers--
			if rt.presenceViewers == 0 {
				s.setOnlineLocked(rt, false)
			}
		})
	}, nil
}

func (s *Service) syncOnline(rt *runtimeAccount) {
	rt.presenceMu.Lock()
	defer rt.presenceMu.Unlock()
	if rt.presenceViewers > 0 {
		s.setOnlineLocked(rt, true)
	}
}

func (s *Service) setOnlineLocked(rt *runtimeAccount, online bool) {
	rt.mu.RLock()
	session, state, id := rt.session, rt.state, rt.account.ID
	rt.mu.RUnlock()
	sender, ok := session.(core.PresenceSender)
	if !ok || state != "connected" {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	if err := sender.SetOnline(ctx, online); err != nil {
		s.logger.Warn("set account presence failed", "account_id", id, "error", err)
	}
}

func (s *Service) SendChatPresence(ctx context.Context, conversationID, clientID string, activity core.ChatActivity) error {
	if len(clientID) == 0 || len(clientID) > 128 {
		return fmt.Errorf("%w: client_id must contain 1-128 characters", core.ErrInvalid)
	}
	if !activity.Valid() {
		return fmt.Errorf("%w: activity must be typing, recording, or paused", core.ErrInvalid)
	}
	conversation, err := s.repo.GetConversation(ctx, conversationID)
	if err != nil {
		return err
	}
	rt, err := s.runtime(conversation.AccountID)
	if err != nil {
		return err
	}
	rt.mu.RLock()
	session, state := rt.session, rt.state
	rt.mu.RUnlock()
	if session == nil || state != "connected" {
		return core.ErrNotConnected
	}
	_, ok := session.(core.PresenceSender)
	if !ok {
		return fmt.Errorf("%w: provider does not support chat presence", core.ErrInvalid)
	}
	return s.updateActivity(ctx, rt, conversation.ProviderChatID, clientID, activity)
}
