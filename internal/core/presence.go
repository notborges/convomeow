package core

import "context"

type ChatActivity string

const (
	ActivityTyping    ChatActivity = "typing"
	ActivityRecording ChatActivity = "recording"
	ActivityPaused    ChatActivity = "paused"
)

func (a ChatActivity) Valid() bool {
	return a == ActivityTyping || a == ActivityRecording || a == ActivityPaused
}

type ChatPresence struct {
	ChatID        string
	ParticipantID string
	Activity      ChatActivity
}

type PresenceSender interface {
	SetOnline(context.Context, bool) error
	SendChatPresence(context.Context, string, ChatActivity) error
}
