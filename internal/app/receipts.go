package app

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func (s *Service) MessageReceipts(ctx context.Context, id, after string, limit int) ([]core.MessageReceipt, error) {
	if err := s.requireCapability("message_receipts"); err != nil {
		return nil, err
	}
	message, err := s.repo.GetMessage(ctx, id)
	if err != nil {
		return nil, err
	}
	if message.Direction != "outbound" {
		return nil, fmt.Errorf("%w: recipient receipts apply to outgoing messages", core.ErrInvalid)
	}
	items, err := s.repo.ListMessageReceipts(ctx, id, after, limit)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if contact, err := s.Contact(ctx, message.AccountID, items[i].ParticipantID); err == nil {
			items[i].DisplayName = contact.Name
		}
	}
	return items, nil
}

type ReadReceiptFailure struct {
	MessageIDs []string `json:"message_ids"`
	Code       string   `json:"code"`
}

type ReadReceiptResult struct {
	ReadIDs []string             `json:"read_message_ids"`
	Failed  []ReadReceiptFailure `json:"failed"`
}

func (s *Service) SendReadReceipts(ctx context.Context, conversationID string, ids []string) (ReadReceiptResult, error) {
	if err := s.requireCapability("read_receipts"); err != nil {
		return ReadReceiptResult{}, err
	}
	result := ReadReceiptResult{ReadIDs: []string{}, Failed: []ReadReceiptFailure{}}
	if len(ids) == 0 || len(ids) > 100 {
		return result, fmt.Errorf("%w: provide 1-100 message IDs", core.ErrInvalid)
	}
	conversation, err := s.repo.GetConversation(ctx, conversationID)
	if err != nil {
		return result, err
	}
	seen := map[string]bool{}
	groups := map[string][]core.Message{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		message, err := s.repo.GetMessage(ctx, id)
		if err != nil {
			return result, err
		}
		if message.ConversationID != conversation.ID || message.AccountID != conversation.AccountID || message.Direction != "inbound" || message.SenderID == "" {
			return result, fmt.Errorf("%w: read receipts require incoming messages in this conversation", core.ErrInvalid)
		}
		if message.ReadAt != nil {
			result.ReadIDs = append(result.ReadIDs, id)
		} else {
			groups[message.SenderID] = append(groups[message.SenderID], message)
		}
	}
	if len(groups) == 0 {
		return result, nil
	}
	rt, err := s.runtime(conversation.AccountID)
	if err != nil {
		return result, err
	}
	rt.mu.RLock()
	session, state := rt.session, rt.state
	rt.mu.RUnlock()
	if state != "connected" || session == nil {
		return result, core.ErrNotConnected
	}
	sender, ok := session.(core.ReadReceiptSender)
	if !ok {
		return result, fmt.Errorf("%w: provider does not support sending read receipts", core.ErrInvalid)
	}
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		messages := groups[name]
		publicIDs, providerIDs := make([]string, 0, len(messages)), make([]string, 0, len(messages))
		for _, m := range messages {
			publicIDs = append(publicIDs, m.ID)
			providerIDs = append(providerIDs, m.ProviderMessageID)
		}
		at := time.Now().UTC()
		if err := sender.SendReadReceipts(ctx, conversation.ProviderChatID, name, providerIDs, at); err != nil {
			result.Failed = append(result.Failed, ReadReceiptFailure{publicIDs, "receipt_send_failed"})
			continue
		}
		recordCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := s.repo.RecordReadReceipts(recordCtx, publicIDs, at)
		cancel()
		if err != nil {
			result.Failed = append(result.Failed, ReadReceiptFailure{publicIDs, "receipt_record_failed"})
			continue
		}
		result.ReadIDs = append(result.ReadIDs, publicIDs...)
	}
	if len(result.ReadIDs) > 0 {
		s.notifyConversation(conversation.AccountID, conversationID)
	}
	return result, nil
}
