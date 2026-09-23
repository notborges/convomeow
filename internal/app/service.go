package app

import (
	"context"
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
	account    core.Account
	state      string
	lastError  string
	loginState string
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
}

func New(repo core.Repository, connector core.Connector, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{repo: repo, connector: connector, logger: logger, accounts: make(map[string]*runtimeAccount), ctx: ctx, cancel: cancel}
}

func (s *Service) Start(ctx context.Context) error {
	accounts, err := s.repo.ListAccounts(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	for _, account := range accounts {
		rt := &runtimeAccount{account: account, state: "needs_login", loginState: "idle"}
		if account.ProviderIdentity != "" {
			rt.state = "connecting"
		}
		s.accounts[account.ID] = rt
	}
	s.mu.Unlock()
	for _, account := range accounts {
		if account.ProviderIdentity == "" {
			continue
		}
		rt := s.accounts[account.ID]
		session, err := s.connector.Open(ctx, account.ProviderIdentity, func(event core.Event) { s.onEvent(account.ID, event) })
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
	s.eventMu.Lock()
	s.closing = true
	s.eventMu.Unlock()
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
	s.eventWG.Wait()
	connectorErr := s.connector.Close()
	repoErr := s.repo.Close()
	return errors.Join(connectorErr, repoErr)
}

func (s *Service) CreateAccount(ctx context.Context, label string) (core.AccountStatus, error) {
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
	a := core.Account{ID: uuid.NewString(), Provider: core.ProviderWhatsApp, Label: label, CreatedAt: now, UpdatedAt: now}
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

func (s *Service) LoginStatus(id string) (core.LoginStatus, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return core.LoginStatus{}, err
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	state := rt.loginState
	if rt.state == "connected" {
		state = "connected"
	}
	return core.LoginStatus{State: state, Challenge: rt.challenge, Error: rt.lastError}, nil
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
	session, err := s.connector.New(func(event core.Event) { s.onEvent(id, event) })
	if err != nil {
		s.loginMu.Lock()
		s.activeLogin = ""
		s.loginMu.Unlock()
		return core.LoginStatus{}, err
	}
	rt.mu.Lock()
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
			rt.challenge = &challenge
			rt.loginState = "challenge_available"
			rt.mu.Unlock()
		})
		s.finishLogin(id, rt, session, err)
	}()
	return core.LoginStatus{State: "waiting_for_challenge"}, nil
}

func (s *Service) finishLogin(id string, rt *runtimeAccount, session core.Session, loginErr error) {
	s.loginMu.Lock()
	if s.activeLogin == id {
		s.activeLogin = ""
	}
	s.loginMu.Unlock()
	rt.mu.RLock()
	paired := rt.account.ProviderIdentity != ""
	rt.mu.RUnlock()
	if !paired && session != nil && session.Identity() != "" {
		if err := s.persistIdentity(id, session.Identity()); err != nil {
			loginErr = errors.Join(loginErr, err)
		} else {
			paired = true
		}
	}
	if loginErr != nil {
		if !paired {
			if session != nil {
				session.Close()
			}
		}
		rt.mu.Lock()
		if !paired {
			rt.session = nil
		}
		rt.state = "error"
		rt.loginState = "failed"
		rt.lastError = loginErr.Error()
		rt.challenge = nil
		rt.mu.Unlock()
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
}

func (s *Service) SendText(ctx context.Context, id, recipient, text string) (core.Message, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return core.Message{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 4096 {
		return core.Message{}, fmt.Errorf("%w: text must be 1-4096 bytes", core.ErrInvalid)
	}
	rt.mu.RLock()
	state, session := rt.state, rt.session
	rt.mu.RUnlock()
	if state != "connected" || session == nil {
		return core.Message{}, core.ErrNotConnected
	}
	sent, err := session.SendText(ctx, recipient, text)
	if err != nil {
		if errors.Is(err, core.ErrInvalid) {
			return core.Message{}, err
		}
		return core.Message{}, fmt.Errorf("%w: %v", core.ErrOutcomeUnknown, err)
	}
	m := core.Message{AccountID: id, ChatID: sent.ChatID, ProviderMessageID: sent.ProviderMessageID, Direction: "outbound", Kind: core.MessageKindText,
		SenderID: sent.SenderID, Text: text, OccurredAt: sent.Timestamp}
	recordCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	saved, err := s.repo.SaveMessage(recordCtx, m)
	if err != nil {
		return m, fmt.Errorf("%w: %v", core.ErrSentUnrecorded, err)
	}
	return saved, nil
}

func (s *Service) ListMessages(ctx context.Context, id string, after int64, limit int) ([]core.Message, error) {
	if _, err := s.runtime(id); err != nil {
		return nil, err
	}
	if after < 0 {
		return nil, core.ErrInvalid
	}
	limit, err := pageLimit(limit)
	if err != nil {
		return nil, err
	}
	return s.repo.ListMessages(ctx, id, after, limit)
}

func (s *Service) ListChats(ctx context.Context, id string, before int64, limit int) ([]core.Chat, error) {
	if _, err := s.runtime(id); err != nil {
		return nil, err
	}
	if before < 0 {
		return nil, core.ErrInvalid
	}
	limit, err := pageLimit(limit)
	if err != nil {
		return nil, err
	}
	return s.repo.ListChats(ctx, id, before, limit)
}

func (s *Service) ListChatMessages(ctx context.Context, id, chatID string, before int64, limit int) ([]core.Message, error) {
	if _, err := s.runtime(id); err != nil {
		return nil, err
	}
	if chatID == "" || before < 0 {
		return nil, core.ErrInvalid
	}
	limit, err := pageLimit(limit)
	if err != nil {
		return nil, err
	}
	return s.repo.ListChatMessages(ctx, id, chatID, before, limit)
}

func pageLimit(limit int) (int, error) {
	if limit < 0 {
		return 0, core.ErrInvalid
	}
	if limit == 0 {
		return 100, nil
	}
	if limit > 500 {
		return 500, nil
	}
	return limit, nil
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

func (s *Service) onEvent(id string, event core.Event) {
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
		if identity == "" && session != nil {
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
	case core.EventDisconnected:
		rt.mu.Lock()
		if rt.state == "connected" || rt.state == "connecting" || rt.state == "reconnecting" {
			rt.state = "reconnecting"
		}
		rt.mu.Unlock()
	case core.EventLoggedOut:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.repo.ClearIdentity(ctx, id); err != nil {
			rt.setError(fmt.Errorf("clear logged-out identity: %w", err))
			s.logger.Error("clear logged-out identity failed", "account_id", id, "error", err)
			return
		}
		rt.mu.Lock()
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
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := s.repo.SaveMessage(ctx, message); err != nil {
			s.logger.Error("save incoming message failed", "account_id", id, "error", err)
		}
	}
}
