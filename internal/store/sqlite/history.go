package sqlite

import (
	"context"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func (s *Store) ImportHistory(ctx context.Context, batch core.HistoryBatch) error {
	if batch.AccountID == "" {
		return core.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, link := range batch.Links {
		if err := addChatLinkTx(ctx, tx, batch.AccountID, link); err != nil {
			return err
		}
	}
	if batch.Chat != nil {
		if batch.Chat.ID == "" {
			return core.ErrInvalid
		}
		aliases := append(append([]string(nil), batch.Chat.Aliases...), batch.Chat.PreferredID)
		conversation, _, err := ensureConversationTx(ctx, tx, batch.AccountID, batch.Chat.ID, aliases, time.Now().UTC())
		if err != nil {
			return err
		}
		if batch.Chat.PreferredID != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE conversations SET provider_chat_id = ? WHERE id = ?`, batch.Chat.PreferredID, conversation.ID); err != nil {
				return normalizeError(err)
			}
		}
		profile := core.ChatProfile{Kind: batch.Chat.Kind}
		if batch.Chat.DisplayName != "" {
			profile.DisplayName = &batch.Chat.DisplayName
		}
		if batch.Chat.Description != "" {
			profile.Description = &batch.Chat.Description
		}
		if err := updateChatProfileTx(ctx, tx, conversation.ID, profile); err != nil {
			return err
		}
		for _, message := range batch.Messages {
			message.AccountID = batch.AccountID
			message.ConversationID = conversation.ID
			if message.ChatID == "" || message.ProviderMessageID == "" || message.Direction == "" {
				return core.ErrInvalid
			}
			if err := prepareMessage(&message); err != nil {
				return err
			}
			if _, err := saveMessageTx(ctx, tx, message); err != nil {
				return err
			}
		}
		if err := refreshConversationTx(ctx, tx, conversation.ID); err != nil {
			return err
		}
		if !batch.Chat.LastActivityAt.IsZero() {
			stamp := dbTime(batch.Chat.LastActivityAt)
			if _, err := tx.ExecContext(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ? AND last_message_id IS NULL`, stamp, conversation.ID); err != nil {
				return err
			}
		}
	} else if len(batch.Messages) > 0 {
		return core.ErrInvalid
	}
	return tx.Commit()
}
