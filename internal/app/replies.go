package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/notborges/convomeow/internal/core"
)

func (s *Service) replyTarget(ctx context.Context, conversation core.Conversation, id string) (*core.Reply, error) {
	if id == "" {
		return nil, nil
	}
	target, err := s.repo.GetMessage(ctx, id)
	if err != nil {
		return nil, err
	}
	if target.AccountID != conversation.AccountID || target.ConversationID != conversation.ID ||
		(target.State != "sent" && target.State != "received") || target.ProviderMessageID == "" {
		return nil, fmt.Errorf("%w: reply target must be a confirmed message in this conversation", core.ErrInvalid)
	}
	text := []rune(target.Text)
	if len(text) > 512 {
		text = text[:512]
	}
	return &core.Reply{MessageID: target.ID, ProviderMessageID: target.ProviderMessageID, SenderID: target.SenderID, Kind: target.Kind, Text: string(text)}, nil
}

func sendRequestHash(fields ...string) string {
	data, _ := json.Marshal(fields)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
