package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/core"
)

type runtimeAccount struct {
	mu         sync.RWMutex
	eventMu    sync.Mutex
	generation uint64
	account    core.Account
	state      string
	lastError  string
	loginState string
	loginID    string
	challenge  *core.LoginChallenge
	session    core.Session
}

type Service struct {
	repo      core.Repository
	connector core.Connector
	logger    *slog.Logger

	mu       sync.RWMutex
	accounts map[string]*runtimeAccount

	loginMu     sync.Mutex
	activeLogin string

	eventMu sync.Mutex
	closing bool
	eventWG sync.WaitGroup
	workWG  sync.WaitGroup
	cancel  context.CancelFunc
	ctx     context.Context
	media   mediaState
	avatars avatarState
}

func New(repo core.Repository, connector core.Connector, logger *slog.Logger) *Service {
	return NewWithMedia(repo, connector, logger, MediaOptions{})
}

func NewWithMedia(repo core.Repository, connector core.Connector, logger *slog.Logger, options MediaOptions) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{repo: repo, connector: connector, logger: logger, accounts: make(map[string]*runtimeAccount), ctx: ctx, cancel: cancel}
	if options.Stores != nil {
		s.media = mediaState{options: options, queue: make(chan string, max(options.Workers, 1)*4),
			sendQueue: make(chan string, max(options.Workers, 1)*4), wake: make(chan struct{}, 1),
			inflight: make(map[string]bool), sendInflight: make(map[string]bool)}
		s.avatars = avatarState{queue: make(chan avatarJob, 256), scans: make(chan string, 32), queued: make(map[string]bool)}
	}
	return s
}

func (s *Service) Start(ctx context.Context) error {
	accounts, err := s.repo.ListAccounts(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	for _, account := range accounts {
		rt := &runtimeAccount{account: account, state: "needs_login", loginState: "idle", generation: 1}
		if account.ProviderIdentity != "" {
			rt.state = "connecting"
		}
		s.accounts[account.ID] = rt
	}
	s.mu.Unlock()
	if s.media.options.Stores != nil {
		if err := s.startMediaWorkers(); err != nil {
			return err
		}
		s.startAvatarWorkers()
	}
	for _, account := range accounts {
		if account.ProviderIdentity == "" {
			continue
		}
		rt := s.accounts[account.ID]
		session, err := s.connector.Open(ctx, account.ProviderIdentity, func(event core.Event) { s.onEvent(account.ID, 1, event) })
		if err != nil {
			rt.setError(fmt.Errorf("restore session: %w", err))
			continue
		}
		rt.mu.Lock()
		rt.session = session
		rt.mu.Unlock()
		s.workWG.Add(1)
		go func(id string, session core.Session, rt *runtimeAccount) {
			defer s.workWG.Done()
			if err := session.Connect(); err != nil {
				rt.setError(err)
				s.logger.Error("connect failed", "account_id", id, "error", err)
			}
		}(account.ID, session, rt)
	}
	return nil
}

func (s *Service) Close() error {
	s.cancel()
	s.mu.RLock()
	var sessions []core.Session
	for _, rt := range s.accounts {
		rt.mu.RLock()
		if rt.session != nil {
			sessions = append(sessions, rt.session)
		}
		rt.mu.RUnlock()
	}
	s.mu.RUnlock()
	for _, session := range sessions {
		session.Close()
	}
	s.workWG.Wait()
	s.eventMu.Lock()
	s.closing = true
	s.eventMu.Unlock()
	s.eventWG.Wait()
	connectorErr := s.connector.Close()
	repoErr := s.repo.Close()
	return errors.Join(connectorErr, repoErr)
}

func (s *Service) CreateAccount(ctx context.Context, label, provider, connectionKind string) (core.AccountStatus, error) {
	if provider != core.ProviderWhatsApp || connectionKind != core.ConnectionKindLinkedDevice {
		return core.AccountStatus{}, fmt.Errorf("%w: unsupported provider or connection kind", core.ErrInvalid)
	}
	label = strings.TrimSpace(label)
	if len(label) < 1 || len(label) > 64 {
		return core.AccountStatus{}, fmt.Errorf("%w: label must be 1-64 characters", core.ErrInvalid)
	}
	for _, r := range label {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return core.AccountStatus{}, fmt.Errorf("%w: label may contain letters, digits, - and _", core.ErrInvalid)
		}
	}
	now := time.Now().UTC()
	a := core.Account{ID: uuid.NewString(), Provider: provider, ConnectionKind: connectionKind, Label: label, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.CreateAccount(ctx, a); err != nil {
		return core.AccountStatus{}, err
	}
	s.mu.Lock()
	s.accounts[a.ID] = &runtimeAccount{account: a, state: "needs_login", loginState: "idle"}
	s.mu.Unlock()
	return core.AccountStatus{Account: a, State: "needs_login"}, nil
}

func (s *Service) ListAccounts() []core.AccountStatus {
	s.mu.RLock()
	runtimes := make([]*runtimeAccount, 0, len(s.accounts))
	for _, rt := range s.accounts {
		runtimes = append(runtimes, rt)
	}
	s.mu.RUnlock()
	result := make([]core.AccountStatus, 0, len(runtimes))
	for _, rt := range runtimes {
		result = append(result, rt.status())
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Label < result[j].Label })
	return result
}

