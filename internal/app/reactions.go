package app

import (
	"context"
	"fmt"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func (s *Service) receiveReaction(accountID string, reaction *core.Reaction) {
	if reaction == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	if err := s.saveReaction(ctx, accountID, *reaction); err != nil {
		s.logger.Warn("save reaction failed", "account_id", accountID, "error", err)
	}
}

func (s *Service) saveReaction(ctx context.Context, accountID string, r core.Reaction) error {
	conversations, err := s.repo.SaveReaction(ctx, accountID, r)
	if err != nil {
		return err
	}
	for _, id := range conversations {
		s.notifyConversation(accountID, id)
	}
	return nil
}

func (s *Service) SetReaction(ctx context.Context, id, emoji string) (core.Message, error) {
	if err := s.requireCapability("reactions"); err != nil {
		return core.Message{}, err
	}
	if emoji != "" && !core.ValidReactionEmoji(emoji) {
		return core.Message{}, fmt.Errorf("%w: provide one emoji", core.ErrInvalid)
	}
	target, err := s.repo.GetMessage(ctx, id)
	if err != nil {
		return core.Message{}, err
	}
	if target.State != "sent" && target.State != "received" {
		return core.Message{}, fmt.Errorf("%w: message is not confirmed", core.ErrConflict)
	}
	rt, err := s.runtime(target.AccountID)
	if err != nil {
		return core.Message{}, err
	}
	// Serialize this account's commands so two tabs cannot send replacements out of order.
	rt.reactionMu.Lock()
	defer rt.reactionMu.Unlock()
	if err := ctx.Err(); err != nil {
		return core.Message{}, err
	}
	rt.mu.RLock()
	session, state := rt.session, rt.state
	rt.mu.RUnlock()
	if state != "connected" || session == nil {
		return core.Message{}, core.ErrNotConnected
	}
	sender, ok := session.(core.ReactionSender)
	if !ok {
		return core.Message{}, fmt.Errorf("%w: provider does not support reactions", core.ErrInvalid)
	}
	target, err = s.repo.GetMessage(ctx, id)
	if err != nil {
		return core.Message{}, err
	}
	if !s.MessageActions(target).React {
		return core.Message{}, core.ErrUnsupported
	}
	for _, r := range target.Reactions {
		if r.Own && r.Emoji == emoji {
			return target, nil
		}
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	if !at.After(rt.reactionAt) {
		at = rt.reactionAt.Add(time.Millisecond)
	}
	rt.reactionAt = at
	reaction, err := sender.SendReaction(ctx, target, emoji, at)
	if err != nil {
		return core.Message{}, err
	}
	recordCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.saveReaction(recordCtx, target.AccountID, reaction); err != nil {
		return core.Message{}, fmt.Errorf("%w: save acknowledged reaction: %v", core.ErrReactionUnconfirmed, err)
	}
	return s.repo.GetMessage(recordCtx, id)
}

func (s *Service) MessageReactions(ctx context.Context, id, after string, limit int) ([]core.MessageReaction, error) {
	message, err := s.repo.GetMessage(ctx, id)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListMessageReactions(ctx, id, after, limit)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if !items[i].IsOwn {
			if contact, err := s.Contact(ctx, message.AccountID, items[i].ParticipantID); err == nil {
				items[i].DisplayName = contact.Name
			}
		}
	}
	return items, nil
}
