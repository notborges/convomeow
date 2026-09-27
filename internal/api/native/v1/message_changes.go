package v1

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func (s *Server) editMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	s.changeMessage(w, r, "edit", body.Text)
}
func (s *Server) revokeMessage(w http.ResponseWriter, r *http.Request) {
	s.changeMessage(w, r, "revoke", "")
}
func (s *Server) changeMessage(w http.ResponseWriter, r *http.Request, kind, text string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	message, err := s.service.ChangeMessage(ctx, r.PathValue("id"), kind, text)
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.messageFromCore(message))
}

func (s *Server) messageRevisions(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			writeProblem(w, r, 400, "invalid_query", "limit must be between 1 and 200.")
			return
		}
		limit = n
	}
	var cursor *core.PageCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var value cursorValue
		data, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(raw) > 512 || json.Unmarshal(data, &value) != nil {
			writeProblem(w, r, 400, "invalid_query", "Invalid cursor.")
			return
		}
		at, timeErr := time.Parse(time.RFC3339Nano, value.At)
		n, idErr := strconv.ParseInt(value.ID, 10, 64)
		if timeErr != nil || idErr != nil || n < 0 {
			writeProblem(w, r, 400, "invalid_query", "Invalid cursor.")
			return
		}
		cursor = &core.PageCursor{Time: at, ID: value.ID}
	}
	items, err := s.service.MessageRevisions(r.Context(), r.PathValue("id"), cursor, limit+1)
	if err != nil {
		respondError(w, r, err)
		return
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodeCursor(last.At, last.ID)
	}
	writeJSON(w, http.StatusOK, pageResponse[core.MessageRevision]{Items: items, NextCursor: next})
}
