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

func translateMessageChange(e *events.Message) *core.MessageChange {
	if e == nil || e.Info.Chat.IsEmpty() || e.Info.ID == "" {
		return nil
	}
	c := core.MessageChange{ChatID: e.Info.Chat.ToNonAD().String(), EventID: string(e.Info.ID), At: e.Info.Timestamp}
	payload := e.Message.GetProtocolMessage()
	// ParseWebMessage unwraps historical edits, so inspect the original envelope too.
	if raw := e.RawMessage; raw != nil {
		copy := events.Message{RawMessage: raw}
		copy.UnwrapRaw()
		if p := copy.Message.GetProtocolMessage(); p != nil {
			payload = p
		}
	}
	if payload != nil && payload.GetKey().GetID() != "" {
		c.TargetID = payload.GetKey().GetID()
		if payload.GetTimestampMS() > 0 {
			c.At = time.UnixMilli(payload.GetTimestampMS()).UTC()
		}
		switch payload.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			c.Kind = "revoke"
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			kind, text := messageContent(payload.GetEditedMessage())
			if kind == "" {
				return nil
			}
			c.Kind = "edit"
			c.Text = text
		default:
			return nil
		}
	} else if e.SourceWebMsg != nil && (e.SourceWebMsg.GetMessageStubType() == waWeb.WebMessageInfo_REVOKE || e.SourceWebMsg.GetMessageStubType() == waWeb.WebMessageInfo_ADMIN_REVOKE) {
		c.TargetID = string(e.Info.ID)
		c.Kind = "revoke"
	} else {
		return nil
	}
	if c.At.IsZero() {
		return nil
	}
	return &c
}

func (s *session) EditMessage(ctx context.Context, m core.Message, text string, at time.Time) (core.MessageChange, error) {
	chat, err := recipientJID(m.ChatID)
	if err != nil {
		return core.MessageChange{}, err
	}
	payload := s.client.BuildEdit(chat, types.MessageID(m.ProviderMessageID), textMessage(text, m.Reply))
	payload.EditedMessage.Message.ProtocolMessage.TimestampMS = proto.Int64(at.UnixMilli())
	return s.sendMessageChange(ctx, m, core.MessageChange{Kind: "edit", Text: text, At: at}, payload)
}
func (s *session) RevokeMessage(ctx context.Context, m core.Message, at time.Time) (core.MessageChange, error) {
	chat, err := recipientJID(m.ChatID)
	if err != nil {
		return core.MessageChange{}, err
	}
	return s.sendMessageChange(ctx, m, core.MessageChange{Kind: "revoke", At: at}, s.client.BuildRevoke(chat, types.EmptyJID, types.MessageID(m.ProviderMessageID)))
}
func (s *session) sendMessageChange(ctx context.Context, m core.Message, c core.MessageChange, payload *waE2E.Message) (core.MessageChange, error) {
	chat, err := recipientJID(m.ChatID)
	if err != nil {
		return core.MessageChange{}, err
	}
	id := s.client.GenerateMessageID()
	if _, err = s.client.SendMessage(ctx, chat, payload, whatsmeow.SendRequestExtra{ID: id}); err != nil {
		return core.MessageChange{}, fmt.Errorf("%w: %v", core.ErrMessageChangeUnconfirmed, err)
	}
	c.ChatID, c.TargetID, c.EventID = m.ChatID, m.ProviderMessageID, string(id)
	return c, nil
}
