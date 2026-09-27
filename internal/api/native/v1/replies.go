package v1

import (
	"net/http"

	"github.com/notborges/convomeow/internal/core"
)

func messageCursor(m core.Message) *core.PageCursor {
	return &core.PageCursor{Time: m.OccurredAt, ID: m.ID, ProviderOrder: m.ProviderOrder, LocalOrder: m.LocalOrder}
}

func (s *Server) messageWindow(w http.ResponseWriter, r *http.Request, limit int) {
	id := r.PathValue("id")
	around, after := r.URL.Query().Get("around_message_id"), r.URL.Query().Get("after_cursor")
	if r.URL.Query().Get("cursor") != "" || (around != "" && after != "") {
		writeProblem(w, r, http.StatusBadRequest, "invalid_query", "Use only one message position.")
		return
	}
	var messages []core.Message
	var next, previous string
	if around != "" {
		target, err := s.service.Message(r.Context(), around)
		if err != nil {
			respondError(w, r, err)
			return
		}
		if target.ConversationID != id {
			respondError(w, r, core.ErrNotFound)
			return
		}
		// Reserve a slot for the target; both sides use the same stable ordering as normal pagination.
		olderLimit, newerLimit := limit/2, (limit-1)/2
		cursor := messageCursor(target)
		older, err := s.service.ListConversationMessages(r.Context(), id, cursor, olderLimit+1)
		if err != nil {
			respondError(w, r, err)
			return
		}
		cursor.After = true
		newer, err := s.service.ListConversationMessages(r.Context(), id, cursor, newerLimit+1)
		if err != nil {
			respondError(w, r, err)
			return
		}
		if len(older) > olderLimit {
			older = older[:olderLimit]
			last := target
			if len(older) > 0 {
				last = older[len(older)-1]
			}
			next = encodeMessageCursor(last)
		}
		if len(newer) > newerLimit {
			newer = newer[len(newer)-newerLimit:]
			first := target
			if len(newer) > 0 {
				first = newer[0]
			}
			previous = encodeMessageCursor(first)
		}
		messages = append(newer, target)
		messages = append(messages, older...)
	} else {
		cursor, err := decodeCursor(after)
		if err != nil || cursor == nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_query", "Invalid cursor.")
			return
		}
		cursor.After = true
		messages, err = s.service.ListConversationMessages(r.Context(), id, cursor, limit+1)
		if err != nil {
			respondError(w, r, err)
			return
		}
		if len(messages) > limit {
			messages = messages[1:]
			previous = encodeMessageCursor(messages[0])
		}
		if len(messages) > 0 {
			next = encodeMessageCursor(messages[len(messages)-1])
		}
	}
	items := make([]messageResponse, 0, len(messages))
	for _, m := range messages {
		items = append(items, s.messageFromCore(m))
	}
	writeJSON(w, http.StatusOK, struct {
		Items    []messageResponse `json:"items"`
		Next     string            `json:"next_cursor,omitempty"`
		Previous string            `json:"previous_cursor,omitempty"`
	}{items, next, previous})
}
