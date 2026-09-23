package sqlite

import (
	"context"
	"fmt"
)

const schemaVersion = 5

func (s *Store) initSchema(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version == schemaVersion {
		return nil
	}
	if version != 0 {
		return fmt.Errorf("unsupported app database schema version %d; use a fresh data directory", version)
	}
	var existingTables int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`).Scan(&existingTables); err != nil {
		return err
	}
	if existingTables != 0 {
		return fmt.Errorf("unsupported app database schema; use a fresh data directory")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const schema = `
CREATE TABLE accounts (
  id TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  connection_kind TEXT NOT NULL,
  label TEXT NOT NULL,
  provider_identity TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX accounts_provider_label ON accounts(provider, lower(label));
CREATE UNIQUE INDEX accounts_provider_identity ON accounts(provider, provider_identity) WHERE provider_identity IS NOT NULL;
CREATE TABLE conversations (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id),
  provider_chat_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_message_id TEXT,
  UNIQUE(account_id, provider_chat_id)
);
CREATE INDEX conversations_recent ON conversations(updated_at DESC, id DESC);
CREATE TABLE conversation_aliases (
  account_id TEXT NOT NULL REFERENCES accounts(id),
  jid TEXT NOT NULL,
  conversation_id TEXT NOT NULL REFERENCES conversations(id),
  PRIMARY KEY(account_id, jid)
);
CREATE INDEX conversation_aliases_conversation ON conversation_aliases(conversation_id);
CREATE TABLE jid_links (
  account_id TEXT NOT NULL REFERENCES accounts(id),
  jid TEXT NOT NULL,
  peer_jid TEXT NOT NULL,
  PRIMARY KEY(account_id, jid, peer_jid)
);
CREATE TABLE messages (
  public_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id),
  conversation_id TEXT NOT NULL REFERENCES conversations(id),
  chat_id TEXT NOT NULL,
  provider_message_id TEXT NOT NULL,
  direction TEXT NOT NULL,
  state TEXT NOT NULL,
  sender_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL DEFAULT 'text',
  text TEXT NOT NULL,
  content_json TEXT,
  occurred_at TEXT NOT NULL,
  ingested_at TEXT NOT NULL,
  UNIQUE(account_id, conversation_id, provider_message_id)
);
CREATE INDEX messages_account_recent ON messages(account_id, occurred_at DESC, public_id DESC);
CREATE INDEX messages_conversation_recent ON messages(conversation_id, occurred_at DESC, public_id DESC);
CREATE INDEX messages_outbound_provider ON messages(account_id, provider_message_id) WHERE direction = 'outbound';
CREATE TABLE attachments (
  id TEXT PRIMARY KEY,
  message_id TEXT NOT NULL REFERENCES messages(public_id),
  part_index INTEGER NOT NULL,
  kind TEXT NOT NULL,
  mime_type TEXT NOT NULL DEFAULT '',
  file_name TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL DEFAULT 0,
  availability TEXT NOT NULL,
  provider_ref BLOB,
  UNIQUE(message_id, part_index)
);
CREATE INDEX attachments_message ON attachments(message_id, part_index);
CREATE TABLE send_keys (
  actor_id TEXT NOT NULL,
  key TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  message_id TEXT NOT NULL REFERENCES messages(public_id),
  PRIMARY KEY(actor_id, key)
);
`
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = 5`); err != nil {
		return err
	}
	return tx.Commit()
}
