package core

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const ProviderWhatsApp = "whatsapp"
const ConnectionKindLinkedDevice = "linked_device"

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
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrInvalid      = errors.New("invalid input")
	ErrNotConnected = errors.New("account is not connected")
	ErrIdempotency  = errors.New("idempotency key belongs to another request")
)

type Account struct {
	ID               string    `json:"id"`
	Provider         string    `json:"provider"`
	ConnectionKind   string    `json:"connection_kind"`
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
	ID                string          `json:"id"`
	AccountID         string          `json:"account_id"`
	ConversationID    string          `json:"conversation_id"`
	ChatID            string          `json:"-"`
	ProviderMessageID string          `json:"provider_message_id"`
	Direction         string          `json:"direction"`
	State             string          `json:"state"`
	SenderID          string          `json:"sender_id,omitempty"`
	Kind              MessageKind     `json:"kind"`
	Text              string          `json:"text,omitempty"`
	Content           json.RawMessage `json:"content,omitempty"`
	OccurredAt        time.Time       `json:"occurred_at"`
	IngestedAt        time.Time       `json:"ingested_at"`
}

type Conversation struct {
	ID             string    `json:"id"`
	AccountID      string    `json:"account_id"`
	ProviderChatID string    `json:"provider_chat_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastMessage    *Message  `json:"last_message,omitempty"`
}

type ConversationTarget struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type PageCursor struct {
	Time time.Time
	ID   string
}

type LoginChallenge struct {
	Type      string    `json:"type"`
	Value     string    `json:"value"`
	ExpiresAt time.Time `json:"expires_at"`
}

type LoginStatus struct {
	ID        string          `json:"id"`
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

type PreparedText struct {
	ChatID            string
	ProviderMessageID string
	SenderID          string
}

type Session interface {
	Connect() error
	Login(ctx context.Context, onChallenge func(LoginChallenge)) error
	PrepareText(recipient string) (PreparedText, error)
	SendText(ctx context.Context, prepared PreparedText, text string) (SentText, error)
	Identity() string
	Close()
}

type Connector interface {
	ResolveTarget(target ConversationTarget) (string, error)
	Open(ctx context.Context, identity string, emit func(Event)) (Session, error)
	New(emit func(Event)) (Session, error)
	Close() error
}

type Repository interface {
	CreateAccount(ctx context.Context, account Account) error
	ListAccounts(ctx context.Context) ([]Account, error)
	SetIdentity(ctx context.Context, id, identity string) error
	ClearIdentity(ctx context.Context, id string) error
	GetOrCreateConversation(ctx context.Context, accountID, providerChatID string) (Conversation, bool, error)
	GetConversation(ctx context.Context, id string) (Conversation, error)
	ListConversations(ctx context.Context, accountID string, before *PageCursor, limit int) ([]Conversation, error)
	SaveMessage(ctx context.Context, message Message) (Message, error)
	LookupSend(ctx context.Context, actorID, key, requestHash string) (Message, bool, error)
	ReserveSend(ctx context.Context, message Message, actorID, key, requestHash string) (Message, bool, error)
	CompleteSend(ctx context.Context, id, state string, sent SentText) (Message, error)
	GetMessage(ctx context.Context, id string) (Message, error)
	ListMessages(ctx context.Context, accountID string, before *PageCursor, limit int) ([]Message, error)
	ListConversationMessages(ctx context.Context, conversationID string, before *PageCursor, limit int) ([]Message, error)
	Close() error
}
