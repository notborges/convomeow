package v1

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func (s *Server) setReaction(w http.ResponseWriter, r *http.Request) {
	emoji := ""
	if r.Method == http.MethodPut {
		var body struct {
			Emoji string `json:"emoji"`
		}
		if !decodeRequest(w, r, &body) {
			return
		}
		if !core.ValidReactionEmoji(body.Emoji) {
			writeProblem(w, r, 400, "invalid_input", "Provide one emoji.")
			return
		}
		emoji = body.Emoji
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	message, err := s.service.SetReaction(ctx, r.PathValue("id"), emoji)
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.messageFromCore(message))
}

func (s *Server) listReactions(w http.ResponseWriter, r *http.Request) {
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
	after, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(raw) > 512 {
		writeProblem(w, r, 400, "invalid_query", "Invalid cursor.")
		return
	}
	items, err := s.service.MessageReactions(r.Context(), r.PathValue("id"), string(after), limit+1)
	if err != nil {
		respondError(w, r, err)
		return
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = base64.RawURLEncoding.EncodeToString([]byte(items[len(items)-1].ParticipantID))
	}
	message, err := s.service.Message(r.Context(), r.PathValue("id"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	type reactionResponse struct {
		core.MessageReaction
		AvatarURL string `json:"avatar_url,omitempty"`
	}
	responses := make([]reactionResponse, 0, len(items))
	for _, item := range items {
		path := "/api/v1/accounts/" + message.AccountID + "/contacts/" + url.PathEscape(item.ParticipantID) + "/avatar"
		if item.IsOwn {
			path = "/api/v1/accounts/" + message.AccountID + "/avatar"
		}
		responses = append(responses, reactionResponse{MessageReaction: item, AvatarURL: s.avatarURL(message.AccountID, path)})
	}
	writeJSON(w, 200, pageResponse[reactionResponse]{Items: responses, NextCursor: next})
}
