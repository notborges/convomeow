package whatsapp

import (
	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"testing"
)

func TestTranslateChatPresence(t *testing.T) {
	source := types.MessageSource{Chat: types.NewJID("group", types.GroupServer), Sender: types.NewJID("123", types.HiddenUserServer), SenderAlt: types.NewJID("456", types.DefaultUserServer), IsGroup: true}
	for _, tc := range []struct {
		state types.ChatPresence
		media types.ChatPresenceMedia
		want  core.ChatActivity
	}{
		{types.ChatPresenceComposing, types.ChatPresenceMediaText, core.ActivityTyping},
		{types.ChatPresenceComposing, types.ChatPresenceMediaAudio, core.ActivityRecording},
		{types.ChatPresencePaused, types.ChatPresenceMediaAudio, core.ActivityPaused},
	} {
		got := translatePresence(&events.ChatPresence{MessageSource: source, State: tc.state, Media: tc.media})
		if got == nil || got.Activity != tc.want || got.ParticipantID != "456@s.whatsapp.net" || got.ChatID != "group@g.us" {
			t.Fatalf("presence: %+v", got)
		}
	}
	source.IsFromMe = true
	if translatePresence(&events.ChatPresence{MessageSource: source, State: types.ChatPresenceComposing}) != nil {
		t.Fatal("own-device activity leaked")
	}
	source.IsFromMe = false
	if translatePresence(&events.ChatPresence{MessageSource: source, State: "invalid"}) != nil {
		t.Fatal("invalid activity accepted")
	}
}
