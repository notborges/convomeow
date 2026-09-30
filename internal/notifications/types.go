package notifications

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

const Lifetime = 5 * time.Minute

type Session struct {
	ID         string
	Generation string
	ExpiresAt  time.Time
}

func Generation(token string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("convomeow:push-authorization:v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

type Subscription struct {
	ID       string `json:"id,omitempty"`
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Locale  string  `json:"locale"`
	Preview bool    `json:"preview"`
	Session Session `json:"-"`
}

type Job struct {
	Subscription Subscription
	MessageID    string
	Attempts     int
	ExpiresAt    time.Time
}

type Payload struct {
	Version          int              `json:"version"`
	AccountID        string           `json:"account_id"`
	ConversationID   string           `json:"conversation_id"`
	MessageID        string           `json:"message_id"`
	AccountLabel     string           `json:"account_label"`
	ConversationName string           `json:"conversation_name"`
	Kind             core.MessageKind `json:"kind"`
	Text             string           `json:"text,omitempty"`
	Locale           string           `json:"locale"`
}

type Result struct {
	Status     int
	RetryAfter time.Duration
}

type Sender interface {
	Send(context.Context, Subscription, Payload, time.Duration) (Result, error)
}

type Repository interface {
	SaveIncomingMessage(context.Context, core.Message, string) (core.Message, error)
	SaveSubscription(context.Context, Subscription) (Subscription, error)
	GetSubscription(context.Context, string, Session) (Subscription, error)
	DeleteSubscription(context.Context, string, Session) error
	DeleteNotificationSession(context.Context, Session) error
	ClaimNotification(context.Context, string, time.Time) (*Job, error)
	FinishNotification(context.Context, Job, time.Time) error
}
