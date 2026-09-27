package whatsapp

import (
	"context"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func translatePresence(event *events.ChatPresence) *core.ChatPresence {
	if event.IsFromMe || event.Chat.IsEmpty() || event.Sender.IsEmpty() {
		return nil
	}
	activity := core.ActivityPaused
	switch event.State {
	case types.ChatPresencePaused:
	case types.ChatPresenceComposing:
		switch event.Media {
		case types.ChatPresenceMediaText:
			activity = core.ActivityTyping
		case types.ChatPresenceMediaAudio:
			activity = core.ActivityRecording
		default:
			return nil
		}
	default:
		return nil
	}
	sender := event.Sender
	if !event.SenderAlt.IsEmpty() && event.SenderAlt.Server == types.DefaultUserServer {
		sender = event.SenderAlt
	}
	return &core.ChatPresence{ChatID: event.Chat.ToNonAD().String(), ParticipantID: sender.ToNonAD().String(), Activity: activity}
}

func (s *session) SetOnline(ctx context.Context, online bool) error {
	state := types.PresenceUnavailable
	if online {
		state = types.PresenceAvailable
	}
	return s.client.SendPresence(ctx, state)
}

func (s *session) SendChatPresence(ctx context.Context, chatID string, activity core.ChatActivity) error {
	if !activity.Valid() {
		return core.ErrInvalid
	}
	chat, err := recipientJID(chatID)
	if err != nil {
		return err
	}
	state, media := types.ChatPresenceComposing, types.ChatPresenceMediaText
	if activity == core.ActivityPaused {
		state = types.ChatPresencePaused
	}
	if activity == core.ActivityRecording {
		media = types.ChatPresenceMediaAudio
	}
	return s.client.SendChatPresence(ctx, chat, state, media)
}
