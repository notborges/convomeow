package whatsapp

import (
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestReactionTranslation(t *testing.T) {
	at := time.Date(2026, 9, 27, 12, 0, 0, 123000000, time.UTC)
	event := &events.Message{Info: types.MessageInfo{ID: "reaction", Timestamp: at, MessageSource: types.MessageSource{Chat: types.NewJID("group", types.GroupServer), Sender: types.NewJID("123", types.HiddenUserServer), SenderAlt: types.NewJID("456", types.DefaultUserServer)}}, Message: &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: &waCommon.MessageKey{ID: proto.String("target")}, Text: proto.String("👍🏽"), SenderTimestampMS: proto.Int64(at.UnixMilli())}}}
	var emitted []core.Event
	translateEvent(func(e core.Event) { emitted = append(emitted, e) }, event)
	if len(emitted) != 1 || emitted[0].Type != core.EventReaction {
		t.Fatalf("events: %+v", emitted)
	}
	r := emitted[0].Reaction
	if r.TargetID != "target" || r.ParticipantID != "123@lid" || r.ParticipantAlias != "456@s.whatsapp.net" || !r.At.Equal(at) || r.Emoji != "👍🏽" {
		t.Fatalf("reaction: %+v", r)
	}
	event.Info.IsFromMe = true
	event.Message.ReactionMessage.Text = proto.String("")
	r = translateReaction(event)
	if r == nil || !r.IsOwn || r.Emoji != "" {
		t.Fatal("own removal lost")
	}
	if m := translateMessage(event); m != nil {
		t.Fatal("reaction became chat message")
	}
	event.Message.ReactionMessage.Key = nil
	if translateReaction(event) != nil {
		t.Fatal("missing target accepted")
	}
}

func TestHistoryReactionsKeepActorAndTimestamp(t *testing.T) {
	at := time.Now().UnixMilli()
	info := &waWeb.WebMessageInfo{Reactions: []*waWeb.Reaction{
		{Key: &waCommon.MessageKey{ID: proto.String("r1"), Participant: proto.String("123:4@s.whatsapp.net")}, Text: proto.String("🔥"), SenderTimestampMS: proto.Int64(at)},
		{Key: &waCommon.MessageKey{ID: proto.String("r2"), FromMe: proto.Bool(true)}, Text: proto.String(""), SenderTimestampMS: proto.Int64(at + 1)},
		{Key: &waCommon.MessageKey{ID: proto.String("bad")}, Text: proto.String("👍")},
	}}
	r := historyReactions(info, core.Message{ChatID: "group@g.us", ProviderMessageID: "target", Direction: "inbound"})
	if len(r) != 2 || r[0].ParticipantID != "123@s.whatsapp.net" || r[0].TargetID != "target" || r[0].At.UnixMilli() != at || !r[1].IsOwn || r[1].Emoji != "" {
		t.Fatalf("history: %+v", r)
	}
}
