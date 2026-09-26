package v1

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) requestHistory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BeforeMessageID string `json:"before_message_id"`
		Count           int    `json:"count"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	if body.BeforeMessageID == "" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "before_message_id is required.")
		return
	}
	if body.Count == 0 {
		body.Count = 50
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	id, err := s.service.RequestHistory(ctx, r.PathValue("id"), body.BeforeMessageID, body.Count)
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"request_id": id, "status": "requested"})
}
