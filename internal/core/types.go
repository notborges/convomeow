package core

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const ProviderWhatsApp = "whatsapp"

type MessageKind string

const (
	MessageKindText     MessageKind = "text"
	MessageKindImage    MessageKind = "image"
	MessageKindVideo    MessageKind = "video"
	MessageKindAudio    MessageKind = "audio"
	MessageKindDocument MessageKind = "document"
	MessageKindSticker  MessageKind = "sticker"
	MessageKindLocation MessageKind = "location"
	MessageKindContact  MessageKind = "contact"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrInvalid        = errors.New("invalid input")
	ErrNotConnected   = errors.New("account is not connected")
	ErrOutcomeUnknown = errors.New("send outcome is unknown")
	ErrSentUnrecorded = errors.New("message sent but could not be recorded")
)

type Account struct {
	ID               string    `json:"id"`
	Provider         string    `json:"provider"`
	Label            string    `json:"label"`
	ProviderIdentity string    `json:"provider_identity,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type AccountStatus struct {
	Account
	State     string `json:"state"`
	LastError string `json:"last_error,omitempty"`
}

type Message struct {
	ID                int64           `json:"id"`
	AccountID         string          `json:"account_id"`
	ChatID            string          `json:"chat_id"`
	ProviderMessageID string          `json:"provider_message_id"`
	Direction         string          `json:"direction"`
	SenderID          string          `json:"sender_id,omitempty"`
	Kind              MessageKind     `json:"kind"`
	Text              string          `json:"text,omitempty"`
	Content           json.RawMessage `json:"content,omitempty"`
	OccurredAt        time.Time       `json:"occurred_at"`
	IngestedAt        time.Time       `json:"ingested_at"`
}

type Chat struct {
	AccountID   string  `json:"account_id"`
	ID          string  `json:"id"`
	LastMessage Message `json:"last_message"`
}

type LoginChallenge struct {
	Type      string    `json:"type"`
	Value     string    `json:"value"`
	ExpiresAt time.Time `json:"expires_at"`
}

type LoginStatus struct {
	State     string          `json:"state"`
	Challenge *LoginChallenge `json:"challenge,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type EventType string

const (
	EventConnected    EventType = "connected"
	EventDisconnected EventType = "disconnected"
	EventLoggedOut    EventType = "logged_out"
	EventPaired       EventType = "paired"
	EventMessage      EventType = "message"
	EventError        EventType = "error"
)

type Event struct {
	Type     EventType
	Identity string
	Message  *Message
	Err      error
}

type SentText struct {
	ChatID            string
	ProviderMessageID string
	SenderID          string
	Timestamp         time.Time
}

type Session interface {
	Connect() error
	Login(ctx context.Context, onChallenge func(LoginChallenge)) error
	SendText(ctx context.Context, recipient, text string) (SentText, error)
	Identity() string
	Close()
}

type Connector interface {
	Open(ctx context.Context, identity string, emit func(Event)) (Session, error)
	New(emit func(Event)) (Session, error)
	Close() error
}

type Repository interface {
	CreateAccount(ctx context.Context, account Account) error
	ListAccounts(ctx context.Context) ([]Account, error)
	SetIdentity(ctx context.Context, id, identity string) error
	ClearIdentity(ctx context.Context, id string) error
	SaveMessage(ctx context.Context, message Message) (Message, error)
	ListMessages(ctx context.Context, accountID string, after int64, limit int) ([]Message, error)
	ListChats(ctx context.Context, accountID string, before int64, limit int) ([]Chat, error)
	ListChatMessages(ctx context.Context, accountID, chatID string, before int64, limit int) ([]Message, error)
	Close() error
}