func (s *Service) Account(id string) (core.AccountStatus, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return core.AccountStatus{}, err
	}
	return rt.status(), nil
}

func (s *Service) LoginStatus(id, attemptID string) (core.LoginStatus, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return core.LoginStatus{}, err
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	if attemptID == "" || rt.loginID != attemptID {
		return core.LoginStatus{}, core.ErrNotFound
	}
	state := rt.loginState
	if rt.state == "connected" {
		state = "connected"
	}
	return core.LoginStatus{ID: rt.loginID, State: state, Challenge: rt.challenge, Error: rt.lastError}, nil
}

func (s *Service) StartLogin(id string) (core.LoginStatus, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return core.LoginStatus{}, err
	}
	s.loginMu.Lock()
	if s.activeLogin != "" {
		s.loginMu.Unlock()
		return core.LoginStatus{}, fmt.Errorf("%w: another login is active", core.ErrConflict)
	}
	rt.mu.RLock()
	paired := rt.account.ProviderIdentity != ""
	rt.mu.RUnlock()
	if paired {
		s.loginMu.Unlock()
		return core.LoginStatus{}, fmt.Errorf("%w: account already paired", core.ErrConflict)
	}
	s.activeLogin = id
	s.loginMu.Unlock()
	clearCtx, clearCancel := context.WithTimeout(s.ctx, 10*time.Second)
	clearErr := s.clearAccountAvatars(clearCtx, id)
	clearCancel()
	if clearErr != nil {
		s.loginMu.Lock()
		s.activeLogin = ""
		s.loginMu.Unlock()
		return core.LoginStatus{}, fmt.Errorf("clear account avatars: %w", clearErr)
	}
	rt.mu.Lock()
	rt.generation++
	generation := rt.generation
	rt.mu.Unlock()
	session, err := s.connector.New(func(event core.Event) { s.onEvent(id, generation, event) })
	if err != nil {
		s.loginMu.Lock()
		s.activeLogin = ""
		s.loginMu.Unlock()
		return core.LoginStatus{}, err
	}
	attemptID := uuid.NewString()
	rt.mu.Lock()
	rt.loginID = attemptID
	rt.session = session
	rt.state = "pairing"
	rt.loginState = "waiting_for_challenge"
	rt.lastError = ""
	rt.challenge = nil
	rt.mu.Unlock()
	s.workWG.Add(1)
	go func() {
		defer s.workWG.Done()
		loginCtx, cancel := context.WithTimeout(s.ctx, 3*time.Minute)
		defer cancel()
		err := session.Login(loginCtx, func(challenge core.LoginChallenge) {
			rt.mu.Lock()
			if rt.generation == generation && rt.session == session {
				rt.challenge = &challenge
				rt.loginState = "challenge_available"
			}
			rt.mu.Unlock()
		})
		s.finishLogin(id, rt, session, err)
	}()
	return core.LoginStatus{ID: attemptID, State: "waiting_for_challenge"}, nil
}

