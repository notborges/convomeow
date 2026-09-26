package whatsapp

import (
	"testing"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestMediaDuration(t *testing.T) {
	for _, tc := range []struct {
		kind    core.MessageKind
		message *waE2E.Message
	}{
		{core.MessageKindAudio, &waE2E.Message{AudioMessage: &waE2E.AudioMessage{Seconds: proto.Uint32(37)}}},
		{core.MessageKindVideo, &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Seconds: proto.Uint32(37)}}},
	} {
		attachment := mediaAttachment(tc.message, tc.kind, false)
		if attachment == nil || attachment.DurationSeconds != 37 {
			t.Fatalf("duration lost for %s", tc.kind)
		}
	}
}
