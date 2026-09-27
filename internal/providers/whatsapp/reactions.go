package whatsapp

import (
	"context"
	"fmt"
	"time"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func translateReaction(event *events.Message) *core.Reaction {
	if event == nil || event.Message == nil {
		return nil
	}
	payload := event.Message.GetReactionMessage()
	if payload == nil || payload.GetKey().GetID() == "" || event.Info.ID == "" || event.Info.Chat.IsEmpty() || (!event.Info.IsFromMe && event.Info.Sender.IsEmpty()) || len(payload.GetText()) > 128 {
		return nil
	}
	at := time.UnixMilli(payload.GetSenderTimestampMS()).UTC()
	if payload.GetSenderTimestampMS() <= 0 {
		at = event.Info.Timestamp
	}
	if at.IsZero() {
		return nil
	}
	r := &core.Reaction{ChatID: event.Info.Chat.ToNonAD().String(), TargetID: payload.GetKey().GetID(),
		ParticipantID: event.Info.Sender.ToNonAD().String(), IsOwn: event.Info.IsFromMe, Emoji: payload.GetText(), At: at, EventID: string(event.Info.ID)}
	if !event.Info.SenderAlt.IsEmpty() {
		r.ParticipantAlias = event.Info.SenderAlt.ToNonAD().String()
	}
	return r
}

func historyReactions(info *waWeb.WebMessageInfo, message core.Message) []core.Reaction {
	var result []core.Reaction
	for _, entry := range info.GetReactions() {
		key := entry.GetKey()
		participant := key.GetParticipant()
		if participant == "" {
			participant = key.GetRemoteJID()
		}
		if participant == "" {
			participant = message.ChatID
		}
		jid, err := types.ParseJID(participant)
		if key.GetID() == "" || entry.GetSenderTimestampMS() <= 0 || len(entry.GetText()) > 128 || (!key.GetFromMe() && (err != nil || jid.IsEmpty() || (jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer))) {
			continue
		}
		result = append(result, core.Reaction{ChatID: message.ChatID, TargetID: message.ProviderMessageID,
			ParticipantID: jid.ToNonAD().String(), IsOwn: key.GetFromMe(), Emoji: entry.GetText(), At: time.UnixMilli(entry.GetSenderTimestampMS()).UTC(), EventID: key.GetID()})
	}
	return result
}

func (s *session) normalizeReaction(r *core.Reaction) {
	if r == nil || r.IsOwn || r.ParticipantAlias != "" || s.client.Store == nil || s.client.Store.LIDs == nil {
		return
	}
	jid, err := types.ParseJID(r.ParticipantID)
	if err != nil || jid.Server != types.HiddenUserServer {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pn, err := s.client.Store.LIDs.GetPNForLID(ctx, jid)
	if err == nil && !pn.IsEmpty() {
		r.ParticipantAlias = pn.ToNonAD().String()
	}
}

func (s *session) receiveReaction(event *events.Message) bool {
	if event.Message.GetReactionMessage() == nil && event.Message.GetEncReactionMessage() == nil {
		return false
	}
	if event.Message.GetEncReactionMessage() != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		payload, err := s.client.DecryptReaction(ctx, event)
		cancel()
		if err != nil {
			s.client.Log.Warnf("Unable to decrypt reaction: %v", err)
			return true
		}
		copy := *event
		copy.Message = &waE2E.Message{ReactionMessage: payload}
		event = &copy
	}
	if r := translateReaction(event); r != nil {
		s.normalizeReaction(r)
		s.emit(core.Event{Type: core.EventReaction, Reaction: r})
	}
	return true
}

func (s *session) SendReaction(ctx context.Context, target core.Message, emoji string, at time.Time) (core.Reaction, error) {
	chat, err := recipientJID(target.ChatID)
	if err != nil {
		return core.Reaction{}, err
	}
	if chat.Server != types.DefaultUserServer && chat.Server != types.HiddenUserServer && chat.Server != types.GroupServer {
		return core.Reaction{}, fmt.Errorf("%w: reactions require a direct or group conversation", core.ErrInvalid)
	}
	sender := types.EmptyJID
	if target.Direction != "outbound" {
		sender, err = recipientJID(target.SenderID)
		if err != nil {
			return core.Reaction{}, err
		}
	}
	payload := s.client.BuildReaction(chat, sender, types.MessageID(target.ProviderMessageID), emoji)
	payload.ReactionMessage.SenderTimestampMS = proto.Int64(at.UnixMilli())
	id := s.client.GenerateMessageID()
	_, err = s.client.SendMessage(ctx, chat, payload, whatsmeow.SendRequestExtra{ID: id})
	if err != nil {
		return core.Reaction{}, fmt.Errorf("%w: %v", core.ErrReactionUnconfirmed, err)
	}
	return core.Reaction{ChatID: chat.ToNonAD().String(), TargetID: target.ProviderMessageID, IsOwn: true, Emoji: emoji, At: at, EventID: string(id)}, nil
}
