package v1

import (
	"context"
	"net/http"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

const groupTimeout = 30 * time.Second

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string   `json:"name"`
		Participants []string `json:"participants"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), groupTimeout)
	defer cancel()
	group, err := s.service.CreateGroup(ctx, r.PathValue("id"), body.Name, body.Participants)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/conversations/"+group.ConversationID+"/group")
	writeJSON(w, http.StatusCreated, group)
}

func (s *Server) getGroup(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), groupTimeout)
	defer cancel()
	group, err := s.service.Group(ctx, r.PathValue("id"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, group)
}

func (s *Server) updateGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           *string `json:"name"`
		Description    *string `json:"description"`
		AdminsOnlySend *bool   `json:"admins_only_send"`
		AdminsOnlyEdit *bool   `json:"admins_only_edit"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), groupTimeout)
	defer cancel()
	group, err := s.service.UpdateGroup(ctx, r.PathValue("id"), core.GroupUpdate{Name: body.Name, Description: body.Description,
		AdminsOnlySend: body.AdminsOnlySend, AdminsOnlyEdit: body.AdminsOnlyEdit})
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, group)
}

func (s *Server) changeGroupParticipants(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action       string   `json:"action"`
		Participants []string `json:"participants"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), groupTimeout)
	defer cancel()
	results, err := s.service.ChangeGroupParticipants(ctx, r.PathValue("id"), core.GroupParticipantAction(body.Action), body.Participants)
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageResponse[core.GroupParticipantResult]{Items: results})
}

func (s *Server) leaveGroup(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), groupTimeout)
	defer cancel()
	if err := s.service.LeaveGroup(ctx, r.PathValue("id")); err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) groupInviteLink(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), groupTimeout)
	defer cancel()
	link, err := s.service.GroupInviteLink(ctx, r.PathValue("id"), r.Method == http.MethodPost)
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": link})
}
