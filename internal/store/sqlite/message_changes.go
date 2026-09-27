package sqlite

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func saveMessageChangeTx(ctx context.Context, tx *sql.Tx, accountID string, c core.MessageChange) error {
	if accountID == "" || c.ChatID == "" || c.TargetID == "" || c.EventID == "" || c.At.IsZero() || (c.Kind != "edit" && c.Kind != "revoke") {
		return core.ErrInvalid
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO message_changes(account_id,chat_id,target_id,kind,text,at_ms,event_id) VALUES(?,?,?,?,?,?,?)
 ON CONFLICT DO NOTHING`, accountID, c.ChatID, c.TargetID, c.Kind, c.Text, c.At.UnixMilli(), c.EventID)
	return err
}

func (s *Store) SaveMessageChange(ctx context.Context, accountID string, c core.MessageChange) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = saveMessageChangeTx(ctx, tx, accountID, c); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT conversation_id FROM conversation_aliases WHERE account_id=? AND jid=?`, accountID, c.ChatID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return ids, tx.Commit()
}

// Keep the original envelope and every received change. Replayed history must not
// clear a deletion marker or move an edited message in the thread.
const rankedMessageChanges = `SELECT m.public_id,c.kind,c.text,c.at_ms,
 ROW_NUMBER() OVER(PARTITION BY m.public_id,c.kind ORDER BY c.at_ms DESC,c.event_id DESC) AS rank
 FROM messages m JOIN conversation_aliases a ON a.account_id=m.account_id AND a.conversation_id=m.conversation_id
 JOIN message_changes c ON c.account_id=m.account_id AND c.chat_id=a.jid AND c.target_id=m.provider_message_id`

func (s *Store) attachMessageChanges(ctx context.Context, messages []core.Message) error {
	if len(messages) == 0 {
		return nil
	}
	marks, args, indexes := make([]string, len(messages)), make([]any, len(messages)), map[string]int{}
	for i, m := range messages {
		marks[i] = "?"
		args[i] = m.ID
		indexes[m.ID] = i
	}
	rows, err := s.db.QueryContext(ctx, `WITH ranked AS (`+rankedMessageChanges+` WHERE m.public_id IN (`+strings.Join(marks, ",")+`)) SELECT public_id,kind,text,at_ms FROM ranked WHERE rank=1`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, kind, text string
		var ms int64
		if err := rows.Scan(&id, &kind, &text, &ms); err != nil {
			return err
		}
		m := &messages[indexes[id]]
		at := time.UnixMilli(ms).UTC()
		if kind == "revoke" {
			m.DeletedAt = &at
		} else {
			m.EditedAt = &at
			m.Text = text
			m.Content = nil
		}
	}
	return rows.Err()
}

func (s *Store) ListMessageRevisions(ctx context.Context, id string, before *core.PageCursor, limit int) ([]core.MessageRevision, error) {
	if limit < 1 || limit > 201 {
		return nil, core.ErrInvalid
	}
	original, err := scanMessage(s.db.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE public_id=?`, id))
	if err != nil {
		return nil, err
	}
	query := `WITH revisions AS (
 SELECT MIN(c.id) revision_id,c.kind,c.text,c.at_ms FROM messages m
 JOIN conversation_aliases a ON a.account_id=m.account_id AND a.conversation_id=m.conversation_id
 JOIN message_changes c ON c.account_id=m.account_id AND c.chat_id=a.jid AND c.target_id=m.provider_message_id
 WHERE m.public_id=? GROUP BY c.kind,c.text,c.at_ms
 UNION ALL SELECT 0,'original',?,?
 ) SELECT revision_id,kind,text,at_ms FROM revisions`
	args := []any{id, original.Text, original.OccurredAt.UnixMilli()}
	if before != nil {
		n, err := strconv.ParseInt(before.ID, 10, 64)
		if err != nil || n < 0 {
			return nil, core.ErrInvalid
		}
		query += ` WHERE (at_ms,revision_id)<(?,?)`
		args = append(args, before.Time.UnixMilli(), n)
	}
	query += ` ORDER BY at_ms DESC,revision_id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []core.MessageRevision{}
	for rows.Next() {
		var r core.MessageRevision
		var n, ms int64
		if err := rows.Scan(&n, &r.Kind, &r.Text, &ms); err != nil {
			return nil, err
		}
		r.ID = strconv.FormatInt(n, 10)
		r.At = time.UnixMilli(ms).UTC()
		result = append(result, r)
	}
	return result, rows.Err()
}
