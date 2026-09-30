package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/core"
	"github.com/notborges/convomeow/internal/notifications"
)

func (s *Store) initNotifications(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS browser_subscriptions (
id TEXT PRIMARY KEY, endpoint TEXT NOT NULL UNIQUE, p256dh TEXT NOT NULL, auth TEXT NOT NULL,
session_id TEXT NOT NULL, generation TEXT NOT NULL, session_expires TEXT NOT NULL,
locale TEXT NOT NULL, preview INTEGER NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS notification_jobs (
subscription_id TEXT NOT NULL REFERENCES browser_subscriptions(id) ON DELETE CASCADE,
message_id TEXT NOT NULL REFERENCES messages(public_id) ON DELETE CASCADE,
attempts INTEGER NOT NULL DEFAULT 0, available_at TEXT NOT NULL, expires_at TEXT NOT NULL,
PRIMARY KEY(subscription_id, message_id));
CREATE INDEX IF NOT EXISTS notification_jobs_due ON notification_jobs(available_at);
CREATE TABLE IF NOT EXISTS notification_revocations (session_id TEXT PRIMARY KEY, expires_at TEXT NOT NULL);`)
	return err
}

func enqueueNotificationsTx(ctx context.Context, tx *sql.Tx, messageID, generation string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO notification_jobs(subscription_id, message_id, available_at, expires_at)
SELECT id, ?, ?, ? FROM browser_subscriptions WHERE generation = ? AND session_expires > ?
ON CONFLICT(subscription_id, message_id) DO NOTHING`, messageID, dbTime(now), dbTime(now.Add(notifications.Lifetime)), generation, dbTime(now))
	return err
}

const subscriptionColumns = `id, endpoint, p256dh, auth, session_id, generation, session_expires, locale, preview`

func scanSubscription(row interface{ Scan(...any) error }) (notifications.Subscription, error) {
	var s notifications.Subscription
	var expires string
	err := row.Scan(&s.ID, &s.Endpoint, &s.Keys.P256DH, &s.Keys.Auth, &s.Session.ID, &s.Session.Generation, &expires, &s.Locale, &s.Preview)
	if errors.Is(err, sql.ErrNoRows) {
		return s, core.ErrNotFound
	}
	if err != nil {
		return s, err
	}
	s.Session.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	return s, err
}

func (s *Store) SaveSubscription(ctx context.Context, sub notifications.Subscription) (notifications.Subscription, error) {
	if sub.Session.ID == "" || sub.Session.Generation == "" || !sub.Session.ExpiresAt.After(time.Now()) {
		return sub, core.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sub, err
	}
	defer tx.Rollback()
	var revoked bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_revocations WHERE session_id = ?)`, sub.Session.ID).Scan(&revoked); err != nil {
		return sub, err
	}
	if revoked {
		return sub, core.ErrInvalid
	}
	existing, err := scanSubscription(tx.QueryRowContext(ctx, `SELECT `+subscriptionColumns+` FROM browser_subscriptions WHERE endpoint = ?`, sub.Endpoint))
	if err == nil {
		if existing.Keys != sub.Keys || existing.Session.Generation != sub.Session.Generation {
			return sub, core.ErrInvalid
		}
		sub.ID = existing.ID
		if existing.Session.ID != sub.Session.ID {
			if _, err := tx.ExecContext(ctx, `DELETE FROM notification_jobs WHERE subscription_id = ?`, sub.ID); err != nil {
				return sub, err
			}
		}
	} else if errors.Is(err, core.ErrNotFound) {
		sub.ID = uuid.NewString()
	} else {
		return sub, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO browser_subscriptions(`+subscriptionColumns+`, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(endpoint) DO UPDATE SET session_id = excluded.session_id, session_expires = excluded.session_expires,
locale = excluded.locale, preview = excluded.preview, updated_at = excluded.updated_at`, sub.ID, sub.Endpoint, sub.Keys.P256DH, sub.Keys.Auth,
		sub.Session.ID, sub.Session.Generation, dbTime(sub.Session.ExpiresAt), sub.Locale, sub.Preview, dbTime(time.Now()))
	if err != nil {
		return sub, err
	}
	return sub, tx.Commit()
}

func (s *Store) GetSubscription(ctx context.Context, id string, owner notifications.Session) (notifications.Subscription, error) {
	return scanSubscription(s.db.QueryRowContext(ctx, `SELECT `+subscriptionColumns+` FROM browser_subscriptions WHERE id = ? AND session_id = ? AND generation = ? AND session_expires > ?`,
		id, owner.ID, owner.Generation, dbTime(time.Now())))
}

func (s *Store) DeleteSubscription(ctx context.Context, id string, owner notifications.Session) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM browser_subscriptions WHERE id = ? AND session_id = ? AND generation = ?`, id, owner.ID, owner.Generation)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return core.ErrNotFound
	}
	return err
}

func (s *Store) DeleteNotificationSession(ctx context.Context, owner notifications.Session) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// An already authorized registration can finish after logout clears the cookie.
	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_revocations(session_id, expires_at) VALUES(?, ?) ON CONFLICT(session_id) DO NOTHING`, owner.ID, dbTime(owner.ExpiresAt)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM browser_subscriptions WHERE session_id = ?`, owner.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ClaimNotification(ctx context.Context, generation string, now time.Time) (*notifications.Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM notification_revocations WHERE expires_at <= ?`, dbTime(now)); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM browser_subscriptions WHERE generation != ? OR session_expires <= ?`, generation, dbTime(now)); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM notification_jobs WHERE expires_at <= ? OR attempts >= 5`, dbTime(now)); err != nil {
		return nil, err
	}
	var job notifications.Job
	var subID, expires string
	err = tx.QueryRowContext(ctx, `SELECT subscription_id, message_id, attempts, expires_at FROM notification_jobs WHERE available_at <= ? ORDER BY available_at LIMIT 1`, dbTime(now)).Scan(&subID, &job.MessageID, &job.Attempts, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	job.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return nil, err
	}
	job.Subscription, err = scanSubscription(tx.QueryRowContext(ctx, `SELECT `+subscriptionColumns+` FROM browser_subscriptions WHERE id = ?`, subID))
	if err != nil {
		return nil, err
	}
	job.Attempts++
	if _, err := tx.ExecContext(ctx, `UPDATE notification_jobs SET attempts = ?, available_at = ? WHERE subscription_id = ? AND message_id = ?`, job.Attempts, dbTime(now.Add(30*time.Second)), subID, job.MessageID); err != nil {
		return nil, err
	}
	return &job, tx.Commit()
}

func (s *Store) FinishNotification(ctx context.Context, job notifications.Job, next time.Time) error {
	if next.IsZero() {
		_, err := s.db.ExecContext(ctx, `DELETE FROM notification_jobs WHERE subscription_id = ? AND message_id = ?`, job.Subscription.ID, job.MessageID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE notification_jobs SET available_at = ? WHERE subscription_id = ? AND message_id = ? AND attempts = ?`,
		dbTime(next), job.Subscription.ID, job.MessageID, job.Attempts)
	return err
}