func (s *Service) finishLogin(id string, rt *runtimeAccount, session core.Session, loginErr error) {
	s.loginMu.Lock()
	if s.activeLogin == id {
		s.activeLogin = ""
	}
	s.loginMu.Unlock()
	rt.eventMu.Lock()
	rt.mu.RLock()
	paired, current := rt.account.ProviderIdentity != "", rt.session == session
	rt.mu.RUnlock()
	if !current {
		rt.eventMu.Unlock()
		session.Close()
		return
	}
	if !paired && session != nil && session.Identity() != "" {
		if err := s.persistIdentity(id, session.Identity()); err != nil {
			loginErr = errors.Join(loginErr, err)
		} else {
			paired = true
		}
	}
	if loginErr != nil {
		rt.mu.Lock()
		if !paired {
			rt.session = nil
			rt.generation++
		}
		rt.state = "error"
		rt.loginState = "failed"
		rt.lastError = loginErr.Error()
		rt.challenge = nil
		rt.mu.Unlock()
		rt.eventMu.Unlock()
		if !paired && session != nil {
			session.Close()
		}
		s.logger.Error("login failed", "account_id", id, "error", loginErr)
		return
	}
	rt.mu.Lock()
	if rt.state != "connected" {
		rt.state = "connecting"
		rt.loginState = "connecting"
	}
	rt.challenge = nil
	rt.mu.Unlock()
	rt.eventMu.Unlock()
}

func (s *Service) CreateConversation(ctx context.Context, accountID string, target core.ConversationTarget) (core.Conversation, bool, error) {
	if _, err := s.runtime(accountID); err != nil {
		return core.Conversation{}, false, err
	}
	var chatID string
	if target.Type == "contact" {
		contact, err := s.Contact(ctx, accountID, target.Value)
		if err != nil {
			return core.Conversation{}, false, err
		}
		chatID = contact.ProviderID
		if contact.AlternateID != "" {
			if err := s.repo.LinkChats(ctx, accountID, core.ChatLink{First: chatID, Second: contact.AlternateID}); err != nil {
				return core.Conversation{}, false, err
			}
		}
	} else {
		var err error
		chatID, err = s.connector.ResolveTarget(target)
		if err != nil {
			return core.Conversation{}, false, err
		}
		contact, err := s.Contact(ctx, accountID, chatID)
		if err != nil && !errors.Is(err, core.ErrNotFound) && !errors.Is(err, core.ErrNotConnected) {
			return core.Conversation{}, false, err
		}
		if err == nil && contact.AlternateID != "" {
			if err := s.repo.LinkChats(ctx, accountID, core.ChatLink{First: chatID, Second: contact.AlternateID}); err != nil {
				return core.Conversation{}, false, err
			}
		}
	}
	conversation, created, err := s.repo.GetOrCreateConversation(ctx, accountID, chatID)
	if err != nil {
		return core.Conversation{}, false, err
	}
	if created && target.Type == "phone_number" {
		name := "+" + strings.TrimPrefix(strings.TrimSpace(target.Value), "+")
		if err := s.repo.UpdateChatProfile(ctx, accountID, core.ChatProfile{ProviderChatID: chatID, Kind: "direct", DisplayName: &name}); err != nil {
			return core.Conversation{}, false, err
		}
		conversation.DisplayName = name
	}
	s.enrichConversation(ctx, &conversation)
	return conversation, created, nil
}

