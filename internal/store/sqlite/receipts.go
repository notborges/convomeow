package sqlite

import (
	"context"
	"database/sql"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

const mergeReceiptTimes = `delivered_at = COALESCE(min(message_receipts.delivered_at, excluded.delivered_at), message_receipts.delivered_at, excluded.delivered_at),
read_at = COALESCE(min(message_receipts.read_at, excluded.read_at), message_receipts.read_at, excluded.read_at),
is_group = max(message_receipts.is_group, excluded.is_group)`

func (s *Store) SaveReceipt(ctx context.Context, accountID string, receipt core.Receipt) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	newLink := false
	if receipt.ParticipantAlias != "" {
		aliases, err := linkedJIDsTx(ctx, tx, accountID, []string{receipt.ParticipantID})
		if err != nil {
			return nil, err
		}
		newLink = !slices.Contains(aliases, receipt.ParticipantAlias)
	}
	changed, err := saveReceiptTx(ctx, tx, accountID, receipt)
	if err != nil {
		return nil, err
	}
	var conversations []string
	if changed || newLink {
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT conversation_id FROM conversation_aliases WHERE account_id=? AND (jid=? OR ?)`, accountID, receipt.ChatID, newLink)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			conversations = append(conversations, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return conversations, nil
}

func saveReceiptTx(ctx context.Context, tx *sql.Tx, accountID string, receipt core.Receipt) (bool, error) {
	if accountID == "" || receipt.ChatID == "" || receipt.ParticipantID == "" || receipt.At.IsZero() || len(receipt.MessageIDs) == 0 || len(receipt.MessageIDs) > 1000 || (receipt.Kind != "delivered" && receipt.Kind != "read") {
		return false, core.ErrInvalid
	}
	if receipt.ParticipantAlias != "" {
		if err := addChatLinkTx(ctx, tx, accountID, core.ChatLink{First: receipt.ParticipantID, Second: receipt.ParticipantAlias}); err != nil {
			return false, err
		}
	}
	aliases, err := linkedJIDsTx(ctx, tx, accountID, []string{receipt.ParticipantID})
	if err != nil {
		return false, err
	}
	sort.Strings(aliases)
	participant := aliases[0]
	var delivered, read any
	if receipt.Kind == "read" {
		read = dbTime(receipt.At)
	} else {
		delivered = dbTime(receipt.At)
	}
	changed := false
	for _, id := range receipt.MessageIDs {
		if id == "" {
			return false, core.ErrInvalid
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO message_receipts(account_id,chat_id,provider_message_id,participant_id,delivered_at,read_at,is_group)
 VALUES(?,?,?,?,?,?,?) ON CONFLICT(account_id,chat_id,provider_message_id,participant_id) DO UPDATE SET `+mergeReceiptTimes+`
 WHERE (excluded.delivered_at IS NOT NULL AND (message_receipts.delivered_at IS NULL OR excluded.delivered_at < message_receipts.delivered_at))
 OR (excluded.read_at IS NOT NULL AND (message_receipts.read_at IS NULL OR excluded.read_at < message_receipts.read_at))
 OR excluded.is_group > message_receipts.is_group`, accountID, receipt.ChatID, id, participant, delivered, read, receipt.Group)
		if err != nil {
			return false, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return false, err
		}
		changed = changed || count > 0
	}
	return changed, nil
}

