package whatsapp

import (
	"time"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

const historyBatchSize = 100

func (s *session) importHistory(event *events.HistorySync) {
	if event == nil || event.Data == nil {
		return
	}
	var links []core.ChatLink
	for _, mapping := range event.Data.GetPhoneNumberToLidMappings() {
		pn, pnOK := historyJID(mapping.GetPnJID())
		lid, lidOK := historyJID(mapping.GetLidJID())
		if !pnOK || !lidOK || pn.Server != types.DefaultUserServer || lid.Server != types.HiddenUserServer {
			continue
		}
		links = append(links, core.ChatLink{First: pn.String(), Second: lid.String()})
		if len(links) == historyBatchSize {
			s.emit(core.Event{Type: core.EventHistory, History: &core.HistoryBatch{Links: links}})
			links = nil
		}
	}
	if len(links) > 0 {
		s.emit(core.Event{Type: core.EventHistory, History: &core.HistoryBatch{Links: links}})
	}
	conversationCount, messageCount, skipped := 0, 0, 0
	for _, conversation := range event.Data.GetConversations() {
		chat, chatJID, ok := historyChat(conversation)
		if !ok {
			skipped++
			continue
		}
		conversationCount++
		batch := core.HistoryBatch{Chat: &chat, Messages: make([]core.Message, 0, historyBatchSize)}
		sentBatch := false
		for _, item := range conversation.GetMessages() {
			if item.GetMessage() == nil {
				skipped++
				continue
			}
			parsed, err := s.client.ParseWebMessage(chatJID, item.GetMessage())
			if err != nil {
				skipped++
				continue
			}
			message := translateMessage(parsed)
			if message == nil || message.OccurredAt.Before(time.Unix(946684800, 0)) {
				skipped++
				continue
			}
			batch.Messages = append(batch.Messages, *message)
			messageCount++
			if len(batch.Messages) == historyBatchSize {
				ready := batch
				s.emit(core.Event{Type: core.EventHistory, History: &ready})
				sentBatch = true
				batch = core.HistoryBatch{Chat: &chat, Messages: make([]core.Message, 0, historyBatchSize)}
			}
		}
		if len(batch.Messages) > 0 || (!sentBatch && !chat.LastActivityAt.IsZero()) {
			ready := batch
			s.emit(core.Event{Type: core.EventHistory, History: &ready})
		}
	}
	if skipped > 0 {
		s.client.Log.Warnf("History sync processed %d conversations and %d messages; skipped %d entries", conversationCount, messageCount, skipped)
	}
}

func historyChat(conversation *waHistorySync.Conversation) (core.HistoryChat, types.JID, bool) {
	if conversation == nil {
		return core.HistoryChat{}, types.JID{}, false
	}
	values := []string{conversation.GetID(), conversation.GetNewJID(), conversation.GetOldJID(), conversation.GetPnJID(), conversation.GetLidJID()}
	var primary types.JID
	var aliases []string
	seen := make(map[string]bool)
	for _, value := range values {
		jid, ok := historyJID(value)
		if !ok || seen[jid.String()] {
			continue
		}
		seen[jid.String()] = true
		if primary.IsEmpty() {
			primary = jid
		} else {
			aliases = append(aliases, jid.String())
		}
	}
	if primary.IsEmpty() {
		return core.HistoryChat{}, types.JID{}, false
	}
	timestamp := conversation.GetLastMsgTimestamp()
	if timestamp == 0 {
		timestamp = conversation.GetConversationTimestamp()
	}
	chat := core.HistoryChat{ID: primary.String(), Aliases: aliases, LastActivityAt: historyTimestamp(timestamp)}
	if newest, ok := historyJID(conversation.GetNewJID()); ok {
		chat.PreferredID = newest.String()
	}
	return chat, primary, true
}

func historyJID(value string) (types.JID, bool) {
	if value == "" {
		return types.JID{}, false
	}
	jid, err := types.ParseJID(value)
	if err != nil || jid.IsEmpty() {
		return types.JID{}, false
	}
	return jid.ToNonAD(), true
}

func historyTimestamp(seconds uint64) time.Time {
	if seconds < 946684800 || seconds > 253402300799 {
		return time.Time{}
	}
	return time.Unix(int64(seconds), 0).UTC()
}
