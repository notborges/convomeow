package whatsapp

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func historyText(chatID, messageID string, at uint64) *waHistorySync.HistorySyncMsg {
	return &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
		Key:              &waCommon.MessageKey{RemoteJID: proto.String(chatID), ID: proto.String(messageID)},
		MessageTimestamp: proto.Uint64(at), Message: &waE2E.Message{Conversation: proto.String(messageID)},
	}}
}

func TestHistorySyncUsesWhatsmeowParserAndBoundsBatches(t *testing.T) {
	client := whatsmeow.NewClient(&store.Device{}, nil)
	var emitted []core.Event
	s := &session{client: client, emit: func(event core.Event) { emitted = append(emitted, event) }}
	pn, lid := "15551234567@s.whatsapp.net", "abc123@lid"
	at := uint64(time.Date(2025, 7, 8, 9, 10, 11, 0, time.UTC).Unix())
	conversation := &waHistorySync.Conversation{ID: proto.String(pn), LidJID: proto.String(lid), LastMsgTimestamp: proto.Uint64(at + 101)}
	for i := range 101 {
		conversation.Messages = append(conversation.Messages, historyText(pn, fmt.Sprintf("text-%d", i), at+uint64(i)))
	}
	image := &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), FileLength: proto.Uint64(1234),
		Caption: proto.String("photo"), DirectPath: proto.String("/media/path"), MediaKey: []byte("private-key"),
		FileSHA256: []byte("plain-hash"), FileEncSHA256: []byte("encrypted-hash")}
	conversation.Messages = append(conversation.Messages, &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
		Key:              &waCommon.MessageKey{RemoteJID: proto.String(pn), ID: proto.String("image-1")},
		MessageTimestamp: proto.Uint64(at + 101), Message: &waE2E.Message{ImageMessage: image},
	}})
	conversation.Messages = append(conversation.Messages, &waHistorySync.HistorySyncMsg{})
	s.importHistory(&events.HistorySync{Data: &waHistorySync.HistorySync{
		PhoneNumberToLidMappings: []*waHistorySync.PhoneNumberToLIDMapping{{PnJID: proto.String(pn), LidJID: proto.String(lid)}},
		Conversations:            []*waHistorySync.Conversation{conversation},
	}})
	if len(emitted) != 3 || emitted[0].Type != core.EventHistory || len(emitted[0].History.Links) != 1 ||
		len(emitted[1].History.Messages) != 100 || len(emitted[2].History.Messages) != 2 {
		t.Fatalf("history batches: %+v", emitted)
	}
	if emitted[1].History.Chat.ID != pn || len(emitted[1].History.Chat.Aliases) != 1 || emitted[1].History.Chat.Aliases[0] != lid {
		t.Fatalf("chat aliases: %+v", emitted[1].History.Chat)
	}
	message := emitted[2].History.Messages[1]
	if message.Kind != core.MessageKindImage || message.Text != "photo" || len(message.Attachments) != 1 {
		t.Fatalf("media translation: %+v", message)
	}
	attachment := message.Attachments[0]
	if attachment.MIMEType != "image/jpeg" || attachment.Size != 1234 || attachment.Availability != "remote" {
		t.Fatalf("media metadata: %+v", attachment)
	}
	var ref mediaReference
	if err := json.Unmarshal(attachment.ProviderRef, &ref); err != nil || ref.DirectPath != "/media/path" || string(ref.MediaKey) != "private-key" || ref.MediaType == "" {
		t.Fatalf("media retrieval data: %+v, %v", ref, err)
	}
}

func TestHistoryGroupSenderAndEmptyConversation(t *testing.T) {
	client := whatsmeow.NewClient(&store.Device{}, nil)
	var emitted []core.Event
	s := &session{client: client, emit: func(event core.Event) { emitted = append(emitted, event) }}
	group, sender := "12345@g.us", "15550000001@s.whatsapp.net"
	at := uint64(time.Date(2025, 8, 9, 10, 11, 12, 0, time.UTC).Unix())
	groupMessage := historyText(group, "group-1", at)
	groupMessage.Message.Participant = proto.String(sender)
	s.importHistory(&events.HistorySync{Data: &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{
		{ID: proto.String(group), Messages: []*waHistorySync.HistorySyncMsg{groupMessage}},
		{ID: proto.String("15551112222@s.whatsapp.net"), ConversationTimestamp: proto.Uint64(at)},
	}}})
	if len(emitted) != 2 || len(emitted[0].History.Messages) != 1 || emitted[0].History.Messages[0].SenderID != sender ||
		len(emitted[1].History.Messages) != 0 || emitted[1].History.Chat.LastActivityAt.IsZero() {
		t.Fatalf("group sender or empty conversation: %+v", emitted)
	}
}

func TestMediaAvailabilityRequiresDownloadPath(t *testing.T) {
	message := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{MediaKey: []byte("key")}}
	attachment := mediaAttachment(message, core.MessageKindImage, false)
	if attachment == nil || attachment.Availability != "unavailable" {
		t.Fatalf("missing download path: %+v", attachment)
	}

	message.ImageMessage.DirectPath = proto.String("/media/path")
	attachment = mediaAttachment(message, core.MessageKindImage, true)
	if attachment == nil || attachment.Availability != "unavailable" || len(attachment.ProviderRef) != 0 {
		t.Fatalf("view-once media: %+v", attachment)
	}
}

func TestLoggedOutSessionStopsHistoryWorker(t *testing.T) {
	s := newSession(&store.Device{}, func(core.Event) {}).(*session)
	s.handleEvent(&events.LoggedOut{})
	done := make(chan struct{})
	go func() {
		s.historyWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("history worker did not stop after logout")
	}
	s.Close()
}
