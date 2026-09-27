package v1

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func (s *Server) listReceipts(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			writeProblem(w, r, 400, "invalid_query", "limit must be between 1 and 200.")
			return
		}
		limit = n
	}
	raw := r.URL.Query().Get("cursor")
	if len(raw) > 512 {
		writeProblem(w, r, 400, "invalid_query", "Invalid cursor.")
		return
	}
	after, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		writeProblem(w, r, 400, "invalid_query", "Invalid cursor.")
		return
	}
	items, err := s.service.MessageReceipts(r.Context(), r.PathValue("id"), string(after), limit+1)
	if err != nil {
		respondError(w, r, err)
		return
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = base64.RawURLEncoding.EncodeToString([]byte(items[len(items)-1].ParticipantID))
	}
	writeJSON(w, http.StatusOK, pageResponse[core.MessageReceipt]{Items: items, NextCursor: next})
}

func (s *Server) sendReadReceipts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MessageIDs []string `json:"message_ids"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.service.SendReadReceipts(ctx, r.PathValue("id"), body.MessageIDs)
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
