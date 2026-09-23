package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/core"
)

const messageColumns = `public_id, account_id, conversation_id, chat_id, provider_message_id, direction, state, sender_id, kind, text, content_json, occurred_at, ingested_at`

func (s *Store) SaveMessage(ctx context.Context, m core.Message) (core.Message, error) {
	if m.AccountID == "" || m.ChatID == "" || m.ProviderMessageID == "" || m.Direction == "" {
		return core.Message{}, core.ErrInvalid
	}
	if err := prepareMessage(&m); err != nil {
		return core.Message{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.Message{}, err
	}
	defer tx.Rollback()
	saved, err := saveMessageTx(ctx, tx, m)
	if err != nil {
		return core.Message{}, err
	}
	if err := refreshConversationTx(ctx, tx, saved.ConversationID); err != nil {
		return core.Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return core.Message{}, err
	}
	return s.GetMessage(ctx, saved.ID)
}

func saveMessageTx(ctx context.Context, tx *sql.Tx, m core.Message) (core.Message, error) {
	if m.Direction == "outbound" {
		var existingID string
		err := tx.QueryRowContext(ctx, `SELECT public_id FROM messages WHERE account_id = ? AND provider_message_id = ? AND direction = 'outbound' LIMIT 1`,
			m.AccountID, m.ProviderMessageID).Scan(&existingID)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE messages SET state = CASE WHEN state IN ('queued', 'outcome_unknown') THEN 'sent' ELSE state END,
sender_id = CASE WHEN sender_id = '' THEN ? ELSE sender_id END,
occurred_at = CASE WHEN state IN ('queued', 'outcome_unknown') THEN ? ELSE occurred_at END WHERE public_id = ?`,
				m.SenderID, dbTime(m.OccurredAt), existingID)
			if err != nil {
				return core.Message{}, err
			}
			saved, err := scanMessage(tx.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE public_id = ?`, existingID))
			if err != nil {
				return core.Message{}, err
			}
			if err := saveAttachmentsTx(ctx, tx, saved.ID, m.Attachments); err != nil {
				return core.Message{}, err
			}
			return saved, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return core.Message{}, err
		}
	}
	if m.ConversationID == "" {
		conversation, _, err := ensureConversationTx(ctx, tx, m.AccountID, m.ChatID, nil, m.IngestedAt)
		if err != nil {
			return core.Message{}, err
		}
		m.ConversationID = conversation.ID
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO messages(public_id, account_id, conversation_id, chat_id, provider_message_id, direction, state, sender_id, kind, text, content_json, occurred_at, ingested_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(account_id, conversation_id, provider_message_id) DO UPDATE SET
state = CASE WHEN messages.direction = 'outbound' AND messages.state IN ('queued', 'outcome_unknown') THEN 'sent' ELSE messages.state END,
sender_id = CASE WHEN messages.sender_id = '' THEN excluded.sender_id ELSE messages.sender_id END,
text = CASE WHEN messages.text = '' THEN excluded.text ELSE messages.text END`,
		m.ID, m.AccountID, m.ConversationID, m.ChatID, m.ProviderMessageID, m.Direction, m.State, m.SenderID, m.Kind, m.Text, nullableContent(m.Content), dbTime(m.OccurredAt), dbTime(m.IngestedAt))
	if err != nil {
		return core.Message{}, normalizeError(err)
	}
	saved, err := scanMessage(tx.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE account_id = ? AND conversation_id = ? AND provider_message_id = ?`, m.AccountID, m.ConversationID, m.ProviderMessageID))
	if err != nil {
		return core.Message{}, err
	}
	if err := saveAttachmentsTx(ctx, tx, saved.ID, m.Attachments); err != nil {
		return core.Message{}, err
	}
	return saved, nil
}

func prepareMessage(m *core.Message) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	if m.Kind == "" {
		m.Kind = core.MessageKindText
	}
	if len(m.Content) > 0 && !json.Valid(m.Content) {
		return fmt.Errorf("%w: message content must be valid JSON", core.ErrInvalid)
	}
	if m.IngestedAt.IsZero() {
		m.IngestedAt = time.Now().UTC()
	}
	if m.OccurredAt.IsZero() {
		m.OccurredAt = m.IngestedAt
	}
	if m.State == "" {
		if m.Direction == "outbound" {
			m.State = "sent"
		} else {
			m.State = "received"
		}
	}
	return nil
}

func nullableContent(content json.RawMessage) any {
	if len(content) == 0 {
		return nil
	}
	return string(content)
}

func (s *Store) LookupSend(ctx context.Context, actorID, key, requestHash string) (core.Message, bool, error) {
	var storedHash, messageID string
	err := s.db.QueryRowContext(ctx, `SELECT request_hash, message_id FROM send_keys WHERE actor_id = ? AND key = ?`, actorID, key).Scan(&storedHash, &messageID)
	if errors.Is(err, sql.ErrNoRows) {
		return core.Message{}, false, nil
	}
	if err != nil {
		return core.Message{}, false, err
	}
	if storedHash != requestHash {
		return core.Message{}, false, core.ErrIdempotency
	}
	message, err := s.GetMessage(ctx, messageID)
	return message, true, err
}

