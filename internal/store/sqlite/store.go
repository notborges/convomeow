package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/notborges/convomeow/internal/core"
)

type Store struct {
	db *sql.DB
}

func dbTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func DSN(path string) string {
	u := &url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("_foreign_keys", "on")
	q.Set("_busy_timeout", "5000")
	q.Set("_journal_mode", "WAL")
	u.RawQuery = q.Encode()
	return u.String()
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite3", DSN(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.initSchema(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.recoverSends(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) recoverSends(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE messages SET state = 'outcome_unknown' WHERE state = 'queued' AND (
NOT EXISTS (SELECT 1 FROM outgoing_media_jobs j WHERE j.message_id = messages.public_id) OR
EXISTS (SELECT 1 FROM outgoing_media_jobs j WHERE j.message_id = messages.public_id AND j.phase = 'sending'))`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM outgoing_media_jobs WHERE phase = 'sending' OR
EXISTS (SELECT 1 FROM messages m WHERE m.public_id = outgoing_media_jobs.message_id AND m.state != 'queued')`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE outgoing_media_jobs SET phase = 'queued' WHERE phase = 'uploading'`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) CreateAccount(ctx context.Context, a core.Account) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO accounts(id, provider, connection_kind, label, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`,
		a.ID, a.Provider, a.ConnectionKind, a.Label, a.CreatedAt.UTC().Format(time.RFC3339Nano), a.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return normalizeError(err)
}

func scanAccount(row interface{ Scan(...any) error }) (core.Account, error) {
	var a core.Account
	var identity sql.NullString
	var created, updated string
	err := row.Scan(&a.ID, &a.Provider, &a.ConnectionKind, &a.Label, &identity, &created, &updated)
	if err != nil {
		return a, err
	}
	a.ProviderIdentity = identity.String
	a.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return a, fmt.Errorf("parse account created_at: %w", err)
	}
	a.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return a, fmt.Errorf("parse account updated_at: %w", err)
	}
	return a, nil
}

const accountColumns = `id, provider, connection_kind, label, provider_identity, created_at, updated_at`

func (s *Store) ListAccounts(ctx context.Context) ([]core.Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+accountColumns+` FROM accounts ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := make([]core.Account, 0)
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

func (s *Store) SetIdentity(ctx context.Context, id, identity string) error {
	if identity == "" {
		return core.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `UPDATE accounts SET provider_identity = ?, updated_at = ? WHERE id = ?`,
		identity, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return normalizeError(err)
	}
	return requireAffected(result)
}

func (s *Store) ClearIdentity(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE accounts SET provider_identity = NULL, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func requireAffected(result sql.Result) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return core.ErrNotFound
	}
	return nil
}

func normalizeError(err error) error {
	if err == nil {
		return nil
	}
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint {
		return fmt.Errorf("%w: %v", core.ErrConflict, err)
	}
	return err
}
