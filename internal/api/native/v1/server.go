package v1

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/notborges/convomeow/internal/app"
	"github.com/notborges/convomeow/internal/core"
)

type Server struct {
	service *app.Service
}

func New(service *app.Service, token string) http.Handler {
	s := &Server{service: service}
	mux := http.NewServeMux()
	register(mux, "/api/v1/accounts", map[string]http.HandlerFunc{"GET": s.listAccounts, "POST": s.createAccount})
	register(mux, "/api/v1/accounts/{id}", map[string]http.HandlerFunc{"GET": s.getAccount})
	register(mux, "/api/v1/accounts/{id}/login-attempts", map[string]http.HandlerFunc{"POST": s.startLogin})
	register(mux, "/api/v1/accounts/{id}/login-attempts/{attempt_id}", map[string]http.HandlerFunc{"GET": s.getLogin})
	register(mux, "/api/v1/accounts/{id}/conversations", map[string]http.HandlerFunc{"POST": s.createConversation})
	register(mux, "/api/v1/conversations", map[string]http.HandlerFunc{"GET": s.listConversations})
	register(mux, "/api/v1/conversations/{id}", map[string]http.HandlerFunc{"GET": s.getConversation})
	register(mux, "/api/v1/conversations/{id}/messages", map[string]http.HandlerFunc{"GET": s.listConversationMessages, "POST": s.sendMessage})
	register(mux, "/api/v1/messages", map[string]http.HandlerFunc{"GET": s.listMessages})
	register(mux, "/api/v1/messages/{id}", map[string]http.HandlerFunc{"GET": s.getMessage})
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusNotFound, "not_found", "Resource not found.")
	})
	return withRequestID(authenticate(token, mux))
}

func register(mux *http.ServeMux, pattern string, handlers map[string]http.HandlerFunc) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		method := r.Method
		if method == http.MethodHead {
			method = http.MethodGet
		}
		if handler := handlers[method]; handler != nil {
			handler(w, r)
			return
		}
		allowed := make([]string, 0, len(handlers))
		for method := range handlers {
			allowed = append(allowed, method)
		}
		sort.Strings(allowed)
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		writeProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method is not allowed for this route.")
	})
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label          string `json:"label"`
		Provider       string `json:"provider"`
		ConnectionKind string `json:"connection_kind"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	account, err := s.service.CreateAccount(r.Context(), body.Label, body.Provider, body.ConnectionKind)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/accounts/"+account.ID)
	writeJSON(w, http.StatusCreated, accountFromCore(account))
}

func (s *Server) listAccounts(w http.ResponseWriter, _ *http.Request) {
	accounts := s.service.ListAccounts()
	items := make([]accountResponse, 0, len(accounts))
	for _, account := range accounts {
		items = append(items, accountFromCore(account))
	}
	writeJSON(w, http.StatusOK, pageResponse[accountResponse]{Items: items})
}

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request) {
	account, err := s.service.Account(r.PathValue("id"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, accountFromCore(account))
}

func (s *Server) startLogin(w http.ResponseWriter, r *http.Request) {
	status, err := s.service.StartLogin(r.PathValue("id"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/accounts/"+r.PathValue("id")+"/login-attempts/"+status.ID)
	writeJSON(w, http.StatusAccepted, loginAttemptFromCore(status))
}

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	status, err := s.service.LoginStatus(r.PathValue("id"), r.PathValue("attempt_id"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, loginAttemptFromCore(status))
}

func (s *Server) createConversation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"target"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	conversation, created, err := s.service.CreateConversation(r.Context(), r.PathValue("id"), core.ConversationTarget{Type: body.Target.Type, Value: body.Target.Value})
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/conversations/"+conversation.ID)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, conversationFromCore(conversation))
}

func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	limit, cursor, ok := parsePage(w, r)
	if !ok {
		return
	}
	conversations, err := s.service.ListConversations(r.Context(), r.URL.Query().Get("account_id"), cursor, limit+1)
	if err != nil {
		respondError(w, r, err)
		return
	}
	next := ""
	if len(conversations) > limit {
		conversations = conversations[:limit]
		last := conversations[len(conversations)-1]
		next = encodeCursor(last.UpdatedAt, last.ID)
	}
	items := make([]conversationResponse, 0, len(conversations))
	for _, conversation := range conversations {
		items = append(items, conversationFromCore(conversation))
	}
	writeJSON(w, http.StatusOK, pageResponse[conversationResponse]{Items: items, NextCursor: next})
}

func (s *Server) getConversation(w http.ResponseWriter, r *http.Request) {
	conversation, err := s.service.Conversation(r.Context(), r.PathValue("id"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationFromCore(conversation))
}

func (s *Server) listConversationMessages(w http.ResponseWriter, r *http.Request) {
	limit, cursor, ok := parsePage(w, r)
	if !ok {
		return
	}
	messages, err := s.service.ListConversationMessages(r.Context(), r.PathValue("id"), cursor, limit+1)
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeMessagesPage(w, messages, limit)
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind    string `json:"kind"`
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	if body.Kind == "" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "kind is required.")
		return
	}
	if body.Kind != "text" {
		writeProblem(w, r, http.StatusUnprocessableEntity, "unsupported_capability", "Only text sending is available.")
		return
	}
	key := r.Header.Get("Idempotency-Key")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	message, err := s.service.SendText(ctx, r.PathValue("id"), body.Content.Text, key)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/messages/"+message.ID)
	writeJSON(w, http.StatusCreated, messageFromCore(message))
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("account_id")
	if accountID == "" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_query", "account_id is required.")
		return
	}
	limit, cursor, ok := parsePage(w, r)
	if !ok {
		return
	}
	messages, err := s.service.ListMessages(r.Context(), accountID, cursor, limit+1)
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeMessagesPage(w, messages, limit)
}

func (s *Server) getMessage(w http.ResponseWriter, r *http.Request) {
	message, err := s.service.Message(r.Context(), r.PathValue("id"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, messageFromCore(message))
}

func writeMessagesPage(w http.ResponseWriter, messages []core.Message, limit int) {
	next := ""
	if len(messages) > limit {
		messages = messages[:limit]
		last := messages[len(messages)-1]
		next = encodeCursor(last.OccurredAt, last.ID)
	}
	items := make([]messageResponse, 0, len(messages))
	for _, message := range messages {
		items = append(items, messageFromCore(message))
	}
	writeJSON(w, http.StatusOK, pageResponse[messageResponse]{Items: items, NextCursor: next})
}
