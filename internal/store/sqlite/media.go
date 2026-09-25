package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func (s *Store) GetMedia(ctx context.Context, attachmentID string) (core.MediaRecord, error) {
	var media core.MediaRecord
	var declared, stored sql.NullInt64
	var profile, key sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT a.id, m.account_id, m.chat_id, m.provider_message_id, m.direction, m.sender_id,
a.kind, a.mime_type, a.file_name, a.size, a.stored_size, a.availability, a.provider_ref,
a.storage_profile_id, a.object_key, a.media_version, a.attempt_count, a.failure_code, a.required_bytes
FROM attachments a JOIN messages m ON m.public_id = a.message_id WHERE a.id = ?`, attachmentID).
		Scan(&media.AttachmentID, &media.AccountID, &media.ChatID, &media.ProviderMessageID, &media.Direction,
			&media.SenderID, &media.Kind, &media.MIMEType, &media.FileName, &declared, &stored,
			&media.Availability, &media.ProviderRef, &profile, &key, &media.Version, &media.AttemptCount,
			&media.FailureCode, &media.RequiredBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return core.MediaRecord{}, core.ErrNotFound
	}
	if err != nil {
		return core.MediaRecord{}, err
	}
	if !declared.Valid || declared.Int64 < 0 || stored.Valid && stored.Int64 < 0 {
		return core.MediaRecord{}, fmt.Errorf("invalid stored media size")
	}
	media.DeclaredSize = uint64(declared.Int64)
	if stored.Valid {
		media.StoredSize = stored.Int64
	}
	media.StorageProfileID, media.ObjectKey = profile.String, key.String
	return media, nil
}

func (s *Store) ListPendingMedia(ctx context.Context, now time.Time, limit int) ([]string, error) {
	if limit < 1 || limit > 1000 {
		return nil, core.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM attachments WHERE auto_fetch = 1 AND availability = 'remote'
AND (next_attempt_at IS NULL OR next_attempt_at <= ?) ORDER BY id LIMIT ?`, dbTime(now), limit)
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

func (s *Store) StoredMediaBytes(ctx context.Context) (int64, error) {
	var total int64
	err := s.db.QueryRowContext(ctx, `SELECT
(SELECT COALESCE(SUM(stored_size), 0) FROM attachments WHERE availability = 'ready') +
(SELECT COALESCE(SUM(size), 0) FROM uploads) +
(SELECT COALESCE(SUM(size), 0) FROM avatars WHERE object_key IS NOT NULL)`).Scan(&total)
	return total, err
}

func (s *Store) MarkMediaReady(ctx context.Context, attachmentID, profileID, key string, size int64, sha256 []byte) error {
	if attachmentID == "" || profileID == "" || key == "" || size < 0 || len(sha256) != 32 {
		return core.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `UPDATE attachments SET availability = 'ready', storage_profile_id = ?, object_key = ?,
stored_size = ?, stored_sha256 = ?, auto_fetch = 0, next_attempt_at = NULL, attempt_count = 0,
failure_code = '', required_bytes = 0, media_version = media_version + 1
WHERE id = ? AND availability = 'remote'`, profileID, key, size, sha256, attachmentID)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) MarkMediaRemote(ctx context.Context, attachmentID string, version int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE attachments SET availability = 'remote', storage_profile_id = NULL,
object_key = NULL, stored_size = NULL, stored_sha256 = NULL, next_attempt_at = NULL,
failure_code = '', required_bytes = 0, media_version = media_version + 1
WHERE id = ? AND availability = 'ready' AND provider_ref IS NOT NULL AND media_version = ?`, attachmentID, version)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) MarkMediaUnavailable(ctx context.Context, attachmentID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE attachments SET availability = 'unavailable', auto_fetch = 0,
next_attempt_at = NULL, storage_profile_id = NULL, object_key = NULL, stored_size = NULL,
stored_sha256 = NULL, failure_code = '', required_bytes = 0
WHERE id = ? AND availability IN ('remote', 'ready')`, attachmentID)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) UpdateMediaRef(ctx context.Context, attachmentID string, ref []byte) error {
	if len(ref) == 0 {
		return core.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `UPDATE attachments SET provider_ref = ?, availability = 'remote' WHERE id = ? AND availability != 'ready'`, ref, attachmentID)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) ScheduleMediaRetry(ctx context.Context, attachmentID string, next time.Time, attempts int) error {
	if attempts < 0 {
		return core.ErrInvalid
	}
	var retryAt any
	autoFetch := 0
	if !next.IsZero() {
		retryAt = dbTime(next)
		autoFetch = 1
	}
	result, err := s.db.ExecContext(ctx, `UPDATE attachments SET next_attempt_at = ?, attempt_count = ?, auto_fetch = ? WHERE id = ? AND availability = 'remote'`,
		retryAt, attempts, autoFetch, attachmentID)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) BlockMedia(ctx context.Context, attachmentID, reason string, requiredBytes int64) error {
	if reason != "too_large" && reason != "quota" || requiredBytes < 1 {
		return core.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `UPDATE attachments SET auto_fetch = 0, next_attempt_at = NULL,
failure_code = ?, required_bytes = ? WHERE id = ? AND availability = 'remote'`, reason, requiredBytes, attachmentID)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) ClearMediaBlock(ctx context.Context, attachmentID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE attachments SET failure_code = '', required_bytes = 0
WHERE id = ? AND availability = 'remote'`, attachmentID)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) ListMediaOrphans(ctx context.Context, limit int) ([]core.MediaObject, error) {
	if limit < 1 || limit > 1000 {
		return nil, core.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT profile_id, object_key FROM media_orphans ORDER BY profile_id, object_key LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var objects []core.MediaObject
	for rows.Next() {
		var object core.MediaObject
		if err := rows.Scan(&object.ProfileID, &object.Key); err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	return objects, rows.Err()
}

func (s *Store) ClearMediaOrphan(ctx context.Context, object core.MediaObject) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM media_orphans WHERE profile_id = ? AND object_key = ?`, object.ProfileID, object.Key)
	return err
}
