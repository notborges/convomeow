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
		_, err := tx.ExecContext(ctx, `INSERT INTO attachments(id, message_id, part_index, kind, mime_type, file_name, size, availability, provider_ref)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(message_id, part_index) DO UPDATE SET
mime_type = CASE WHEN excluded.mime_type != '' THEN excluded.mime_type ELSE attachments.mime_type END,
file_name = CASE WHEN excluded.file_name != '' THEN excluded.file_name ELSE attachments.file_name END,
size = CASE WHEN excluded.size != 0 THEN excluded.size ELSE attachments.size END,
availability = CASE WHEN attachments.availability = 'ready' THEN 'ready' WHEN excluded.availability = 'remote' THEN 'remote' ELSE attachments.availability END,
provider_ref = COALESCE(excluded.provider_ref, attachments.provider_ref)`,
			attachment.ID, messageID, attachment.Index, attachment.Kind, attachment.MIMEType,
			attachment.FileName, int64(attachment.Size), attachment.Availability, attachment.ProviderRef)
		if err != nil {
			return normalizeError(err)
		}
	}
	return nil
}

func mergeAttachmentsTx(ctx context.Context, tx *sql.Tx, targetID, sourceID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT part_index, kind, mime_type, file_name, size, availability, provider_ref FROM attachments WHERE message_id = ?`, sourceID)
	if err != nil {
		return err
	}
	var attachments []core.Attachment
	for rows.Next() {
		var a core.Attachment
		var size int64
		if err := rows.Scan(&a.Index, &a.Kind, &a.MIMEType, &a.FileName, &size, &a.Availability, &a.ProviderRef); err != nil {
			rows.Close()
			return err
		}
		if size < 0 {
			rows.Close()
			return fmt.Errorf("negative attachment size")
		}
		a.Size = uint64(size)
		attachments = append(attachments, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err := saveAttachmentsTx(ctx, tx, targetID, attachments); err != nil {
		return err
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
	rows, err := s.db.QueryContext(ctx, `SELECT id, message_id, part_index, kind, mime_type, file_name, size, availability FROM attachments WHERE message_id IN (`+strings.Join(marks, ",")+`) ORDER BY message_id, part_index`, args...)
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
