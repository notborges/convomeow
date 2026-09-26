package app

import (
	"context"
	"fmt"

	"github.com/notborges/convomeow/internal/core"
)

func (s *Service) RequestHistory(ctx context.Context, conversationID, beforeID string, count int) (string, error) {
	if count < 1 || count > 50 {
		return "", fmt.Errorf("%w: history count must be between 1 and 50", core.ErrInvalid)
	}
	conversation, err := s.repo.GetConversation(ctx, conversationID)
	if err != nil {
		return "", err
	}
	message, err := s.repo.GetMessage(ctx, beforeID)
	if err != nil {
		return "", err
	}
	if message.ConversationID != conversation.ID || message.AccountID != conversation.AccountID || message.State == "queued" || message.State == "failed" || message.State == "outcome_unknown" {
		return "", fmt.Errorf("%w: history anchor must be a confirmed message in this conversation", core.ErrInvalid)
	}
	rt, err := s.runtime(conversation.AccountID)
	if err != nil {
		return "", err
	}
	rt.mu.RLock()
	session, connected := rt.session, rt.state == "connected"
	rt.mu.RUnlock()
	if !connected || session == nil {
		return "", core.ErrNotConnected
	}
	requester, ok := session.(core.HistoryRequester)
	if !ok {
		return "", fmt.Errorf("%w: provider does not support history requests", core.ErrInvalid)
	}
	message.ChatID = conversation.ProviderChatID
	return requester.RequestHistory(ctx, message, count)
}
