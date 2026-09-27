package whatsapp

import (
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestMessageChangeTranslationPreservesTargetAndTime(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Millisecond)
	protocol := &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: &waCommon.MessageKey{ID: proto.String("target")}, TimestampMS: proto.Int64(at.UnixMilli()), EditedMessage: &waE2E.Message{Conversation: proto.String("new text")}}
	e := &events.Message{Info: types.MessageInfo{ID: "event", Timestamp: at.Add(time.Second), MessageSource: types.MessageSource{Chat: types.NewJID("123", types.DefaultUserServer)}}, RawMessage: &waE2E.Message{EditedMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ProtocolMessage: protocol}}}}
	e.UnwrapRaw()
	for _, history := range []bool{false, true} {
		if history {
			e.Message = protocol.EditedMessage
			e.Info.ID = "target"
		}
		c := translateMessageChange(e)
		if c == nil || c.TargetID != "target" || c.Text != "new text" || !c.At.Equal(at) {
			t.Fatal(c)
		}
	}
	protocol.Type = waE2E.ProtocolMessage_REVOKE.Enum()
	if c := translateMessageChange(e); c == nil || c.Kind != "revoke" || c.Text != "" {
		t.Fatal(c)
	}
}
func TestWhatsAppMessageActionEligibility(t *testing.T) {
	now := time.Now()
	m := core.Message{ChatID: "123@s.whatsapp.net", Direction: "outbound", State: "sent", Kind: core.MessageKindText, OccurredAt: now.Add(-time.Minute)}
	c := &Connector{}
	if a := c.MessageActions(m, now); !a.Edit || !a.Revoke || !a.React || !a.Reply {
		t.Fatal(a)
	}
	m.OccurredAt = now.Add(-16 * time.Minute)
	if a := c.MessageActions(m, now); a.Edit || !a.Revoke {
		t.Fatal(a)
	}
	m.Direction = "inbound"
	if a := c.MessageActions(m, now); a.Edit || a.Revoke || !a.React {
		t.Fatal(a)
	}
	m.DeletedAt = &now
	if a := c.MessageActions(m, now); a.React || a.Reply || a.Edit || a.Revoke {
		t.Fatal(a)
	}
	m.DeletedAt = nil
	m.ChatID = "status@broadcast"
	if a := c.MessageActions(m, now); a.React || a.Reply {
		t.Fatal(a)
	}
}