func (s *Service) Conversation(ctx context.Context, id string) (core.Conversation, error) {
	conversation, err := s.repo.GetConversation(ctx, id)
	if err != nil {
		return core.Conversation{}, err
	}
	s.enrichConversation(ctx, &conversation)
	return conversation, nil
}

func (s *Service) ListConversations(ctx context.Context, accountID string, before *core.PageCursor, limit int) ([]core.Conversation, error) {
	if accountID != "" {
		if _, err := s.runtime(accountID); err != nil {
			return nil, err
		}
	}
	if err := validatePage(limit); err != nil {
		return nil, err
	}
	conversations, err := s.repo.ListConversations(ctx, accountID, before, limit)
	if err != nil {
		return nil, err
	}
	for i := range conversations {
		s.enrichConversation(ctx, &conversations[i])
	}
	return conversations, nil
}

func (s *Service) enrichConversation(ctx context.Context, conversation *core.Conversation) {
	if conversation.Kind != "direct" {
		return
	}
	rt, err := s.runtime(conversation.AccountID)
	if err != nil {
		return
	}
	rt.mu.RLock()
	session := rt.session
	rt.mu.RUnlock()
	if session == nil {
		return
	}
	contact, err := session.Contact(ctx, conversation.ProviderChatID)
	if err != nil {
		return
	}
	conversation.Contact = &contact
	conversation.DisplayName = contact.Name
}

func (s *Service) Contacts(ctx context.Context, accountID string) ([]core.Contact, error) {
	rt, err := s.runtime(accountID)
	if err != nil {
		return nil, err
	}
	rt.mu.RLock()
	session := rt.session
	rt.mu.RUnlock()
	if session == nil {
		return nil, core.ErrNotConnected
	}
	return session.Contacts(ctx)
}

func (s *Service) Contact(ctx context.Context, accountID, providerID string) (core.Contact, error) {
	rt, err := s.runtime(accountID)
	if err != nil {
		return core.Contact{}, err
	}
	rt.mu.RLock()
	session := rt.session
	rt.mu.RUnlock()
	if session == nil {
		return core.Contact{}, core.ErrNotConnected
	}
	return session.Contact(ctx, providerID)
}

func (s *Service) SendText(ctx context.Context, conversationID, text, key string) (core.Message, error) {
	requestedText := text
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 4096 {
		return core.Message{}, fmt.Errorf("%w: text must be 1-4096 bytes", core.ErrInvalid)
	}
	if err := validateSendKey(key); err != nil {
		return core.Message{}, err
	}
	conversation, err := s.repo.GetConversation(ctx, conversationID)
	if err != nil {
		return core.Message{}, err
	}
	hash := sha256.Sum256([]byte(conversationID + "\x00text\x00" + requestedText))
	requestHash := hex.EncodeToString(hash[:])
	const actorID = "control"
	if saved, found, err := s.repo.LookupSend(ctx, actorID, key, requestHash); err != nil || found {
		return saved, err
	}
	rt, err := s.runtime(conversation.AccountID)
	if err != nil {
		return core.Message{}, err
	}
	rt.mu.RLock()
	state, session := rt.state, rt.session
	rt.mu.RUnlock()
	if state != "connected" || session == nil {
		return core.Message{}, core.ErrNotConnected
	}
	prepared, err := session.PrepareMessage(conversation.ProviderChatID)
	if err != nil {
		return core.Message{}, err
	}
	now := time.Now().UTC()
	message := core.Message{AccountID: conversation.AccountID, ConversationID: conversationID, ChatID: prepared.ChatID,
		ProviderMessageID: prepared.ProviderMessageID, Direction: "outbound", State: "queued", SenderID: prepared.SenderID,
		Kind: core.MessageKindText, Text: text, OccurredAt: now, IngestedAt: now}
	reserved, created, err := s.repo.ReserveSend(ctx, message, actorID, key, requestHash)
	if err != nil || !created {
		return reserved, err
	}
	sent, sendErr := session.SendText(ctx, prepared, text)
	result := "sent"
	if sendErr != nil {
		result = "outcome_unknown"
		if errors.Is(sendErr, core.ErrInvalid) {
			result = "failed"
		}
		s.logger.Warn("message send failed", "message_id", reserved.ID, "account_id", conversation.AccountID, "error", sendErr)
	} else if sent.ProviderMessageID != prepared.ProviderMessageID {
		result = "outcome_unknown"
		s.logger.Error("provider changed prepared message ID", "message_id", reserved.ID, "account_id", conversation.AccountID)
	}
	recordCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	completed, err := s.repo.CompleteSend(recordCtx, reserved.ID, result, sent)
	if err != nil {
		return reserved, fmt.Errorf("record send result for message %s: %w", reserved.ID, err)
	}
	return completed, nil
}

