package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/core"
)

func saveAttachmentsTx(ctx context.Context, tx *sql.Tx, messageID string, attachments []core.Attachment) error {
	for _, attachment := range attachments {
		if attachment.Index < 0 || attachment.Kind == "" || attachment.Size > math.MaxInt64 {
			return core.ErrInvalid
		}
		if attachment.ID == "" {
			attachment.ID = uuid.NewString()
		}
		if attachment.Availability == "" {
			attachment.Availability = "remote"
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO attachments(id, message_id, part_index, kind, mime_type, file_name, size, availability, provider_ref, auto_fetch)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(message_id, part_index) DO UPDATE SET
mime_type = CASE WHEN excluded.mime_type != '' THEN excluded.mime_type ELSE attachments.mime_type END,
file_name = CASE WHEN excluded.file_name != '' THEN excluded.file_name ELSE attachments.file_name END,
size = CASE WHEN excluded.size != 0 THEN excluded.size ELSE attachments.size END,
availability = CASE WHEN attachments.availability = 'ready' THEN 'ready' WHEN excluded.availability = 'remote' THEN 'remote' ELSE attachments.availability END,
provider_ref = COALESCE(excluded.provider_ref, attachments.provider_ref),
auto_fetch = CASE WHEN attachments.failure_code != '' THEN 0 ELSE max(attachments.auto_fetch, excluded.auto_fetch) END`,
			attachment.ID, messageID, attachment.Index, attachment.Kind, attachment.MIMEType,
			attachment.FileName, int64(attachment.Size), attachment.Availability, attachment.ProviderRef, attachment.AutoFetch)
		if err != nil {
			return normalizeError(err)
		}
	}
	return nil
}

func mergeAttachmentsTx(ctx context.Context, tx *sql.Tx, targetID, sourceID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT part_index, kind, mime_type, file_name, size, availability, provider_ref, auto_fetch,
storage_profile_id, object_key, stored_size, stored_sha256 FROM attachments WHERE message_id = ?`, sourceID)
	if err != nil {
		return err
	}
	type mergedAttachment struct {
		attachment core.Attachment
		profileID  sql.NullString
		objectKey  sql.NullString
		storedSize sql.NullInt64
		sha256     []byte
	}
	var attachments []mergedAttachment
	for rows.Next() {
		var a mergedAttachment
		var size int64
		if err := rows.Scan(&a.attachment.Index, &a.attachment.Kind, &a.attachment.MIMEType, &a.attachment.FileName,
			&size, &a.attachment.Availability, &a.attachment.ProviderRef, &a.attachment.AutoFetch,
			&a.profileID, &a.objectKey, &a.storedSize, &a.sha256); err != nil {
			rows.Close()
			return err
		}
		if size < 0 {
			rows.Close()
			return fmt.Errorf("negative attachment size")
		}
		a.attachment.Size = uint64(size)
		attachments = append(attachments, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, source := range attachments {
		if err := saveAttachmentsTx(ctx, tx, targetID, []core.Attachment{source.attachment}); err != nil {
			return err
		}
		if source.attachment.Availability != "ready" {
			continue
		}
		var targetIDForPart, targetState string
		var targetProfile, targetKey sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT id, availability, storage_profile_id, object_key FROM attachments WHERE message_id = ? AND part_index = ?`,
			targetID, source.attachment.Index).Scan(&targetIDForPart, &targetState, &targetProfile, &targetKey)
		if err != nil {
			return err
		}
		if targetState != "ready" || !targetProfile.Valid || !targetKey.Valid {
			_, err = tx.ExecContext(ctx, `UPDATE attachments SET availability = 'ready', storage_profile_id = ?, object_key = ?, stored_size = ?, stored_sha256 = ? WHERE id = ?`,
				source.profileID, source.objectKey, source.storedSize, source.sha256, targetIDForPart)
			if err != nil {
				return err
			}
		} else if source.profileID.Valid && source.objectKey.Valid &&
			(targetProfile.String != source.profileID.String || targetKey.String != source.objectKey.String) {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO media_orphans(profile_id, object_key) VALUES(?, ?)`,
				source.profileID.String, source.objectKey.String); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM attachments WHERE message_id = ?`, sourceID)
	return err
}

func (s *Store) attachToMessages(ctx context.Context, messages []core.Message) error {
	if len(messages) == 0 {
		return nil
	}
	indexes := make(map[string]int, len(messages))
	args := make([]any, 0, len(messages))
	marks := make([]string, 0, len(messages))
	for i := range messages {
		indexes[messages[i].ID] = i
		args = append(args, messages[i].ID)
		marks = append(marks, "?")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, message_id, part_index, kind, mime_type, file_name,
CASE WHEN availability = 'ready' AND stored_size IS NOT NULL THEN stored_size ELSE size END, availability
FROM attachments WHERE message_id IN (`+strings.Join(marks, ",")+`) ORDER BY message_id, part_index`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a core.Attachment
		var messageID string
		var size int64
		if err := rows.Scan(&a.ID, &messageID, &a.Index, &a.Kind, &a.MIMEType, &a.FileName, &size, &a.Availability); err != nil {
			return err
		}
		if size < 0 {
			return fmt.Errorf("negative attachment size")
		}
		a.Size = uint64(size)
		messages[indexes[messageID]].Attachments = append(messages[indexes[messageID]].Attachments, a)
	}
	return rows.Err()
}
