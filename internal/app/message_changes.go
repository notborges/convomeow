package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func (s *Service) saveMessageChange(ctx context.Context, accountID string, c core.MessageChange) error {
	ids, err := s.repo.SaveMessageChange(ctx, accountID, c)
	if err != nil {
		return err
	}
	for _, id := range ids {
		s.notifyConversation(accountID, id)
	}
	return nil
}
func (s *Service) receiveMessageChange(accountID string, c *core.MessageChange) {
	if c == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	if err := s.saveMessageChange(ctx, accountID, *c); err != nil {
		s.logger.Warn("save message change failed", "account_id", accountID, "error", err)
	}
}
func (s *Service) ChangeMessage(ctx context.Context, id, kind, text string) (core.Message, error) {
	if kind != "edit" && kind != "revoke" {
		return core.Message{}, core.ErrInvalid
	}
	if err := s.requireCapability(map[string]string{"edit": "edit_messages", "revoke": "revoke_messages"}[kind]); err != nil {
		return core.Message{}, err
	}
	text = strings.TrimSpace(text)
	if kind == "edit" && (text == "" || len(text) > 4096) {
		return core.Message{}, fmt.Errorf("%w: text must be 1-4096 bytes", core.ErrInvalid)
	}
	m, err := s.repo.GetMessage(ctx, id)
	if err != nil {
		return core.Message{}, err
	}
	rt, err := s.runtime(m.AccountID)
	if err != nil {
		return core.Message{}, err
	}
	rt.changeMu.Lock()
	defer rt.changeMu.Unlock()
	if err = ctx.Err(); err != nil {
		return core.Message{}, err
	}
	m, err = s.repo.GetMessage(ctx, id)
	if err != nil {
		return core.Message{}, err
	}
	a := s.MessageActions(m)
	if (kind == "edit" && !a.Edit) || (kind == "revoke" && !a.Revoke) {
		return core.Message{}, fmt.Errorf("%w: action is not available for this message", core.ErrConflict)
	}
	rt.mu.RLock()
	session, state := rt.session, rt.state
	rt.mu.RUnlock()
	if session == nil || state != "connected" {
		return core.Message{}, core.ErrNotConnected
	}
	sender, ok := session.(core.MessageChanger)
	if !ok {
		return core.Message{}, core.ErrUnsupported
	}
	if kind == "edit" && text == m.Text {
		return m, nil
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	if !at.After(rt.changeAt) {
		at = rt.changeAt.Add(time.Millisecond)
	}
	if m.EditedAt != nil && !at.After(*m.EditedAt) {
		at = m.EditedAt.Add(time.Millisecond)
	}
	rt.changeAt = at
	var change core.MessageChange
	if kind == "edit" {
		change, err = sender.EditMessage(ctx, m, text, at)
	} else {
		change, err = sender.RevokeMessage(ctx, m, at)
	}
	if err != nil {
		return core.Message{}, err
	}
	recordCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = s.saveMessageChange(recordCtx, m.AccountID, change); err != nil {
		return core.Message{}, fmt.Errorf("%w: record acknowledged change: %v", core.ErrMessageChangeUnconfirmed, err)
	}
	return s.repo.GetMessage(recordCtx, id)
}

func (s *Service) MessageRevisions(ctx context.Context, id string, before *core.PageCursor, limit int) ([]core.MessageRevision, error) {
	return s.repo.ListMessageRevisions(ctx, id, before, limit)
}