func (s *Service) Message(ctx context.Context, id string) (core.Message, error) {
	return s.repo.GetMessage(ctx, id)
}

func (s *Service) ListMessages(ctx context.Context, accountID string, before *core.PageCursor, limit int) ([]core.Message, error) {
	if _, err := s.runtime(accountID); err != nil {
		return nil, err
	}
	if err := validatePage(limit); err != nil {
		return nil, err
	}
	return s.repo.ListMessages(ctx, accountID, before, limit)
}

func (s *Service) ListConversationMessages(ctx context.Context, conversationID string, before *core.PageCursor, limit int) ([]core.Message, error) {
	if _, err := s.repo.GetConversation(ctx, conversationID); err != nil {
		return nil, err
	}
	if err := validatePage(limit); err != nil {
		return nil, err
	}
	return s.repo.ListConversationMessages(ctx, conversationID, before, limit)
}

func validatePage(limit int) error {
	if limit < 1 || limit > 201 {
		return core.ErrInvalid
	}
	return nil
}

func (s *Service) runtime(id string) (*runtimeAccount, error) {
	s.mu.RLock()
	rt := s.accounts[id]
	s.mu.RUnlock()
	if rt == nil {
		return nil, core.ErrNotFound
	}
	return rt, nil
}

func (rt *runtimeAccount) status() core.AccountStatus {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return core.AccountStatus{Account: rt.account, State: rt.state, LastError: rt.lastError}
}

func (rt *runtimeAccount) setError(err error) {
	rt.mu.Lock()
	rt.state = "error"
	rt.loginState = "failed"
	rt.lastError = err.Error()
	rt.challenge = nil
	rt.mu.Unlock()
}

func (s *Service) persistIdentity(id, identity string) error {
	if identity == "" {
		return core.ErrInvalid
	}
	if err := s.repo.SetIdentity(context.Background(), id, identity); err != nil {
		return err
	}
	rt, err := s.runtime(id)
	if err != nil {
		return err
	}
	rt.mu.Lock()
	rt.account.ProviderIdentity = identity
	rt.account.UpdatedAt = time.Now().UTC()
	rt.mu.Unlock()
	return nil
}

