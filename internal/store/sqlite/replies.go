package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/notborges/convomeow/internal/core"
)

func saveReplyTx(ctx context.Context, tx *sql.Tx, id string, reply *core.Reply) error {
	if reply == nil {
		return nil
	}
	if reply.ProviderMessageID == "" {
		return core.ErrInvalid
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO message_replies(message_id, provider_message_id, sender_id, kind, text)
 VALUES(?, ?, ?, ?, ?) ON CONFLICT(message_id) DO NOTHING`, id, reply.ProviderMessageID, reply.SenderID, reply.Kind, reply.Text)
	return err
}

func (s *Store) attachReplies(ctx context.Context, messages []core.Message) error {
	if len(messages) == 0 {
		return nil
	}
	marks, args := make([]string, len(messages)), make([]any, len(messages))
	indexes := make(map[string]int, len(messages))
	for i, m := range messages {
		marks[i], args[i], indexes[m.ID] = "?", m.ID, i
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.message_id, COALESCE(original.public_id, ''), r.provider_message_id, r.sender_id, r.kind, r.text,
 EXISTS(SELECT 1 FROM message_changes c JOIN conversation_aliases ca ON ca.account_id=c.account_id AND ca.jid=c.chat_id
 WHERE ca.account_id=source.account_id AND ca.conversation_id=source.conversation_id AND c.target_id=r.provider_message_id AND c.kind='revoke')
 FROM message_replies r JOIN messages source ON source.public_id = r.message_id
 LEFT JOIN messages original ON original.account_id = source.account_id AND original.conversation_id = source.conversation_id
 AND original.provider_message_id = r.provider_message_id
 WHERE r.message_id IN (`+strings.Join(marks, ",")+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var reply core.Reply
		if err := rows.Scan(&id, &reply.MessageID, &reply.ProviderMessageID, &reply.SenderID, &reply.Kind, &reply.Text, &reply.Deleted); err != nil {
			return err
		}
		messages[indexes[id]].Reply = &reply
	}
	return rows.Err()
}
