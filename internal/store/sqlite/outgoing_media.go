package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func (s *Store) CreateUpload(ctx context.Context, u core.Upload) error {
	if u.ID == "" || u.AccountID == "" || u.ProfileID == "" || u.ObjectKey == "" || u.Size < 1 || len(u.SHA256) != 32 {
		return core.ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO uploads(id, account_id, profile_id, object_key, mime_type, file_name, size, sha256, state, created_at, expires_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, 'staging', ?, ?)`, u.ID, u.AccountID, u.ProfileID, u.ObjectKey, u.MIMEType, u.FileName,
		u.Size, u.SHA256, dbTime(time.Now()), dbTime(u.ExpiresAt))
	return normalizeError(err)
}

func (s *Store) MarkUploadReady(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE uploads SET state = 'ready' WHERE id = ? AND state = 'staging'`, id)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) MarkUploadDeleting(ctx context.Context, accountID, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE uploads SET state = 'deleting' WHERE id = ? AND account_id = ? AND state IN ('ready', 'staging')`, id, accountID)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) ListUploadsForCleanup(ctx context.Context, now time.Time, limit int) ([]core.Upload, error) {
	if limit < 1 || limit > 1000 {
		return nil, core.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE uploads SET state = 'deleting' WHERE (state = 'ready' AND expires_at <= ?)
OR (state = 'staging' AND created_at <= ?)`, dbTime(now), dbTime(now.Add(-time.Hour)))
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, account_id, profile_id, object_key, mime_type, file_name, size, sha256, state, expires_at
FROM uploads WHERE state = 'deleting' ORDER BY expires_at, id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	var uploads []core.Upload
	for rows.Next() {
		var u core.Upload
		var expires string
		if err := rows.Scan(&u.ID, &u.AccountID, &u.ProfileID, &u.ObjectKey, &u.MIMEType, &u.FileName, &u.Size, &u.SHA256, &u.State, &expires); err != nil {
			rows.Close()
			return nil, err
		}
		u.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
		if err != nil {
			rows.Close()
			return nil, err
		}
		uploads = append(uploads, u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return uploads, nil
}

func (s *Store) ClearUpload(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM uploads WHERE id = ? AND state = 'deleting'`, id)
	return err
}

func (s *Store) ReserveMediaSend(ctx context.Context, m core.Message, actorID, key, requestHash, uploadID string) (core.Message, bool, error) {
	if m.AccountID == "" || m.ConversationID == "" || m.ChatID == "" || m.ProviderMessageID == "" ||
		actorID == "" || key == "" || requestHash == "" || uploadID == "" {
		return core.Message{}, false, core.ErrInvalid
	}
	m.Direction, m.State = "outbound", "queued"
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
		saved, err := scanMessage(tx.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE public_id = ?`, messageID))
		return saved, false, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return core.Message{}, false, err
	}
	var upload core.Upload
	err = tx.QueryRowContext(ctx, `SELECT id, account_id, profile_id, object_key, mime_type, file_name, size, sha256
FROM uploads WHERE id = ? AND account_id = ? AND state = 'ready' AND expires_at > ?`, uploadID, m.AccountID, dbTime(time.Now())).
		Scan(&upload.ID, &upload.AccountID, &upload.ProfileID, &upload.ObjectKey, &upload.MIMEType, &upload.FileName, &upload.Size, &upload.SHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return core.Message{}, false, core.ErrNotFound
	}
	if err != nil {
		return core.Message{}, false, err
	}
	if err := core.ValidateMediaType(m.Kind, upload.MIMEType); err != nil {
		return core.Message{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO messages(public_id, account_id, conversation_id, chat_id, provider_message_id, direction, state, sender_id, kind, text, content_json, occurred_at, ingested_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, m.ID, m.AccountID, m.ConversationID, m.ChatID, m.ProviderMessageID,
		m.Direction, m.State, m.SenderID, m.Kind, m.Text, nullableContent(m.Content), dbTime(m.OccurredAt), dbTime(m.IngestedAt))
	if err != nil {
		return core.Message{}, false, normalizeError(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO attachments(id, message_id, part_index, kind, mime_type, file_name, size, availability,
storage_profile_id, object_key, stored_size, stored_sha256) VALUES(?, ?, 0, ?, ?, ?, ?, 'ready', ?, ?, ?, ?)`,
		upload.ID, m.ID, m.Kind, upload.MIMEType, upload.FileName, upload.Size, upload.ProfileID, upload.ObjectKey, upload.Size, upload.SHA256)
	if err != nil {
		return core.Message{}, false, normalizeError(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO outgoing_media_jobs(message_id, phase) VALUES(?, 'queued')`, m.ID); err != nil {
		return core.Message{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO send_keys(actor_id, key, request_hash, message_id) VALUES(?, ?, ?, ?)`, actorID, key, requestHash, m.ID); err != nil {
		return core.Message{}, false, normalizeError(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM uploads WHERE id = ? AND state = 'ready'`, upload.ID); err != nil {
		return core.Message{}, false, err
	}
	if err := refreshConversationTx(ctx, tx, m.ConversationID); err != nil {
		return core.Message{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return core.Message{}, false, err
	}
	m.Attachments = []core.Attachment{{ID: upload.ID, Kind: m.Kind, MIMEType: upload.MIMEType, FileName: upload.FileName,
		Size: uint64(upload.Size), Availability: "ready"}}
	return m, true, nil
}

func (s *Store) ListPendingMediaSends(ctx context.Context, now time.Time, limit int) ([]string, error) {
	if limit < 1 || limit > 1000 {
		return nil, core.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT j.message_id FROM outgoing_media_jobs j
JOIN messages m ON m.public_id = j.message_id WHERE j.phase = 'queued' AND m.state = 'queued' AND
(j.next_attempt_at IS NULL OR j.next_attempt_at <= ?) ORDER BY j.message_id LIMIT ?`, dbTime(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) ClaimMediaSend(ctx context.Context, id string) (core.OutgoingMediaJob, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.OutgoingMediaJob{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE outgoing_media_jobs SET phase = 'uploading' WHERE message_id = ? AND phase = 'queued' AND
(next_attempt_at IS NULL OR next_attempt_at <= ?) AND EXISTS
(SELECT 1 FROM messages m WHERE m.public_id = outgoing_media_jobs.message_id AND m.state = 'queued')`, id, dbTime(time.Now()))
	if err != nil {
		return core.OutgoingMediaJob{}, err
	}
	if err := requireAffected(result); err != nil {
		return core.OutgoingMediaJob{}, err
	}
	var job core.OutgoingMediaJob
	err = tx.QueryRowContext(ctx, `SELECT m.public_id, m.account_id, m.chat_id, m.provider_message_id, m.sender_id,
m.kind, m.text, a.id, a.storage_profile_id, a.object_key, a.mime_type, a.file_name, a.stored_size, a.stored_sha256, j.attempts
FROM outgoing_media_jobs j JOIN messages m ON m.public_id = j.message_id JOIN attachments a ON a.message_id = m.public_id
WHERE j.message_id = ? AND a.part_index = 0`, id).
		Scan(&job.MessageID, &job.AccountID, &job.ChatID, &job.ProviderMessageID, &job.SenderID, &job.Kind, &job.Caption,
			&job.AttachmentID, &job.ProfileID, &job.ObjectKey, &job.MIMEType, &job.FileName, &job.Size, &job.SHA256, &job.Attempts)
	if err != nil {
		return core.OutgoingMediaJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return core.OutgoingMediaJob{}, err
	}
	return job, nil
}

func (s *Store) RetryMediaSend(ctx context.Context, id string, next time.Time, attempts int) error {
	result, err := s.db.ExecContext(ctx, `UPDATE outgoing_media_jobs SET phase = 'queued', next_attempt_at = ?, attempts = ?
WHERE message_id = ? AND phase = 'uploading'`, dbTime(next), attempts, id)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) BeginMediaSend(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE outgoing_media_jobs SET phase = 'sending' WHERE message_id = ? AND phase = 'uploading'`, id)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) CompleteMediaSend(ctx context.Context, id, state string, sent core.SentMessage) error {
	if state != "sent" && state != "failed" && state != "outcome_unknown" {
		return core.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE messages SET state = CASE WHEN state = 'sent' THEN 'sent' ELSE ? END,
sender_id = CASE WHEN ? = '' THEN sender_id ELSE ? END,
occurred_at = CASE WHEN ? THEN ? ELSE occurred_at END WHERE public_id = ? AND state IN ('queued', 'sent')`,
		state, sent.SenderID, sent.SenderID, !sent.Timestamp.IsZero(), dbTime(sent.Timestamp), id)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM outgoing_media_jobs WHERE message_id = ?`, id); err != nil {
		return err
	}
	var conversationID string
	if err := tx.QueryRowContext(ctx, `SELECT conversation_id FROM messages WHERE public_id = ?`, id).Scan(&conversationID); err != nil {
		return err
	}
	if err := refreshConversationTx(ctx, tx, conversationID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FailMediaSendsForAccount(ctx context.Context, accountID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE messages SET state = CASE
WHEN (SELECT phase FROM outgoing_media_jobs WHERE message_id = messages.public_id) = 'sending' THEN 'outcome_unknown'
ELSE 'failed' END
WHERE account_id = ? AND state = 'queued' AND EXISTS
(SELECT 1 FROM outgoing_media_jobs WHERE message_id = messages.public_id)`, accountID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM outgoing_media_jobs WHERE message_id IN
(SELECT public_id FROM messages WHERE account_id = ?)`, accountID); err != nil {
		return err
	}
	return tx.Commit()
}