func (s *Service) onEvent(id string, generation uint64, event core.Event) {
	s.eventMu.Lock()
	if s.closing {
		s.eventMu.Unlock()
		return
	}
	s.eventWG.Add(1)
	s.eventMu.Unlock()
	defer s.eventWG.Done()
	rt, err := s.runtime(id)
	if err != nil {
		return
	}
	rt.eventMu.Lock()
	defer rt.eventMu.Unlock()
	rt.mu.RLock()
	current := rt.generation == generation
	rt.mu.RUnlock()
	if !current {
		return
	}
	switch event.Type {
	case core.EventPaired:
		if err := s.persistIdentity(id, event.Identity); err != nil {
			rt.setError(err)
			s.logger.Error("save paired identity failed", "account_id", id, "error", err)
			return
		}
		rt.mu.Lock()
		rt.state = "connecting"
		rt.loginState = "connecting"
		rt.challenge = nil
		rt.mu.Unlock()
	case core.EventConnected:
		rt.mu.RLock()
		identity, session := rt.account.ProviderIdentity, rt.session
		rt.mu.RUnlock()
		if session == nil {
			return
		}
		if identity == "" {
			if err := s.persistIdentity(id, session.Identity()); err != nil {
				rt.setError(err)
				return
			}
		}
		rt.mu.Lock()
		rt.state = "connected"
		rt.loginState = "connected"
		rt.lastError = ""
		rt.challenge = nil
		rt.mu.Unlock()
		s.scanPendingMedia()
		s.requestAvatarScan(id)
	case core.EventDisconnected:
		rt.mu.Lock()
		if rt.state == "connected" || rt.state == "connecting" || rt.state == "reconnecting" {
			rt.state = "reconnecting"
		}
		rt.mu.Unlock()
	case core.EventLoggedOut:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		rt.mu.Lock()
		rt.generation++
		rt.account.ProviderIdentity = ""
		rt.account.UpdatedAt = time.Now().UTC()
		rt.session = nil
		rt.state = "needs_login"
		rt.loginState = "idle"
		rt.challenge = nil
		rt.lastError = ""
		if event.Err != nil {
			rt.lastError = event.Err.Error()
		}
		rt.mu.Unlock()
		if err := s.clearAccountAvatars(ctx, id); err != nil {
			s.logger.Error("clear avatars after logout failed", "account_id", id, "error", err)
		}
		if err := s.repo.ClearIdentity(ctx, id); err != nil {
			rt.setError(fmt.Errorf("clear logged-out identity: %w", err))
			s.logger.Error("clear logged-out identity failed", "account_id", id, "error", err)
			return
		}
		if err := s.repo.FailMediaSendsForAccount(ctx, id); err != nil {
			s.logger.Error("fail pending media sends after logout", "account_id", id, "error", err)
		}
		s.scanPendingMedia()
	case core.EventError:
		if event.Err != nil {
			rt.setError(event.Err)
		}
	case core.EventMessage:
		if event.Message == nil {
			return
		}
		message := *event.Message
		message.AccountID = id
		for i := range message.Attachments {
			message.Attachments[i].AutoFetch = true
		}
		saved, err := s.repo.SaveMessage(context.Background(), message)
		if err != nil {
			s.logger.Error("save incoming message failed", "account_id", id, "error", err)
			return
		}
		for _, attachment := range saved.Attachments {
			if attachment.Availability == "remote" {
				s.queueMedia(attachment.ID)
			}
		}
	case core.EventHistory:
		if event.History == nil {
			return
		}
		batch := *event.History
		batch.AccountID = id
		if err := s.repo.ImportHistory(context.Background(), batch); err != nil {
			s.logger.Error("import history batch failed", "account_id", id, "message_count", len(batch.Messages), "error", err)
		}
	case core.EventChatProfile:
		if event.Profile == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.repo.UpdateChatProfile(ctx, id, *event.Profile); err != nil {
			s.logger.Warn("update chat profile failed", "account_id", id, "error", err)
		}
		if s.avatars.queue != nil && (event.Profile.Kind == "group" || event.Profile.Kind == "direct") {
			s.queueAvatarIfDue(ctx, id, event.Profile.ProviderChatID)
		}
	case core.EventAvatarChanged:
		if event.AvatarID == "" {
			return
		}
		epoch := s.invalidateAvatar(id, event.AvatarID)
		if event.AvatarRemoved {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.saveMissingAvatar(ctx, id, event.AvatarID, nil, epoch); err != nil {
				s.logger.Warn("clear removed avatar failed", "account_id", id, "error", err)
			}
			return
		}
		if !s.queueAvatar(id, event.AvatarID, true) && s.avatars.queue != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.repo.TouchAvatar(ctx, id, event.AvatarID, time.Time{}); err != nil && !errors.Is(err, core.ErrNotFound) {
				s.logger.Warn("mark changed avatar stale failed", "account_id", id, "error", err)
			}
			s.requestAvatarScan(id)
		}
	}
}
