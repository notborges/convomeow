package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func (s *Store) GetAvatar(ctx context.Context, accountID, providerID string) (core.AvatarRecord, error) {
	var record core.AvatarRecord
	var profile, key sql.NullString
	var checked string
	err := s.db.QueryRowContext(ctx, `SELECT account_id, provider_id, picture_id, storage_profile_id, object_key,
content_type, size, checked_at FROM avatars WHERE account_id = ? AND provider_id = ?`, accountID, providerID).
		Scan(&record.AccountID, &record.ProviderID, &record.PictureID, &profile, &key,
			&record.ContentType, &record.Size, &checked)
	if errors.Is(err, sql.ErrNoRows) {
		return core.AvatarRecord{}, core.ErrNotFound
	}
	if err != nil {
		return core.AvatarRecord{}, err
	}
	record.ProfileID, record.ObjectKey = profile.String, key.String
	record.CheckedAt, err = time.Parse(time.RFC3339Nano, checked)
	if err != nil {
		return core.AvatarRecord{}, fmt.Errorf("parse avatar checked_at: %w", err)
	}
	return record, nil
}

func (s *Store) SaveAvatar(ctx context.Context, record core.AvatarRecord) error {
	if record.AccountID == "" || record.ProviderID == "" || record.CheckedAt.IsZero() ||
		(record.ObjectKey == "" && (record.ProfileID != "" || record.ContentType != "" || record.Size != 0 || record.PictureID != "")) ||
		(record.ObjectKey != "" && (record.ProfileID == "" || record.ContentType == "" || record.Size < 1)) {
		return core.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previousProfile, previousKey sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT storage_profile_id, object_key FROM avatars WHERE account_id = ? AND provider_id = ?`,
		record.AccountID, record.ProviderID).Scan(&previousProfile, &previousKey)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if previousKey.Valid && previousKey.String != record.ObjectKey {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO media_orphans(profile_id, object_key) VALUES(?, ?)`,
			previousProfile.String, previousKey.String); err != nil {
			return err
		}
	}
	var profile, key any
	if record.ObjectKey != "" {
		profile, key = record.ProfileID, record.ObjectKey
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO avatars(account_id, provider_id, picture_id, storage_profile_id, object_key,
content_type, size, checked_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(account_id, provider_id) DO UPDATE SET picture_id = excluded.picture_id,
storage_profile_id = excluded.storage_profile_id, object_key = excluded.object_key,
content_type = excluded.content_type, size = excluded.size, checked_at = excluded.checked_at`,
		record.AccountID, record.ProviderID, record.PictureID, profile, key,
		record.ContentType, record.Size, dbTime(record.CheckedAt))
	if err != nil {
		return normalizeError(err)
	}
	return tx.Commit()
}

func (s *Store) TouchAvatar(ctx context.Context, accountID, providerID string, checkedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE avatars SET checked_at = ? WHERE account_id = ? AND provider_id = ?`,
		dbTime(checkedAt), accountID, providerID)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) ClearAccountAvatars(ctx context.Context, accountID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO media_orphans(profile_id, object_key)
SELECT storage_profile_id, object_key FROM avatars WHERE account_id = ? AND object_key IS NOT NULL`, accountID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM avatars WHERE account_id = ?`, accountID); err != nil {
		return err
	}
	return tx.Commit()
}
