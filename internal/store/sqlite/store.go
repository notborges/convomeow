package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
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
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > 3 {
		return fmt.Errorf("app database schema version %d is newer than this binary", version)
	}
	if version == 3 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if version == 0 {
		const schema = `
CREATE TABLE IF NOT EXISTS accounts (
  id TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  label TEXT NOT NULL,
  provider_identity TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS accounts_provider_label ON accounts(provider, lower(label));
CREATE UNIQUE INDEX IF NOT EXISTS accounts_provider_identity ON accounts(provider, provider_identity) WHERE provider_identity IS NOT NULL;
CREATE TABLE IF NOT EXISTS messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id TEXT NOT NULL REFERENCES accounts(id),
  chat_id TEXT NOT NULL,
  provider_message_id TEXT NOT NULL,
  direction TEXT NOT NULL,
  sender_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL DEFAULT 'text',
  text TEXT NOT NULL,
  content_json TEXT,
  occurred_at TEXT NOT NULL,
  ingested_at TEXT NOT NULL,
  UNIQUE(account_id, chat_id, provider_message_id)
);
CREATE INDEX IF NOT EXISTS messages_account_cursor ON messages(account_id, id);
`
		if _, err := tx.ExecContext(ctx, schema); err != nil {
			return err
		}
	} else if version == 1 {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE messages ADD COLUMN kind TEXT NOT NULL DEFAULT 'text'`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE messages ADD COLUMN content_json TEXT`); err != nil {
			return err
		}
	}
	const chatsSchema = `
CREATE TABLE chats (
  account_id TEXT NOT NULL REFERENCES accounts(id),
  id TEXT NOT NULL,
  last_message_id INTEGER NOT NULL REFERENCES messages(id),
  PRIMARY KEY(account_id, id)
);
CREATE INDEX chats_account_recent ON chats(account_id, last_message_id DESC);
CREATE INDEX messages_account_chat_recent ON messages(account_id, chat_id, id DESC);
INSERT INTO chats(account_id, id, last_message_id)
SELECT account_id, chat_id, MAX(id) FROM messages GROUP BY account_id, chat_id;
`
	if _, err := tx.ExecContext(ctx, chatsSchema); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = 3`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateAccount(ctx context.Context, a core.Account) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO accounts(id, provider, label, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
		a.ID, a.Provider, a.Label, a.CreatedAt.UTC().Format(time.RFC3339Nano), a.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return normalizeError(err)
}

func scanAccount(row interface{ Scan(...any) error }) (core.Account, error) {
	var a core.Account
	var identity sql.NullString
	var created, updated string
	err := row.Scan(&a.ID, &a.Provider, &a.Label, &identity, &created, &updated)
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

const accountColumns = `id, provider, label, provider_identity, created_at, updated_at`

func (s *Store) ListAccounts(ctx context.Context) ([]core.Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+accountColumns+` FROM accounts ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []core.Account
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

func (s *Store) SaveMessage(ctx context.Context, m core.Message) (core.Message, error) {
	if m.AccountID == "" || m.ChatID == "" || m.ProviderMessageID == "" || m.Direction == "" {
		return m, core.ErrInvalid
	}
	if m.Kind == "" {
		m.Kind = core.MessageKindText
	}
	var content any
	if len(m.Content) > 0 {
		if !json.Valid(m.Content) {
			return m, fmt.Errorf("%w: message content must be valid JSON", core.ErrInvalid)
		}
		content = string(m.Content)
	}
	if m.IngestedAt.IsZero() {
		m.IngestedAt = time.Now().UTC()
	}
	if m.OccurredAt.IsZero() {
		m.OccurredAt = m.IngestedAt
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return m, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO messages(account_id, chat_id, provider_message_id, direction, sender_id, kind, text, content_json, occurred_at, ingested_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(account_id, chat_id, provider_message_id) DO NOTHING`,
		m.AccountID, m.ChatID, m.ProviderMessageID, m.Direction, m.SenderID, m.Kind, m.Text, content,
		m.OccurredAt.UTC().Format(time.RFC3339Nano), m.IngestedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return m, normalizeError(err)
	}
	saved, err := scanMessage(tx.QueryRowContext(ctx, `SELECT `+messageColumns+`
FROM messages WHERE account_id = ? AND chat_id = ? AND provider_message_id = ?`, m.AccountID, m.ChatID, m.ProviderMessageID))
	if err != nil {
		return m, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO chats(account_id, id, last_message_id) VALUES(?, ?, ?)
ON CONFLICT(account_id, id) DO UPDATE SET last_message_id = excluded.last_message_id
WHERE excluded.last_message_id > chats.last_message_id`, saved.AccountID, saved.ChatID, saved.ID)
	if err != nil {
		return m, err
	}
	if err := tx.Commit(); err != nil {
		return m, err
	}
	return saved, nil
}

const messageColumns = `id, account_id, chat_id, provider_message_id, direction, sender_id, kind, text, content_json, occurred_at, ingested_at`

func (s *Store) ListMessages(ctx context.Context, accountID string, after int64, limit int) ([]core.Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+messageColumns+`
FROM messages WHERE account_id = ? AND id > ? ORDER BY id LIMIT ?`, accountID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]core.Message, 0)
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

func (s *Store) ListChats(ctx context.Context, accountID string, before int64, limit int) ([]core.Chat, error) {
	query := `SELECT m.id, m.account_id, m.chat_id, m.provider_message_id, m.direction, m.sender_id, m.kind, m.text, m.content_json, m.occurred_at, m.ingested_at
FROM chats AS c JOIN messages AS m ON m.id = c.last_message_id WHERE c.account_id = ?`
	args := []any{accountID}
	if before > 0 {
		query += ` AND c.last_message_id < ?`
		args = append(args, before)
	}
	query += ` ORDER BY c.last_message_id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	chats := make([]core.Chat, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		chats = append(chats, core.Chat{AccountID: message.AccountID, ID: message.ChatID, LastMessage: message})
	}
	return chats, rows.Err()
}

func (s *Store) ListChatMessages(ctx context.Context, accountID, chatID string, before int64, limit int) ([]core.Message, error) {
	query := `SELECT ` + messageColumns + ` FROM messages WHERE account_id = ? AND chat_id = ?`
	args := []any{accountID, chatID}
	if before > 0 {
		query += ` AND id < ?`
		args = append(args, before)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]core.Message, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func scanMessage(row interface{ Scan(...any) error }) (core.Message, error) {
	var m core.Message
	var occurred, ingested, kind string
	var content sql.NullString
	if err := row.Scan(&m.ID, &m.AccountID, &m.ChatID, &m.ProviderMessageID, &m.Direction, &m.SenderID, &kind, &m.Text, &content, &occurred, &ingested); err != nil {
		return m, err
	}
	m.Kind = core.MessageKind(kind)
	if content.Valid {
		m.Content = json.RawMessage(content.String)
	}
	var err error
	m.OccurredAt, err = time.Parse(time.RFC3339Nano, occurred)
	if err != nil {
		return m, err
	}
	m.IngestedAt, err = time.Parse(time.RFC3339Nano, ingested)
	return m, err
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