func mergeReceiptParticipantsTx(ctx context.Context, tx *sql.Tx, accountID, participant string) error {
	aliases, err := linkedJIDsTx(ctx, tx, accountID, []string{participant})
	if err != nil {
		return err
	}
	if len(aliases) < 2 {
		return nil
	}
	sort.Strings(aliases)
	for _, alias := range aliases[1:] {
		if _, err := tx.ExecContext(ctx, `INSERT INTO message_receipts(account_id,chat_id,provider_message_id,participant_id,delivered_at,read_at,is_group)
 SELECT account_id,chat_id,provider_message_id,?,delivered_at,read_at,is_group FROM message_receipts WHERE account_id=? AND participant_id=?
 ON CONFLICT(account_id,chat_id,provider_message_id,participant_id) DO UPDATE SET `+mergeReceiptTimes, aliases[0], accountID, alias); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM message_receipts WHERE account_id=? AND participant_id=?`, accountID, alias); err != nil {
			return err
		}
	}
	return nil
}

const receiptJoins = ` FROM messages m JOIN conversations c ON c.id=m.conversation_id
 LEFT JOIN conversation_aliases a ON a.account_id=m.account_id AND a.conversation_id=m.conversation_id
 LEFT JOIN message_receipts r ON r.account_id=m.account_id AND r.chat_id=a.jid AND r.provider_message_id=m.provider_message_id `

func (s *Store) attachDelivery(ctx context.Context, messages []core.Message) error {
	var marks []string
	var args []any
	indexes := map[string]int{}
	for i, m := range messages {
		if m.Direction == "outbound" {
			marks = append(marks, "?")
			args = append(args, m.ID)
			indexes[m.ID] = i
		}
	}
	if len(args) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.public_id, c.kind='group' OR COALESCE(max(r.is_group),0),
 COUNT(DISTINCT CASE WHEN r.delivered_at IS NOT NULL OR r.read_at IS NOT NULL THEN r.participant_id END),
 COUNT(DISTINCT CASE WHEN r.read_at IS NOT NULL THEN r.participant_id END)`+receiptJoins+`WHERE m.public_id IN (`+strings.Join(marks, ",")+`) GROUP BY m.public_id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		d := core.DeliverySummary{State: "unknown"}
		if err := rows.Scan(&id, &d.Group, &d.DeliveredCount, &d.ReadCount); err != nil {
			return err
		}
		if d.ReadCount > 0 {
			d.State = "read"
		} else if d.DeliveredCount > 0 {
			d.State = "delivered"
		}
		if d.Group && d.State != "unknown" {
			d.State = "partial_" + d.State
		}
		messages[indexes[id]].Delivery = &d
	}
	return rows.Err()
}

func (s *Store) ListMessageReceipts(ctx context.Context, messageID, after string, limit int) ([]core.MessageReceipt, error) {
	if limit < 1 || limit > 201 {
		return nil, core.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.participant_id,min(r.delivered_at),min(r.read_at)`+receiptJoins+`
 WHERE m.public_id=? AND m.direction='outbound' AND r.participant_id > ? GROUP BY r.participant_id ORDER BY r.participant_id LIMIT ?`, messageID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]core.MessageReceipt, 0)
	for rows.Next() {
		var item core.MessageReceipt
		var delivered, read sql.NullString
		if err := rows.Scan(&item.ParticipantID, &delivered, &read); err != nil {
			return nil, err
		}
		if delivered.Valid {
			at, err := time.Parse(time.RFC3339Nano, delivered.String)
			if err != nil {
				return nil, err
			}
			item.DeliveredAt = &at
		}
		if read.Valid {
			at, err := time.Parse(time.RFC3339Nano, read.String)
			if err != nil {
				return nil, err
			}
			item.ReadAt = &at
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RecordReadReceipts(ctx context.Context, ids []string, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO message_read_receipts(message_id,read_at) VALUES(?,?) ON CONFLICT(message_id) DO NOTHING`, id, dbTime(at)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) attachReadReceipts(ctx context.Context, messages []core.Message) error {
	if len(messages) == 0 {
		return nil
	}
	marks, args := make([]string, len(messages)), make([]any, len(messages))
	indexes := map[string]int{}
	for i, m := range messages {
		marks[i], args[i], indexes[m.ID] = "?", m.ID, i
	}
	rows, err := s.db.QueryContext(ctx, `SELECT message_id,read_at FROM message_read_receipts WHERE message_id IN (`+strings.Join(marks, ",")+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, value string
		if err := rows.Scan(&id, &value); err != nil {
			return err
		}
		at, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return err
		}
		messages[indexes[id]].ReadAt = &at
	}
	return rows.Err()
}
