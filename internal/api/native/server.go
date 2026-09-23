package native

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/notborges/convomeow/internal/app"
	"github.com/notborges/convomeow/internal/core"
)

type Server struct {
	service *app.Service
	token   string
}

func New(service *app.Service, token string) http.Handler {
	s := &Server{service: service, token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v1/accounts", s.listAccounts)
	mux.HandleFunc("POST /api/v1/accounts", s.createAccount)
	mux.HandleFunc("GET /api/v1/accounts/{id}", s.getAccount)
	mux.HandleFunc("POST /api/v1/accounts/{id}/login", s.startLogin)
	mux.HandleFunc("GET /api/v1/accounts/{id}/login", s.loginStatus)
	mux.HandleFunc("POST /api/v1/accounts/{id}/messages", s.sendMessage)
	mux.HandleFunc("GET /api/v1/accounts/{id}/messages", s.listMessages)
	mux.HandleFunc("GET /api/v1/accounts/{id}/chats", s.listChats)
	mux.HandleFunc("GET /api/v1/accounts/{id}/chats/{chat_id}/messages", s.listChatMessages)
	return s.authenticate(mux)
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		const prefix = "Bearer "
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, prefix) || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(authorization, prefix)), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label string `json:"label"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	account, err := s.service.CreateAccount(r.Context(), body.Label)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, account)
}

func (s *Server) listAccounts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.service.ListAccounts())
}

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request) {
	account, err := s.service.Account(r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (s *Server) startLogin(w http.ResponseWriter, r *http.Request) {
	status, err := s.service.StartLogin(r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, status)
}

func (s *Server) loginStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.service.LoginStatus(r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		To   string `json:"to"`
		Text string `json:"text"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	message, err := s.service.SendText(ctx, r.PathValue("id"), body.To, body.Text)
	if errors.Is(err, core.ErrSentUnrecorded) {
		writeJSON(w, http.StatusAccepted, map[string]any{"message": message, "stored": false, "warning": "sent, but local storage failed"})
		return
	}
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, message)
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	after, err := parseIntQuery(r, "after", 0)
	if err != nil || after < 0 {
		writeError(w, http.StatusBadRequest, "invalid_query", "after must be a nonnegative integer")
		return
	}
	limit, err := parseIntQuery(r, "limit", 100)
	if err != nil || limit < 1 {
		writeError(w, http.StatusBadRequest, "invalid_query", "limit must be a positive integer")
		return
	}
	if limit > 500 {
		limit = 500
	}
	messages, err := s.service.ListMessages(r.Context(), r.PathValue("id"), after, int(limit))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, messages)
}

func (s *Server) listChats(w http.ResponseWriter, r *http.Request) {
	before, limit, ok := parseRecentPage(w, r)
	if !ok {
		return
	}
	chats, err := s.service.ListChats(r.Context(), r.PathValue("id"), before, limit)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, chats)
}

func (s *Server) listChatMessages(w http.ResponseWriter, r *http.Request) {
	before, limit, ok := parseRecentPage(w, r)
	if !ok {
		return
	}
	messages, err := s.service.ListChatMessages(r.Context(), r.PathValue("id"), r.PathValue("chat_id"), before, limit)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, messages)
}

func parseRecentPage(w http.ResponseWriter, r *http.Request) (int64, int, bool) {
	before, err := parseIntQuery(r, "before", 0)
	if err != nil || before < 0 {
		writeError(w, http.StatusBadRequest, "invalid_query", "before must be a nonnegative integer")
		return 0, 0, false
	}
	limit, err := parseIntQuery(r, "limit", 100)
	if err != nil || limit < 1 {
		writeError(w, http.StatusBadRequest, "invalid_query", "limit must be a positive integer")
		return 0, 0, false
	}
	if limit > 500 {
		limit = 500
	}
	return before, int(limit), true
}

func parseIntQuery(r *http.Request, key string, fallback int64) (int64, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback, nil
	}
	return strconv.ParseInt(v, 10, 64)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return errors.New("request must contain one JSON object")
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func respondError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, core.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, core.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
	case errors.Is(err, core.ErrNotConnected):
		writeError(w, http.StatusConflict, "not_connected", err.Error())
	case errors.Is(err, core.ErrOutcomeUnknown):
		writeError(w, http.StatusGatewayTimeout, "outcome_unknown", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}
