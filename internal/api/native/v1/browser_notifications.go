package v1

import (
	"net/http"

	"github.com/notborges/convomeow/internal/core"
	"github.com/notborges/convomeow/internal/notifications"
)

func (s *Server) notificationConfig(w http.ResponseWriter, _ *http.Request) {
	enabled, key := s.service.NotificationConfig()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		Enabled   bool   `json:"enabled"`
		PublicKey string `json:"public_key,omitempty"`
	}{enabled, key})
}

func (s *Server) notificationOwner(w http.ResponseWriter, r *http.Request) (notifications.Session, bool) {
	if s.browserSession != nil {
		if owner, ok := s.browserSession(r); ok {
			return owner, true
		}
	}
	writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "A browser session is required.")
	return notifications.Session{}, false
}

func (s *Server) registerNotificationSubscription(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.notificationOwner(w, r)
	if !ok {
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256DH string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
		Locale  string `json:"locale"`
		Preview *bool  `json:"preview"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if !decodeRequest(w, r, &body) {
		return
	}
	if body.Preview == nil {
		respondError(w, r, core.ErrInvalid)
		return
	}
	sub := notifications.Subscription{Endpoint: body.Endpoint, Keys: body.Keys, Locale: body.Locale, Preview: *body.Preview, Session: owner}
	sub, err := s.service.RegisterNotificationSubscription(r.Context(), sub)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		ID string `json:"id"`
	}{sub.ID})
}

func (s *Server) deleteNotificationSubscription(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.notificationOwner(w, r)
	if !ok {
		return
	}
	if err := s.service.DeleteNotificationSubscription(r.Context(), r.PathValue("id"), owner); err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
