package whatsapp

import (
	"context"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow/types"
)

func (s *session) RequestHistory(ctx context.Context, before core.Message, count int) (string, error) {
	jid, err := types.ParseJID(before.ChatID)
	if err != nil || jid.IsEmpty() || before.ProviderMessageID == "" || count < 1 || count > 50 {
		return "", core.ErrInvalid
	}
	info := &types.MessageInfo{MessageSource: types.MessageSource{Chat: jid.ToNonAD(), IsFromMe: before.Direction == "outbound"},
		ID: types.MessageID(before.ProviderMessageID), Timestamp: before.OccurredAt}
	response, err := s.client.SendPeerMessage(ctx, s.client.BuildHistorySyncRequest(info, count))
	if err != nil {
		return "", err
	}
	return string(response.ID), nil
}