func (s *Store) ReserveSend(ctx context.Context, m core.Message, actorID, key, requestHash string) (core.Message, bool, error) {
	if m.AccountID == "" || m.ConversationID == "" || m.ChatID == "" || m.ProviderMessageID == "" || actorID == "" || key == "" || requestHash == "" {
		return core.Message{}, false, core.ErrInvalid
	}
	m.Direction = "outbound"
	m.State = "queued"
	if err := prepareMessage(&m); err != nil {
		return core.Message{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.Message{}, false, err
	}
	defer tx.Rollback()
	var storedHash, messageID string
	err = tx.QueryRowContext(ctx, `SELECT request_hash, message_id FROM send_keys WHERE actor_id = ? AND key = ?`, actorID, key).Scan(&storedHash, &messageID)
	if err == nil {
		if storedHash != requestHash {
			return core.Message{}, false, core.ErrIdempotency
		}
		message, err := scanMessage(tx.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE public_id = ?`, messageID))
		return message, false, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return core.Message{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO messages(public_id, account_id, conversation_id, chat_id, provider_message_id, direction, state, sender_id, kind, text, content_json, occurred_at, ingested_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.AccountID, m.ConversationID, m.ChatID, m.ProviderMessageID, m.Direction, m.State, m.SenderID, m.Kind, m.Text, nullableContent(m.Content), dbTime(m.OccurredAt), dbTime(m.IngestedAt))
	if err != nil {
		return core.Message{}, false, normalizeError(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO send_keys(actor_id, key, request_hash, message_id) VALUES(?, ?, ?, ?)`, actorID, key, requestHash, m.ID); err != nil {
		return core.Message{}, false, normalizeError(err)
	}
	if err := refreshConversationTx(ctx, tx, m.ConversationID); err != nil {
		return core.Message{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return core.Message{}, false, err
	}
	return m, true, nil
}

func (s *Store) CompleteSend(ctx context.Context, id, state string, sent core.SentText) (core.Message, error) {
	if state != "sent" && state != "failed" && state != "outcome_unknown" {
		return core.Message{}, core.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.Message{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE messages SET state = ?,
sender_id = CASE WHEN ? = '' THEN sender_id ELSE ? END,
occurred_at = CASE WHEN ? THEN ? ELSE occurred_at END
WHERE public_id = ? AND state = 'queued'`, state, sent.SenderID, sent.SenderID, !sent.Timestamp.IsZero(), dbTime(sent.Timestamp), id)
	if err != nil {
		return core.Message{}, err
	}
	message, err := scanMessage(tx.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE public_id = ?`, id))
	if err != nil {
		return core.Message{}, err
	}
	if err := refreshConversationTx(ctx, tx, message.ConversationID); err != nil {
		return core.Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return core.Message{}, err
	}
	return message, nil
}

func (s *Store) GetMessage(ctx context.Context, id string) (core.Message, error) {
	message, err := scanMessage(s.db.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE public_id = ?`, id))
	if err != nil {
		return core.Message{}, err
	}
	messages := []core.Message{message}
	if err := s.attachToMessages(ctx, messages); err != nil {
		return core.Message{}, err
	}
	return messages[0], nil
}

func (s *Store) ListMessages(ctx context.Context, accountID string, before *core.PageCursor, limit int) ([]core.Message, error) {
	if accountID == "" {
		return nil, core.ErrInvalid
	}
	return s.listMessages(ctx, `account_id = ?`, accountID, before, limit)
}

func (s *Store) ListConversationMessages(ctx context.Context, conversationID string, before *core.PageCursor, limit int) ([]core.Message, error) {
	if conversationID == "" {
		return nil, core.ErrInvalid
	}
	return s.listMessages(ctx, `conversation_id = ?`, conversationID, before, limit)
}

func (s *Store) listMessages(ctx context.Context, scope string, scopeID string, before *core.PageCursor, limit int) ([]core.Message, error) {
	query := `SELECT ` + messageColumns + ` FROM messages WHERE ` + scope
	args := []any{scopeID}
	if before != nil {
		query += ` AND (occurred_at < ? OR (occurred_at = ? AND public_id < ?))`
		args = append(args, dbTime(before.Time), dbTime(before.Time), before.ID)
	}
	query += ` ORDER BY occurred_at DESC, public_id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]core.Message, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := s.attachToMessages(ctx, messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func scanMessage(row interface{ Scan(...any) error }) (core.Message, error) {
	var m core.Message
	var kind, occurred, ingested string
	var content sql.NullString
	err := row.Scan(&m.ID, &m.AccountID, &m.ConversationID, &m.ChatID, &m.ProviderMessageID, &m.Direction, &m.State,
		&m.SenderID, &kind, &m.Text, &content, &occurred, &ingested)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return m, core.ErrNotFound
		}
		return m, err
	}
	m.Kind = core.MessageKind(kind)
	if content.Valid {
		m.Content = json.RawMessage(content.String)
	}
	m.OccurredAt, err = time.Parse(time.RFC3339Nano, occurred)
	if err != nil {
		return m, fmt.Errorf("parse message occurred_at: %w", err)
	}
	m.IngestedAt, err = time.Parse(time.RFC3339Nano, ingested)
	if err != nil {
		return m, fmt.Errorf("parse message ingested_at: %w", err)
	}
	return m, nil
}
