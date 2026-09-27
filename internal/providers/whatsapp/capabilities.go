package whatsapp

import (
	"time"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow/types"
)

func (*Connector) Capabilities() []string {
	return []string{"read_messages", "read_media", "read_contacts", "read_avatars", "send_text", "send_media", "start_conversation", "replies", "reactions", "typing", "read_receipts", "message_receipts", "request_history", "edit_messages", "revoke_messages"}
}

func (*Connector) MessageActions(m core.Message, now time.Time) core.MessageActions {
	a := core.MessageActions{}
	jid, err := types.ParseJID(m.ChatID)
	if err != nil || (jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer && jid.Server != types.GroupServer) || m.DeletedAt != nil || (m.State != "sent" && m.State != "received") {
		return a
	}
	a.Reply, a.React = true, true
	a.Receipts = m.Direction == "outbound"
	if m.Direction == "outbound" {
		editUntil := m.OccurredAt.Add(15 * time.Minute)
		// WhatsApp permits revocation for about two days; use the conservative 48-hour boundary.
		revokeUntil := m.OccurredAt.Add(48 * time.Hour)
		a.EditUntil, a.RevokeUntil = &editUntil, &revokeUntil
		a.Edit = m.Kind == core.MessageKindText && !now.Before(m.OccurredAt) && now.Before(editUntil)
		a.Revoke = !now.Before(m.OccurredAt) && now.Before(revokeUntil)
	}
	return a
}
