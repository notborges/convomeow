package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/core"
)

type conversationCandidate struct {
	id      string
	created string
}

func (s *Store) LinkChats(ctx context.Context, accountID string, link core.ChatLink) error {
	if accountID == "" {
		return core.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := addChatLinkTx(ctx, tx, accountID, link); err != nil {
		return err
	}
	return tx.Commit()
}

func ensureConversationTx(ctx context.Context, tx *sql.Tx, accountID, chatID string, aliases []string, createdAt time.Time) (core.Conversation, bool, error) {
	if accountID == "" || chatID == "" {
		return core.Conversation{}, false, core.ErrInvalid
	}
	jids, err := linkedJIDsTx(ctx, tx, accountID, append([]string{chatID}, aliases...))
	if err != nil {
		return core.Conversation{}, false, err
	}
	candidates := make(map[string]conversationCandidate)
	for _, jid := range jids {
		var candidate conversationCandidate
		err := tx.QueryRowContext(ctx, `SELECT c.id, c.created_at FROM conversation_aliases a JOIN conversations c ON c.id = a.conversation_id WHERE a.account_id = ? AND a.jid = ?`, accountID, jid).Scan(&candidate.id, &candidate.created)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return core.Conversation{}, false, err
		}
		candidates[candidate.id] = candidate
	}
	ordered := make([]conversationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		ordered = append(ordered, candidate)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].created == ordered[j].created {
			return ordered[i].id < ordered[j].id
		}
		return ordered[i].created < ordered[j].created
	})
	created := len(ordered) == 0
	conversationID := ""
	if created {
		conversationID = uuid.NewString()
		stamp := dbTime(createdAt)
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversations(id, account_id, provider_chat_id, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`, conversationID, accountID, chatID, stamp, stamp); err != nil {
			return core.Conversation{}, false, normalizeError(err)
		}
	} else {
		conversationID = ordered[0].id
		for _, duplicate := range ordered[1:] {
			if err := mergeConversationTx(ctx, tx, conversationID, duplicate.id); err != nil {
				return core.Conversation{}, false, err
			}
		}
	}
	for _, jid := range jids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_aliases(account_id, jid, conversation_id) VALUES(?, ?, ?)
ON CONFLICT(account_id, jid) DO UPDATE SET conversation_id = excluded.conversation_id`, accountID, jid, conversationID); err != nil {
			return core.Conversation{}, false, err
		}
	}
	conversation, _, err := scanConversation(tx.QueryRowContext(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE id = ?`, conversationID))
	return conversation, created, err
}

func linkedJIDsTx(ctx context.Context, tx *sql.Tx, accountID string, seeds []string) ([]string, error) {
	seen := make(map[string]bool)
	jids := make([]string, 0, len(seeds))
	for _, jid := range seeds {
		if jid != "" && !seen[jid] {
			seen[jid] = true
			jids = append(jids, jid)
		}
	}
	for i := 0; i < len(jids); i++ {
		rows, err := tx.QueryContext(ctx, `SELECT peer_jid FROM jid_links WHERE account_id = ? AND jid = ?`, accountID, jids[i])
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var peer string
			if err := rows.Scan(&peer); err != nil {
				rows.Close()
				return nil, err
			}
			if peer != "" && !seen[peer] {
				seen[peer] = true
				jids = append(jids, peer)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return jids, nil
}

func addChatLinkTx(ctx context.Context, tx *sql.Tx, accountID string, link core.ChatLink) error {
	if link.First == "" || link.Second == "" || link.First == link.Second {
		return nil
	}
	for _, pair := range [][2]string{{link.First, link.Second}, {link.Second, link.First}} {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO jid_links(account_id, jid, peer_jid) VALUES(?, ?, ?)`, accountID, pair[0], pair[1]); err != nil {
			return normalizeError(err)
		}
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversation_aliases WHERE account_id = ? AND jid IN (?, ?)`, accountID, link.First, link.Second).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	conversation, _, err := ensureConversationTx(ctx, tx, accountID, link.First, []string{link.Second}, time.Now().UTC())
	if err != nil {
		return err
	}
	return refreshConversationTx(ctx, tx, conversation.ID)
}

func mergeConversationTx(ctx context.Context, tx *sql.Tx, targetID, sourceID string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE conversations SET
kind = CASE WHEN source.kind = 'group' THEN 'group' ELSE conversations.kind END,
display_name = CASE WHEN conversations.display_name = '' THEN source.display_name ELSE conversations.display_name END,
description = CASE WHEN conversations.description = '' THEN source.description ELSE conversations.description END
FROM conversations AS source WHERE conversations.id = ? AND source.id = ?`, targetID, sourceID); err != nil {
		return err
	}
	type entry struct{ id, providerID, direction string }
	for {
		rows, err := tx.QueryContext(ctx, `SELECT public_id, provider_message_id, direction FROM messages WHERE conversation_id = ? LIMIT 100`, sourceID)
		if err != nil {
			return err
		}
		var messages []entry
		for rows.Next() {
			var m entry
			if err := rows.Scan(&m.id, &m.providerID, &m.direction); err != nil {
				rows.Close()
				return err
			}
			messages = append(messages, m)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(messages) == 0 {
			break
		}
		for _, m := range messages {
			var existingID, existingDirection string
			err := tx.QueryRowContext(ctx, `SELECT public_id, direction FROM messages WHERE conversation_id = ? AND provider_message_id = ?`, targetID, m.providerID).Scan(&existingID, &existingDirection)
			if errors.Is(err, sql.ErrNoRows) {
				_, err = tx.ExecContext(ctx, `UPDATE messages SET conversation_id = ? WHERE public_id = ?`, targetID, m.id)
				if err != nil {
					return err
				}
				continue
			}
			if err != nil {
				return err
			}
			var sourceKeys, targetKeys int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM send_keys WHERE message_id = ?`, m.id).Scan(&sourceKeys); err != nil {
				return err
			}
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM send_keys WHERE message_id = ?`, existingID).Scan(&targetKeys); err != nil {
				return err
			}
			if targetKeys == 0 && (sourceKeys > 0 || (m.direction == "outbound" && existingDirection != "outbound")) {
				if err := mergeAttachmentsTx(ctx, tx, m.id, existingID); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE public_id = ?`, existingID); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `UPDATE messages SET conversation_id = ? WHERE public_id = ?`, targetID, m.id); err != nil {
					return err
				}
				continue
			}
			if err := mergeAttachmentsTx(ctx, tx, existingID, m.id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE send_keys SET message_id = ? WHERE message_id = ?`, existingID, m.id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE public_id = ?`, m.id); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversation_aliases SET conversation_id = ? WHERE conversation_id = ?`, targetID, sourceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, sourceID); err != nil {
		return err
	}
	return refreshConversationTx(ctx, tx, targetID)
}
