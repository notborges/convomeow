package v1

import (
	"context"
	"net/http"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func (s *Server) sendPresence(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClientID string            `json:"client_id"`
		Activity core.ChatActivity `json:"activity"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := s.service.SendChatPresence(ctx, r.PathValue("id"), body.ClientID, body.Activity); err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
