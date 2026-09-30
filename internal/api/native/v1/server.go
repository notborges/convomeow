package v1

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/notborges/convomeow/internal/app"
	"github.com/notborges/convomeow/internal/core"
	"github.com/notborges/convomeow/internal/notifications"
)

type Server struct {
	authorized     func(*http.Request) bool
	service        *app.Service
	browserSession func(*http.Request) (notifications.Session, bool)
}

type BrowserAuth struct {
	Authenticated func(*http.Request) bool
	Session       func(*http.Request) (notifications.Session, bool)
}

func New(service *app.Service, token string, browser *BrowserAuth) http.Handler {
	s := &Server{service: service}
	var browserAuth func(*http.Request) bool
	if browser != nil {
		browserAuth = browser.Authenticated
		s.browserSession = browser.Session
	}
	s.authorized = func(r *http.Request) bool { return authorized(token, browserAuth, r) }
	mux := http.NewServeMux()
	register(mux, "/api/v1/notifications/config", map[string]http.HandlerFunc{"GET": s.notificationConfig})
	register(mux, "/api/v1/notifications/subscriptions", map[string]http.HandlerFunc{"POST": s.registerNotificationSubscription})
	register(mux, "/api/v1/notifications/subscriptions/{id}", map[string]http.HandlerFunc{"DELETE": s.deleteNotificationSubscription})
	register(mux, "/api/v1/messages/{id}/revisions", map[string]http.HandlerFunc{"GET": s.messageRevisions})
	register(mux, "/api/v1/messages/{id}/revoke", map[string]http.HandlerFunc{"POST": s.revokeMessage})
	register(mux, "/api/v1/conversations/{id}/presence", map[string]http.HandlerFunc{"POST": s.sendPresence})
	mux.HandleFunc("GET /api/v1/events", s.events)
	register(mux, "/api/v1/messages/{id}/reaction", map[string]http.HandlerFunc{"PUT": s.setReaction, "DELETE": s.setReaction})
	register(mux, "/api/v1/messages/{id}/reactions", map[string]http.HandlerFunc{"GET": s.listReactions})
	register(mux, "/api/v1/messages/{id}/receipts", map[string]http.HandlerFunc{"GET": s.listReceipts})
	register(mux, "/api/v1/conversations/{id}/read-receipts", map[string]http.HandlerFunc{"POST": s.sendReadReceipts})
	register(mux, "/api/v1/conversations/{id}/history-requests", map[string]http.HandlerFunc{"POST": s.requestHistory})
	register(mux, "/api/v1/accounts", map[string]http.HandlerFunc{"GET": s.listAccounts, "POST": s.createAccount})
	register(mux, "/api/v1/accounts/{id}", map[string]http.HandlerFunc{"GET": s.getAccount})
	register(mux, "/api/v1/accounts/{id}/avatar", map[string]http.HandlerFunc{"GET": s.getAccountAvatar})
	register(mux, "/api/v1/accounts/{id}/login-attempts", map[string]http.HandlerFunc{"POST": s.startLogin})
	register(mux, "/api/v1/accounts/{id}/login-attempts/{attempt_id}", map[string]http.HandlerFunc{"GET": s.getLogin})
	register(mux, "/api/v1/accounts/{id}/conversations", map[string]http.HandlerFunc{"POST": s.createConversation})
	register(mux, "/api/v1/accounts/{id}/contacts", map[string]http.HandlerFunc{"GET": s.listContacts})
	register(mux, "/api/v1/accounts/{id}/contacts/{provider_id}", map[string]http.HandlerFunc{"GET": s.getContact})
	register(mux, "/api/v1/accounts/{id}/contacts/{provider_id}/avatar", map[string]http.HandlerFunc{"GET": s.getContactAvatar})
	register(mux, "/api/v1/accounts/{id}/uploads", map[string]http.HandlerFunc{"POST": s.createUpload})
	register(mux, "/api/v1/accounts/{id}/uploads/{upload_id}", map[string]http.HandlerFunc{"DELETE": s.deleteUpload})
	register(mux, "/api/v1/conversations", map[string]http.HandlerFunc{"GET": s.listConversations})
	register(mux, "/api/v1/conversations/{id}", map[string]http.HandlerFunc{"GET": s.getConversation})
	register(mux, "/api/v1/conversations/{id}/avatar", map[string]http.HandlerFunc{"GET": s.getConversationAvatar})
	register(mux, "/api/v1/conversations/{id}/messages", map[string]http.HandlerFunc{"GET": s.listConversationMessages, "POST": s.sendMessage})
	register(mux, "/api/v1/messages", map[string]http.HandlerFunc{"GET": s.listMessages})
	register(mux, "/api/v1/messages/{id}", map[string]http.HandlerFunc{"GET": s.getMessage, "PATCH": s.editMessage})
	register(mux, "/api/v1/attachments/{id}", map[string]http.HandlerFunc{"GET": s.getAttachment})
	register(mux, "/api/v1/attachments/{id}/content", map[string]http.HandlerFunc{"GET": s.getAttachmentContent})
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusNotFound, "not_found", "Resource not found.")
	})
	return withRequestID(authenticate(token, browserAuth, mux))
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
	writeJSON(w, status, s.conversationFromCore(conversation))
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
		items = append(items, s.conversationFromCore(conversation))
	}
	writeJSON(w, http.StatusOK, pageResponse[conversationResponse]{Items: items, NextCursor: next})
}

