package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

type activityLease struct {
	activity core.ChatActivity
	expires  time.Time
}
type chatActivity struct {
	leases map[string]activityLease
	sent   core.ChatActivity
	sentAt time.Time
	timer  *time.Timer
}
type accountActivity struct {
	mu    sync.Mutex
	chats map[string]*chatActivity
}

func (s *Service) updateActivity(ctx context.Context, rt *runtimeAccount, chatID, clientID string, activity core.ChatActivity) error {
	a := &rt.activity
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.chats == nil {
		a.chats = make(map[string]*chatActivity)
	}
	chat := a.chats[chatID]
	if chat == nil {
		if activity == core.ActivityPaused {
			return nil
		}
		if len(a.chats) >= 256 {
			return fmt.Errorf("%w: too many active conversations", core.ErrConflict)
		}
		chat = &chatActivity{leases: make(map[string]activityLease), sent: core.ActivityPaused}
		a.chats[chatID] = chat
	}
	if activity == core.ActivityPaused {
		delete(chat.leases, clientID)
	} else {
		if _, exists := chat.leases[clientID]; !exists && len(chat.leases) >= 32 {
			return fmt.Errorf("%w: too many active composers", core.ErrConflict)
		}
		chat.leases[clientID] = activityLease{activity, time.Now().Add(10 * time.Second)}
	}
	return s.flushActivityLocked(ctx, rt, chatID, chat)
}

func (s *Service) flushActivityLocked(ctx context.Context, rt *runtimeAccount, chatID string, chat *chatActivity) error {
	now := time.Now()
	state := core.ActivityPaused
	var expiry time.Time
	for id, lease := range chat.leases {
		if !lease.expires.After(now) {
			delete(chat.leases, id)
			continue
		}
		if expiry.IsZero() || lease.expires.Before(expiry) {
			expiry = lease.expires
		}
		if state != core.ActivityRecording {
			state = lease.activity
		}
	}
	if chat.timer != nil {
		chat.timer.Stop()
	}
	if !expiry.IsZero() {
		chat.timer = time.AfterFunc(time.Until(expiry), func() {
			rt.activity.mu.Lock()
			defer rt.activity.mu.Unlock()
			if rt.activity.chats[chatID] != chat || s.ctx.Err() != nil {
				return
			}
			timeout, cancel := context.WithTimeout(s.ctx, 5*time.Second)
			defer cancel()
			if err := s.flushActivityLocked(timeout, rt, chatID, chat); err != nil {
				s.logger.Debug("expire chat activity failed", "error", err)
			}
		})
	} else {
		delete(rt.activity.chats, chatID)
	}
	if state == chat.sent && (state == core.ActivityPaused || now.Sub(chat.sentAt) < 3*time.Second) {
		return nil
	}
	rt.mu.RLock()
	session, connected := rt.session, rt.state == "connected"
	rt.mu.RUnlock()
	sender, ok := session.(core.PresenceSender)
	if !connected || !ok {
		return core.ErrNotConnected
	}
	if err := sender.SendChatPresence(ctx, chatID, state); err != nil {
		return err
	}
	chat.sent, chat.sentAt = state, now
	return nil
}

func (s *Service) clearActivity(rt *runtimeAccount) {
	rt.activity.mu.Lock()
	defer rt.activity.mu.Unlock()
	for _, chat := range rt.activity.chats {
		if chat.timer != nil {
			chat.timer.Stop()
		}
	}
	rt.activity.chats = nil
}
