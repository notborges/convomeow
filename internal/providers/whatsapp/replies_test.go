package whatsapp

import (
	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	"strings"
	"testing"
)

func TestReplyContextForTextAndMedia(t *testing.T) {
	reply := &core.Reply{ProviderMessageID: "quoted-id", SenderID: "123@s.whatsapp.net", Kind: core.MessageKindImage, Text: "caption"}
	messages := []*waE2E.Message{
		textMessage("answer", reply),
		{ImageMessage: &waE2E.ImageMessage{}}, {VideoMessage: &waE2E.VideoMessage{}},
		{AudioMessage: &waE2E.AudioMessage{}}, {DocumentMessage: &waE2E.DocumentMessage{}}, {StickerMessage: &waE2E.StickerMessage{}},
	}
	for _, message := range messages {
		setMediaReply(message, reply)
		got := replyFromMessage(message)
		if got == nil || got.ProviderMessageID != reply.ProviderMessageID || got.SenderID != reply.SenderID || got.Kind != reply.Kind || got.Text != reply.Text {
			t.Fatalf("quote roundtrip: %+v", got)
		}
	}
	if textMessage("plain", nil).GetConversation() != "plain" {
		t.Fatal("plain text changed")
	}
	context := &waE2E.ContextInfo{StanzaID: proto.String("id"), QuotedMessage: &waE2E.Message{Conversation: proto.String(strings.Repeat("é", 600))}}
	got := replyFromMessage(&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{ContextInfo: context}})
	if len([]rune(got.Text)) != 512 {
		t.Fatal("quote not bounded")
	}
	context.QuotedMessage = &waE2E.Message{ViewOnceMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("private")}}}}
	if got := replyFromMessage(&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{ContextInfo: context}}); got.Text != "" {
		t.Fatal("view-once quote exposed content")
	}
}