func (s *Server) getConversation(w http.ResponseWriter, r *http.Request) {
	conversation, err := s.service.Conversation(r.Context(), r.PathValue("id"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.conversationFromCore(conversation))
}

func (s *Server) listConversationMessages(w http.ResponseWriter, r *http.Request) {
	limit, cursor, ok := parsePage(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("around_message_id") != "" || r.URL.Query().Get("after_cursor") != "" {
		s.messageWindow(w, r, limit)
		return
	}
	messages, err := s.service.ListConversationMessages(r.Context(), r.PathValue("id"), cursor, limit+1)
	if err != nil {
		respondError(w, r, err)
		return
	}
	s.writeMessagesPage(w, messages, limit, cursor != nil)
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ReplyID string `json:"reply_to_message_id"`
		Kind    string `json:"kind"`
		Content struct {
			Text     string `json:"text"`
			UploadID string `json:"upload_id"`
			Caption  string `json:"caption"`
		} `json:"content"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	if body.Kind == "" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_input", "kind is required.")
		return
	}
	key := r.Header.Get("Idempotency-Key")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var message core.Message
	var err error
	status := http.StatusCreated
	if body.Kind == "text" {
		if body.Content.UploadID != "" || body.Content.Caption != "" {
			writeProblem(w, r, http.StatusBadRequest, "invalid_input", "Text messages accept only content.text.")
			return
		}
		message, err = s.service.SendTextReply(ctx, r.PathValue("id"), body.Content.Text, key, body.ReplyID)
	} else {
		if body.Content.Text != "" || body.Content.UploadID == "" {
			writeProblem(w, r, http.StatusBadRequest, "invalid_input", "Media messages require content.upload_id.")
			return
		}
		message, err = s.service.SendMediaReply(ctx, r.PathValue("id"), core.MessageKind(body.Kind), body.Content.UploadID, body.Content.Caption, key, body.ReplyID)
		status = http.StatusAccepted
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/messages/"+message.ID)
	if status == http.StatusAccepted {
		w.Header().Set("Retry-After", "2")
	}
	writeJSON(w, status, s.messageFromCore(message))
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
	s.writeMessagesPage(w, messages, limit, false)
}

func (s *Server) getMessage(w http.ResponseWriter, r *http.Request) {
	message, err := s.service.Message(r.Context(), r.PathValue("id"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.messageFromCore(message))
}

func (s *Server) writeMessagesPage(w http.ResponseWriter, messages []core.Message, limit int, hasNewer bool) {
	next := ""
	if len(messages) > limit {
		messages = messages[:limit]
		last := messages[len(messages)-1]
		next = encodeMessageCursor(last)
	}
	items := make([]messageResponse, 0, len(messages))
	for _, message := range messages {
		items = append(items, s.messageFromCore(message))
	}
	previous := ""
	if hasNewer && len(messages) > 0 {
		previous = encodeMessageCursor(messages[0])
	}
	writeJSON(w, http.StatusOK, pageResponse[messageResponse]{Items: items, NextCursor: next, PreviousCursor: previous})
}
