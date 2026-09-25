package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

const conversationColumns = `id, account_id, provider_chat_id, kind, display_name, description, created_at, updated_at, last_message_id`

func (s *Store) GetOrCreateConversation(ctx context.Context, accountID, providerChatID string) (core.Conversation, bool, error) {
	if accountID == "" || providerChatID == "" {
		return core.Conversation{}, false, core.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.Conversation{}, false, err
	}
	defer tx.Rollback()
	conversation, created, err := ensureConversationTx(ctx, tx, accountID, providerChatID, nil, time.Now().UTC())
	if err != nil {
		return core.Conversation{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return core.Conversation{}, false, err
	}
	conversation, err = s.GetConversation(ctx, conversation.ID)
	return conversation, created, err
}

func (s *Store) GetConversation(ctx context.Context, id string) (core.Conversation, error) {
	conversation, lastID, err := scanConversation(s.db.QueryRowContext(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE id = ?`, id))
	if err != nil {
		return core.Conversation{}, err
	}
	if lastID != "" {
		last, err := s.GetMessage(ctx, lastID)
		if err != nil {
			return core.Conversation{}, err
		}
		conversation.LastMessage = &last
	}
	return conversation, nil
}

func (s *Store) ListConversations(ctx context.Context, accountID string, before *core.PageCursor, limit int) ([]core.Conversation, error) {
	query := `SELECT ` + conversationColumns + ` FROM conversations`
	var conditions []string
	var args []any
	if accountID != "" {
		conditions = append(conditions, `account_id = ?`)
		args = append(args, accountID)
	}
	if before != nil {
		conditions = append(conditions, `(updated_at < ? OR (updated_at = ? AND id < ?))`)
		args = append(args, dbTime(before.Time), dbTime(before.Time), before.ID)
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, ` AND `)
	}
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	conversations := make([]core.Conversation, 0)
	var lastIDs []string
	for rows.Next() {
		conversation, lastID, err := scanConversation(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		conversations = append(conversations, conversation)
		lastIDs = append(lastIDs, lastID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	ids := make([]any, 0, len(lastIDs))
	placeholders := make([]string, 0, len(lastIDs))
	for _, id := range lastIDs {
		if id == "" {
			continue
		}
		ids = append(ids, id)
		placeholders = append(placeholders, "?")
	}
	lastByID := make(map[string]core.Message, len(ids))
	if len(ids) > 0 {
		messageRows, err := s.db.QueryContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE public_id IN (`+strings.Join(placeholders, ",")+`)`, ids...)
		if err != nil {
			return nil, err
		}
		for messageRows.Next() {
			message, err := scanMessage(messageRows)
			if err != nil {
				messageRows.Close()
				return nil, err
			}
			lastByID[message.ID] = message
		}
		if err := messageRows.Err(); err != nil {
			messageRows.Close()
			return nil, err
		}
		messageRows.Close()
	}
	lastMessages := make([]core.Message, 0, len(lastByID))
	for _, message := range lastByID {
		lastMessages = append(lastMessages, message)
	}
	if err := s.attachToMessages(ctx, lastMessages); err != nil {
		return nil, err
	}
	for _, message := range lastMessages {
		lastByID[message.ID] = message
	}
	for i, id := range lastIDs {
		if last, ok := lastByID[id]; ok {
			conversations[i].LastMessage = &last
		}
	}
	return conversations, nil
}

func (s *Store) ListGroupChatIDs(ctx context.Context, accountID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider_chat_id FROM conversations WHERE account_id = ? AND kind = 'group' ORDER BY provider_chat_id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func scanConversation(row interface{ Scan(...any) error }) (core.Conversation, string, error) {
	var conversation core.Conversation
	var created, updated string
	var lastID sql.NullString
	if err := row.Scan(&conversation.ID, &conversation.AccountID, &conversation.ProviderChatID, &conversation.Kind, &conversation.DisplayName, &conversation.Description, &created, &updated, &lastID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return conversation, "", core.ErrNotFound
		}
		return conversation, "", err
	}
	if conversation.DisplayName == "" {
		conversation.DisplayName = conversation.ProviderChatID
	}
	var err error
	conversation.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return conversation, "", fmt.Errorf("parse conversation created_at: %w", err)
	}
	conversation.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return conversation, "", fmt.Errorf("parse conversation updated_at: %w", err)
	}
	return conversation, lastID.String, nil
}

func (s *Store) UpdateChatProfile(ctx context.Context, accountID string, profile core.ChatProfile) error {
	if accountID == "" || profile.ProviderChatID == "" {
		return core.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if profile.AlternateID != "" {
		var linkedConversation string
		linkErr := tx.QueryRowContext(ctx, `SELECT conversation_id FROM conversation_aliases WHERE account_id = ? AND jid IN (?, ?) LIMIT 1`,
			accountID, profile.ProviderChatID, profile.AlternateID).Scan(&linkedConversation)
		if errors.Is(linkErr, sql.ErrNoRows) && !profile.CreateConversation {
			return nil
		}
		if linkErr != nil && !errors.Is(linkErr, sql.ErrNoRows) {
			return linkErr
		}
		if err := addChatLinkTx(ctx, tx, accountID, core.ChatLink{First: profile.ProviderChatID, Second: profile.AlternateID}); err != nil {
			return err
		}
	}
	var conversationID string
	err = tx.QueryRowContext(ctx, `SELECT conversation_id FROM conversation_aliases WHERE account_id = ? AND jid = ?`, accountID, profile.ProviderChatID).Scan(&conversationID)
	if errors.Is(err, sql.ErrNoRows) {
		if !profile.CreateConversation {
			return tx.Commit()
		}
		conversation, _, createErr := ensureConversationTx(ctx, tx, accountID, profile.ProviderChatID, nil, time.Now().UTC())
		if createErr != nil {
			return createErr
		}
		conversationID = conversation.ID
		err = nil
	}
	if err != nil {
		return err
	}
	if err := updateChatProfileTx(ctx, tx, conversationID, profile); err != nil {
		return err
	}
	return tx.Commit()
}

func updateChatProfileTx(ctx context.Context, tx *sql.Tx, conversationID string, profile core.ChatProfile) error {
	if profile.Kind != "" && profile.Kind != "direct" && profile.Kind != "group" {
		return core.ErrInvalid
	}
	_, err := tx.ExecContext(ctx, `UPDATE conversations SET
kind = COALESCE(NULLIF(?, ''), kind),
display_name = COALESCE(?, display_name),
description = COALESCE(?, description)
WHERE id = ?`, profile.Kind, profile.DisplayName, profile.Description, conversationID)
	return err
}

func refreshConversationTx(ctx context.Context, tx *sql.Tx, conversationID string) error {
	var messageID, occurredAt string
	err := tx.QueryRowContext(ctx, `SELECT public_id, occurred_at FROM messages WHERE conversation_id = ? ORDER BY occurred_at DESC, public_id DESC LIMIT 1`, conversationID).Scan(&messageID, &occurredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE conversations SET updated_at = ?, last_message_id = ? WHERE id = ?`, occurredAt, messageID, conversationID)
	return err
}
