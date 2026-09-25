package v1

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

type contactResponse struct {
	ProviderID  string `json:"provider_id"`
	Name        string `json:"name"`
	Phone       string `json:"phone,omitempty"`
	MaskedPhone string `json:"masked_phone,omitempty"`
	AvatarURL   string `json:"avatar_url"`
}

func contactFromCore(accountID string, contact core.Contact) contactResponse {
	return contactResponse{ProviderID: contact.ProviderID, Name: contact.Name, Phone: contact.Phone, MaskedPhone: contact.MaskedPhone,
		AvatarURL: "/api/v1/accounts/" + accountID + "/contacts/" + url.PathEscape(contact.ProviderID) + "/avatar"}
}

func (s *Server) listContacts(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeProblem(w, r, http.StatusBadRequest, "invalid_query", "limit must be between 1 and 200.")
			return
		}
		limit = parsed
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > 100 {
		writeProblem(w, r, http.StatusBadRequest, "invalid_query", "q must be at most 100 bytes.")
		return
	}
	var after string
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(decoded) > 1024 || !strings.ContainsRune(string(decoded), 0) {
			writeProblem(w, r, http.StatusBadRequest, "invalid_query", "Invalid contact cursor.")
			return
		}
		after = string(decoded)
	}
	accountID := r.PathValue("id")
	contacts, err := s.service.Contacts(r.Context(), accountID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if query != "" {
		needle := strings.ToLower(query)
		filtered := contacts[:0]
		for _, contact := range contacts {
			if strings.Contains(strings.ToLower(contact.Name), needle) || strings.Contains(contact.Phone, needle) || strings.Contains(strings.ToLower(contact.ProviderID), needle) {
				filtered = append(filtered, contact)
			}
		}
		contacts = filtered
	}
	sort.Slice(contacts, func(i, j int) bool { return contactKey(contacts[i]) < contactKey(contacts[j]) })
	start := sort.Search(len(contacts), func(i int) bool { return contactKey(contacts[i]) > after })
	end := min(start+limit, len(contacts))
	items := make([]contactResponse, 0, end-start)
	for _, contact := range contacts[start:end] {
		items = append(items, contactFromCore(accountID, contact))
	}
	next := ""
	if end < len(contacts) {
		next = base64.RawURLEncoding.EncodeToString([]byte(contactKey(contacts[end-1])))
	}
	writeJSON(w, http.StatusOK, pageResponse[contactResponse]{Items: items, NextCursor: next})
}

func contactKey(contact core.Contact) string {
	return strings.ToLower(contact.Name) + "\x00" + contact.ProviderID
}

func (s *Server) getContact(w http.ResponseWriter, r *http.Request) {
	contact, err := s.service.Contact(r.Context(), r.PathValue("id"), r.PathValue("provider_id"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, contactFromCore(r.PathValue("id"), contact))
}

func (s *Server) getContactAvatar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	accountID, providerID := r.PathValue("id"), r.PathValue("provider_id")
	contact, err := s.service.Contact(r.Context(), accountID, providerID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	s.writeAvatar(w, r, accountID, contact.ProviderID)
}

func (s *Server) getConversationAvatar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	avatar, err := s.service.ConversationAvatar(ctx, r.PathValue("id"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	serveAvatar(w, r, avatar)
}

func (s *Server) writeAvatar(w http.ResponseWriter, r *http.Request, accountID, providerID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	avatar, err := s.service.Avatar(ctx, accountID, providerID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	serveAvatar(w, r, avatar)
}

func serveAvatar(w http.ResponseWriter, r *http.Request, avatar core.Avatar) {
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Type", avatar.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(avatar.Data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(avatar.Data)
	}
}
