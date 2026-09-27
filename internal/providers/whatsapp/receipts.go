package whatsapp

import (
	"context"
	"time"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func translateReceipt(event *events.Receipt) *core.Receipt {
	if event.IsFromMe || !event.MessageSender.IsEmpty() || event.Chat.IsEmpty() || event.Sender.IsEmpty() || event.Timestamp.IsZero() {
		return nil
	}
	kind := ""
	switch event.Type {
	case types.ReceiptTypeDelivered:
		kind = "delivered"
	case types.ReceiptTypeRead:
		kind = "read"
	default:
		return nil
	}
	receipt := &core.Receipt{ChatID: event.Chat.ToNonAD().String(), ParticipantID: event.Sender.ToNonAD().String(), Kind: kind, At: event.Timestamp, Group: event.IsGroup}
	if !event.SenderAlt.IsEmpty() {
		receipt.ParticipantAlias = event.SenderAlt.ToNonAD().String()
	}
	for _, id := range event.MessageIDs {
		if id != "" {
			receipt.MessageIDs = append(receipt.MessageIDs, string(id))
		}
	}
	if len(receipt.MessageIDs) == 0 {
		return nil
	}
	return receipt
}

func (s *session) SendReadReceipts(ctx context.Context, chatID, senderID string, providerIDs []string, at time.Time) error {
	chat, err := recipientJID(chatID)
	if err != nil {
		return err
	}
	sender, err := recipientJID(senderID)
	if err != nil {
		return err
	}
	ids := make([]types.MessageID, len(providerIDs))
	for i, id := range providerIDs {
		ids[i] = types.MessageID(id)
	}
	return s.client.MarkRead(ctx, ids, at, chat, sender)
}

func historyReceipts(info *waWeb.WebMessageInfo, message core.Message, group bool) []core.Receipt {
	if message.Direction != "outbound" {
		return nil
	}
	var receipts []core.Receipt
	for _, entry := range info.GetUserReceipt() {
		participant, err := types.ParseJID(entry.GetUserJID())
		if err != nil || participant.IsEmpty() {
			continue
		}
		for kind, stamp := range map[string]int64{"delivered": entry.GetReceiptTimestamp(), "read": entry.GetReadTimestamp()} {
			if stamp <= 0 {
				continue
			}
			receipts = append(receipts, core.Receipt{ChatID: message.ChatID, ParticipantID: participant.ToNonAD().String(), MessageIDs: []string{message.ProviderMessageID}, Kind: kind, At: time.Unix(stamp, 0), Group: group})
		}
	}
	return receipts
}
