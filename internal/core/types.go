package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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
	ErrNotFound         = errors.New("not found")
	ErrConflict         = errors.New("conflict")
	ErrInvalid          = errors.New("invalid input")
	ErrNotConnected     = errors.New("account is not connected")
	ErrIdempotency      = errors.New("idempotency key belongs to another request")
	ErrMediaUnavailable = errors.New("media is unavailable")
	ErrMediaTooLarge    = errors.New("media exceeds the configured size limit")
	ErrMediaQuota       = errors.New("media storage limit reached")
	ErrMediaStorage     = errors.New("media storage is unavailable")
	ErrMediaBusy        = errors.New("media download queue is full")
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
	Attachments       []Attachment    `json:"attachments,omitempty"`
	OccurredAt        time.Time       `json:"occurred_at"`
	IngestedAt        time.Time       `json:"ingested_at"`
}

type Attachment struct {
	ID           string      `json:"id"`
	Index        int         `json:"-"`
	Kind         MessageKind `json:"kind"`
	MIMEType     string      `json:"mime_type,omitempty"`
	FileName     string      `json:"file_name,omitempty"`
	Size         uint64      `json:"size,omitempty"`
	Availability string      `json:"availability"`
	ProviderRef  []byte      `json:"-"`
	AutoFetch    bool        `json:"-"`
}

type MediaRecord struct {
	AttachmentID      string
	AccountID         string
	ChatID            string
	ProviderMessageID string
	Direction         string
	SenderID          string
	Kind              MessageKind
	MIMEType          string
	FileName          string
	DeclaredSize      uint64
	StoredSize        int64
	Availability      string
	ProviderRef       []byte `json:"-"`
	StorageProfileID  string `json:"-"`
	ObjectKey         string `json:"-"`
	Version           int64
	AttemptCount      int
	FailureCode       string
	RequiredBytes     int64
}

type MediaSource struct {
	ChatID            string
	ProviderMessageID string
	Direction         string
	SenderID          string
	ProviderRef       []byte `json:"-"`
}

type ChatLink struct {
	First  string
	Second string
}

type HistoryChat struct {
	ID             string
	Aliases        []string
	PreferredID    string
	LastActivityAt time.Time
}

type HistoryBatch struct {
	AccountID string
	Chat      *HistoryChat
	Messages  []Message
	Links     []ChatLink
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
	EventHistory      EventType = "history"
	EventError        EventType = "error"
)

type Event struct {
	Type     EventType
	Identity string
	Message  *Message
	History  *HistoryBatch
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
	DownloadMedia(ctx context.Context, source MediaSource, file *os.File, maxBytes int64) ([]byte, error)
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
	ImportHistory(ctx context.Context, batch HistoryBatch) error
	LookupSend(ctx context.Context, actorID, key, requestHash string) (Message, bool, error)
	ReserveSend(ctx context.Context, message Message, actorID, key, requestHash string) (Message, bool, error)
	CompleteSend(ctx context.Context, id, state string, sent SentText) (Message, error)
	GetMessage(ctx context.Context, id string) (Message, error)
	ListMessages(ctx context.Context, accountID string, before *PageCursor, limit int) ([]Message, error)
	ListConversationMessages(ctx context.Context, conversationID string, before *PageCursor, limit int) ([]Message, error)
	GetMedia(ctx context.Context, attachmentID string) (MediaRecord, error)
	ListPendingMedia(ctx context.Context, now time.Time, limit int) ([]string, error)
	StoredMediaBytes(ctx context.Context) (int64, error)
	MarkMediaReady(ctx context.Context, attachmentID, profileID, key string, size int64, sha256 []byte) error
	MarkMediaRemote(ctx context.Context, attachmentID string, version int64) error
	MarkMediaUnavailable(ctx context.Context, attachmentID string) error
	UpdateMediaRef(ctx context.Context, attachmentID string, ref []byte) error
	ScheduleMediaRetry(ctx context.Context, attachmentID string, next time.Time, attempts int) error
	BlockMedia(ctx context.Context, attachmentID, reason string, requiredBytes int64) error
	ClearMediaBlock(ctx context.Context, attachmentID string) error
	ListMediaOrphans(ctx context.Context, limit int) ([]MediaObject, error)
	ClearMediaOrphan(ctx context.Context, object MediaObject) error
	Close() error
}

type MediaObject struct {
	ProfileID string
	Key       string
}
