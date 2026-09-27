package core

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"go.mau.fi/util/emojishortcodes"
)

var ErrReactionUnconfirmed = errors.New("reaction outcome is unconfirmed")

// An empty emoji is a removal. Retain its timestamp to reject older history.
type Reaction struct {
	ChatID           string
	TargetID         string
	ParticipantID    string
	ParticipantAlias string
	IsOwn            bool
	Emoji            string
	At               time.Time
	EventID          string
}

type ReactionSummary struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Own   bool   `json:"own"`
}

type MessageReaction struct {
	ParticipantID string    `json:"participant_id"`
	DisplayName   string    `json:"display_name,omitempty"`
	IsOwn         bool      `json:"is_own"`
	Emoji         string    `json:"emoji"`
	At            time.Time `json:"at"`
}

func ValidReactionEmoji(emoji string) bool {
	return len(emoji) <= 128 && utf8.ValidString(emoji) && emojishortcodes.Get(emoji) != ""
}

type ReactionSender interface {
	SendReaction(ctx context.Context, target Message, emoji string, at time.Time) (Reaction, error)
}
