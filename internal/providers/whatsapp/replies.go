package whatsapp

import (
	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func replyFromMessage(message *waE2E.Message) *core.Reply {
	var context *waE2E.ContextInfo
	switch {
	case message.GetExtendedTextMessage() != nil:
		context = message.GetExtendedTextMessage().GetContextInfo()
	case message.GetImageMessage() != nil:
		context = message.GetImageMessage().GetContextInfo()
	case message.GetVideoMessage() != nil:
		context = message.GetVideoMessage().GetContextInfo()
	case message.GetAudioMessage() != nil:
		context = message.GetAudioMessage().GetContextInfo()
	case message.GetDocumentMessage() != nil:
		context = message.GetDocumentMessage().GetContextInfo()
	case message.GetStickerMessage() != nil:
		context = message.GetStickerMessage().GetContextInfo()
	}
	if context.GetStanzaID() == "" {
		return nil
	}
	kind, text := core.MessageKind("unknown"), ""
	if quoted := context.GetQuotedMessage(); quoted != nil {
		if k, value := messageContent(quoted); k != "" {
			kind, text = k, value
		}
	}
	preview := []rune(text)
	if len(preview) > 512 {
		preview = preview[:512]
	}
	return &core.Reply{ProviderMessageID: context.GetStanzaID(), SenderID: context.GetParticipant(), Kind: kind, Text: string(preview)}
}

func replyContext(reply *core.Reply) *waE2E.ContextInfo {
	if reply == nil {
		return nil
	}
	quoted := &waE2E.Message{}
	switch reply.Kind {
	case core.MessageKindText:
		quoted.Conversation = proto.String(reply.Text)
	case core.MessageKindImage:
		quoted.ImageMessage = &waE2E.ImageMessage{Caption: proto.String(reply.Text)}
	case core.MessageKindVideo:
		quoted.VideoMessage = &waE2E.VideoMessage{Caption: proto.String(reply.Text)}
	case core.MessageKindAudio:
		quoted.AudioMessage = &waE2E.AudioMessage{}
	case core.MessageKindDocument:
		quoted.DocumentMessage = &waE2E.DocumentMessage{Caption: proto.String(reply.Text)}
	case core.MessageKindSticker:
		quoted.StickerMessage = &waE2E.StickerMessage{}
	case core.MessageKindContact:
		quoted.ContactMessage = &waE2E.ContactMessage{}
	case core.MessageKindLocation:
		quoted.LocationMessage = &waE2E.LocationMessage{}
	}
	return &waE2E.ContextInfo{StanzaID: proto.String(reply.ProviderMessageID), Participant: proto.String(reply.SenderID), QuotedMessage: quoted}
}

func textMessage(text string, reply *core.Reply) *waE2E.Message {
	if reply == nil {
		return &waE2E.Message{Conversation: proto.String(text)}
	}
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(text), ContextInfo: replyContext(reply)}}
}

func setMediaReply(message *waE2E.Message, reply *core.Reply) {
	if reply == nil {
		return
	}
	context := replyContext(reply)
	switch {
	case message.ImageMessage != nil:
		message.ImageMessage.ContextInfo = context
	case message.VideoMessage != nil:
		message.VideoMessage.ContextInfo = context
	case message.AudioMessage != nil:
		message.AudioMessage.ContextInfo = context
	case message.DocumentMessage != nil:
		message.DocumentMessage.ContextInfo = context
	case message.StickerMessage != nil:
		message.StickerMessage.ContextInfo = context
	}
}
