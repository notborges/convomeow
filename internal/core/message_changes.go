package core

import (
	"context"
	"errors"
	"time"
)

var ErrMessageChangeUnconfirmed = errors.New("message change outcome is unconfirmed")

type MessageChange struct {
	ChatID   string
	TargetID string
	EventID  string
	Kind     string
	Text     string
	At       time.Time
}

type MessageChanger interface {
	EditMessage(context.Context, Message, string, time.Time) (MessageChange, error)
	RevokeMessage(context.Context, Message, time.Time) (MessageChange, error)
}

type MessageRevision struct {
	ID   string    `json:"id"`
	Kind string    `json:"kind"`
	Text string    `json:"text,omitempty"`
	At   time.Time `json:"at"`
}
