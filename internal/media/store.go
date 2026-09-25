package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("media object not found")

type Store interface {
	Put(ctx context.Context, key string, file *os.File, size int64) error
	Open(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

type Registry struct {
	active string
	stores map[string]Store
}

func NewRegistry(active string, stores map[string]Store) (*Registry, error) {
	if stores[active] == nil {
		return nil, fmt.Errorf("active media profile %q is unavailable", active)
	}
	return &Registry{active: active, stores: stores}, nil
}

func (r *Registry) Active() (string, Store) {
	return r.active, r.stores[r.active]
}

func (r *Registry) Get(id string) (Store, bool) {
	store, ok := r.stores[id]
	return store, ok && store != nil
}

func Key(accountID, attachmentID string) (string, error) {
	if _, err := uuid.Parse(accountID); err != nil {
		return "", fmt.Errorf("invalid account ID: %w", err)
	}
	if _, err := uuid.Parse(attachmentID); err != nil {
		return "", fmt.Errorf("invalid attachment ID: %w", err)
	}
	return "accounts/" + accountID + "/attachments/" + attachmentID, nil
}

func AvatarKey(accountID, avatarID string) (string, error) {
	if _, err := uuid.Parse(accountID); err != nil {
		return "", fmt.Errorf("invalid account ID: %w", err)
	}
	if _, err := uuid.Parse(avatarID); err != nil {
		return "", fmt.Errorf("invalid avatar ID: %w", err)
	}
	return "accounts/" + accountID + "/avatars/" + avatarID, nil
}

func validKey(key string) bool {
	parts := strings.Split(key, "/")
	if len(parts) != 4 || parts[0] != "accounts" || (parts[2] != "attachments" && parts[2] != "avatars") {
		return false
	}
	_, accountErr := uuid.Parse(parts[1])
	_, attachmentErr := uuid.Parse(parts[3])
	return accountErr == nil && attachmentErr == nil
}
