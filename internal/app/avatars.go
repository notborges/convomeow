package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/core"
	"github.com/notborges/convomeow/internal/media"
)

const maxAvatarBytes = 512 << 10

var errAvatarCacheMissing = errors.New("cached avatar object is missing")

type avatarJob struct {
	accountID  string
	providerID string
}

type avatarState struct {
	queue   chan avatarJob
	scans   chan string
	mu      sync.Mutex
	queued  map[string]bool
	forced  map[string]bool
	epoch   map[string]uint64
	flights map[string]*avatarFlight
	next    map[string]int
}

type avatarFlight struct {
	done   chan struct{}
	avatar core.Avatar
	err    error
}

func avatarKey(accountID, providerID string) string { return accountID + "\x00" + providerID }

func (s *Service) startAvatarWorkers() {
	s.avatars.forced = make(map[string]bool)
	s.avatars.epoch = make(map[string]uint64)
	s.avatars.flights = make(map[string]*avatarFlight)
	s.avatars.next = make(map[string]int)
	pace := time.NewTicker(500 * time.Millisecond)
	sweep := time.NewTicker(10 * time.Minute)
	s.workWG.Add(1)
	go func() {
		defer s.workWG.Done()
		defer pace.Stop()
		defer sweep.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case accountID := <-s.avatars.scans:
				s.scanAccountAvatars(accountID)
			case <-sweep.C:
				for _, account := range s.ListAccounts() {
					if account.State == "connected" {
						s.scanAccountAvatars(account.ID)
					}
				}
			}
		}
	}()
	for range 2 {
		s.workWG.Add(1)
		go func() {
			defer s.workWG.Done()
			for {
				select {
				case <-s.ctx.Done():
					return
				case job := <-s.avatars.queue:
					select {
					case <-s.ctx.Done():
						return
					case <-pace.C:
					}
					s.avatars.mu.Lock()
					key := avatarKey(job.accountID, job.providerID)
					force := s.avatars.forced[key]
					delete(s.avatars.queued, key)
					delete(s.avatars.forced, key)
					s.avatars.mu.Unlock()
					ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
					if force || s.avatarNeedsRefresh(ctx, job.accountID, job.providerID) {
						if _, err := s.refreshAvatar(ctx, job.accountID, job.providerID); err != nil &&
							!errors.Is(err, core.ErrNotFound) && !errors.Is(err, core.ErrNotConnected) && s.ctx.Err() == nil {
							s.logger.Warn("refresh avatar failed", "account_id", job.accountID, "error", err)
						}
					}
					cancel()
				}
			}
		}()
	}
}

func (s *Service) requestAvatarScan(accountID string) {
	if s.avatars.scans == nil {
		return
	}
	select {
	case s.avatars.scans <- accountID:
	default:
	}
}

func (s *Service) queueAvatar(accountID, providerID string, force bool) bool {
	if s.avatars.queue == nil || s.ctx.Err() != nil || providerID == "" {
		return false
	}
	key := avatarKey(accountID, providerID)
	s.avatars.mu.Lock()
	defer s.avatars.mu.Unlock()
	if s.avatars.queued[key] {
		s.avatars.forced[key] = s.avatars.forced[key] || force
		return true
	}
	select {
	case s.avatars.queue <- avatarJob{accountID: accountID, providerID: providerID}:
		s.avatars.queued[key] = true
		s.avatars.forced[key] = force
		return true
	default:
		return false
	}
}

func (s *Service) invalidateAvatar(accountID, providerID string) uint64 {
	if s.avatars.queue == nil {
		return 0
	}
	s.avatars.mu.Lock()
	key := avatarKey(accountID, providerID)
	s.avatars.epoch[key]++
	epoch := s.avatars.epoch[key]
	s.avatars.mu.Unlock()
	return epoch
}

func (s *Service) scanAccountAvatars(accountID string) {
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
	defer cancel()
	contacts, err := s.Contacts(ctx, accountID)
	if err != nil {
		if s.ctx.Err() == nil {
			s.logger.Warn("list contacts for avatar cache failed", "account_id", accountID, "error", err)
		}
		return
	}
	groups, err := s.repo.ListGroupChatIDs(ctx, accountID)
	if err != nil {
		s.logger.Warn("list groups for avatar cache failed", "account_id", accountID, "error", err)
		return
	}
	targets := make([]string, 0, len(contacts)+len(groups))
	for _, contact := range contacts {
		targets = append(targets, contact.ProviderID)
	}
	targets = append(targets, groups...)
	if len(targets) == 0 {
		delete(s.avatars.next, accountID)
		return
	}
	start := s.avatars.next[accountID] % len(targets)
	for offset := range targets {
		index := (start + offset) % len(targets)
		if !s.queueAvatarIfDue(ctx, accountID, targets[index]) {
			s.avatars.next[accountID] = index
			return
		}
	}
	delete(s.avatars.next, accountID)
}

func (s *Service) queueAvatarIfDue(ctx context.Context, accountID, providerID string) bool {
	record, err := s.repo.GetAvatar(ctx, accountID, providerID)
	if err != nil && !errors.Is(err, core.ErrNotFound) {
		s.logger.Warn("read avatar cache failed", "account_id", accountID, "error", err)
		return false
	}
	if errors.Is(err, core.ErrNotFound) || avatarDue(record, time.Now()) {
		return s.queueAvatar(accountID, providerID, false)
	}
	return true
}

func avatarDue(record core.AvatarRecord, now time.Time) bool {
	interval := 24 * time.Hour
	if record.ObjectKey == "" {
		interval = 6 * time.Hour
	}
	return !now.Before(record.CheckedAt.Add(interval))
}

func (s *Service) avatarNeedsRefresh(ctx context.Context, accountID, providerID string) bool {
	record, err := s.repo.GetAvatar(ctx, accountID, providerID)
	return errors.Is(err, core.ErrNotFound) || err == nil && avatarDue(record, time.Now())
}

func (s *Service) Avatar(ctx context.Context, accountID, providerID string) (core.Avatar, error) {
	rt, err := s.runtime(accountID)
	if err != nil {
		return core.Avatar{}, err
	}
	rt.mu.RLock()
	paired, connected, session := rt.account.ProviderIdentity != "", rt.state == "connected", rt.session
	rt.mu.RUnlock()
	if !paired {
		return core.Avatar{}, core.ErrNotConnected
	}
	if s.media.options.Stores == nil {
		if !connected || session == nil {
			return core.Avatar{}, core.ErrNotConnected
		}
		avatar, _, err := session.FetchAvatar(ctx, providerID, "")
		return avatar, err
	}
	record, err := s.repo.GetAvatar(ctx, accountID, providerID)
	if err != nil && !errors.Is(err, core.ErrNotFound) {
		return core.Avatar{}, err
	}
	if err == nil && !avatarDue(record, time.Now()) {
		avatar, readErr := s.readAvatar(ctx, record)
		if readErr == nil || errors.Is(readErr, core.ErrNotFound) {
			return avatar, readErr
		}
		if !errors.Is(readErr, errAvatarCacheMissing) {
			return core.Avatar{}, readErr
		}
	}
	if connected && session != nil {
		avatar, fetchErr := s.refreshAvatar(ctx, accountID, providerID)
		if fetchErr == nil || errors.Is(fetchErr, core.ErrNotFound) {
			return avatar, fetchErr
		}
		record, err = s.repo.GetAvatar(ctx, accountID, providerID)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				return core.Avatar{}, fetchErr
			}
			return core.Avatar{}, err
		}
	}
	if err == nil {
		avatar, readErr := s.readAvatar(ctx, record)
		if errors.Is(readErr, errAvatarCacheMissing) {
			return core.Avatar{}, core.ErrMediaStorage
		}
		return avatar, readErr
	}
	return core.Avatar{}, core.ErrNotConnected
}

func (s *Service) ConversationAvatar(ctx context.Context, id string) (core.Avatar, error) {
	conversation, err := s.repo.GetConversation(ctx, id)
	if err != nil {
		return core.Avatar{}, err
	}
	if conversation.Kind == "direct" {
		s.enrichConversation(ctx, &conversation)
		if conversation.Contact != nil {
			return s.Avatar(ctx, conversation.AccountID, conversation.Contact.ProviderID)
		}
	}
	return s.Avatar(ctx, conversation.AccountID, conversation.ProviderChatID)
}

func (s *Service) refreshAvatar(ctx context.Context, accountID, providerID string) (core.Avatar, error) {
	key := avatarKey(accountID, providerID)
	s.avatars.mu.Lock()
	if flight := s.avatars.flights[key]; flight != nil {
		s.avatars.mu.Unlock()
		select {
		case <-ctx.Done():
			return core.Avatar{}, ctx.Err()
		case <-flight.done:
			return flight.avatar, flight.err
		}
	}
	flight := &avatarFlight{done: make(chan struct{})}
	s.avatars.flights[key] = flight
	s.avatars.mu.Unlock()
	flight.avatar, flight.err = s.fetchAndStoreAvatar(ctx, accountID, providerID)
	s.avatars.mu.Lock()
	delete(s.avatars.flights, key)
	close(flight.done)
	s.avatars.mu.Unlock()
	return flight.avatar, flight.err
}

func (s *Service) fetchAndStoreAvatar(ctx context.Context, accountID, providerID string) (core.Avatar, error) {
	rt, err := s.runtime(accountID)
	if err != nil {
		return core.Avatar{}, err
	}
	rt.mu.RLock()
	connected, session := rt.state == "connected", rt.session
	rt.mu.RUnlock()
	if !connected || session == nil {
		return core.Avatar{}, core.ErrNotConnected
	}
	key := avatarKey(accountID, providerID)
	s.avatars.mu.Lock()
	epoch := s.avatars.epoch[key]
	s.avatars.mu.Unlock()
	record, err := s.repo.GetAvatar(ctx, accountID, providerID)
	if err != nil && !errors.Is(err, core.ErrNotFound) {
		return core.Avatar{}, err
	}
	existingID := ""
	if err == nil && record.ObjectKey != "" {
		existingID = record.PictureID
	}
	avatar, unchanged, err := session.FetchAvatar(ctx, providerID, existingID)
	if errors.Is(err, core.ErrNotFound) {
		if s.avatarEpoch(key) != epoch {
			return core.Avatar{}, core.ErrAvatarFetch
		}
		if saveErr := s.saveMissingAvatar(ctx, accountID, providerID, session, epoch); saveErr != nil {
			return core.Avatar{}, saveErr
		}
		return core.Avatar{}, core.ErrNotFound
	}
	if err != nil {
		return core.Avatar{}, err
	}
	if unchanged {
		if err == nil && record.ObjectKey != "" {
			avatar, readErr := s.readAvatar(ctx, record)
			if readErr == nil {
				if s.avatarEpoch(key) != epoch {
					return core.Avatar{}, core.ErrAvatarFetch
				}
				s.avatars.mu.Lock()
				if s.avatars.epoch[key] != epoch || !avatarSessionCurrent(rt, session) {
					s.avatars.mu.Unlock()
					return core.Avatar{}, core.ErrAvatarFetch
				}
				touchErr := s.repo.TouchAvatar(ctx, accountID, providerID, time.Now().UTC())
				s.avatars.mu.Unlock()
				if touchErr != nil {
					return core.Avatar{}, touchErr
				}
				return avatar, nil
			}
			if !errors.Is(readErr, errAvatarCacheMissing) {
				return core.Avatar{}, readErr
			}
		}
		avatar, unchanged, err = session.FetchAvatar(ctx, providerID, "")
		if err != nil || unchanged {
			return core.Avatar{}, fmt.Errorf("%w: retry avatar download: %v", core.ErrAvatarFetch, err)
		}
	}
	if s.avatarEpoch(key) != epoch {
		return core.Avatar{}, core.ErrAvatarFetch
	}
	if err := s.saveAvatar(ctx, accountID, providerID, avatar, session, epoch); err != nil {
		return core.Avatar{}, err
	}
	return avatar, nil
}

func (s *Service) avatarEpoch(key string) uint64 {
	s.avatars.mu.Lock()
	defer s.avatars.mu.Unlock()
	return s.avatars.epoch[key]
}

func avatarSessionCurrent(rt *runtimeAccount, session core.Session) bool {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.state == "connected" && rt.session == session
}

func (s *Service) saveMissingAvatar(ctx context.Context, accountID, providerID string, session core.Session, epoch uint64) error {
	if s.media.options.Stores == nil {
		return nil
	}
	s.avatars.mu.Lock()
	if s.avatars.epoch[avatarKey(accountID, providerID)] != epoch {
		s.avatars.mu.Unlock()
		return core.ErrAvatarFetch
	}
	rt, err := s.runtime(accountID)
	if err != nil {
		s.avatars.mu.Unlock()
		return err
	}
	if session != nil && !avatarSessionCurrent(rt, session) {
		s.avatars.mu.Unlock()
		return core.ErrNotConnected
	}
	if session == nil {
		rt.mu.RLock()
		active := rt.session != nil && rt.account.ProviderIdentity != ""
		rt.mu.RUnlock()
		if !active {
			s.avatars.mu.Unlock()
			return core.ErrNotConnected
		}
	}
	err = s.repo.SaveAvatar(ctx, core.AvatarRecord{AccountID: accountID, ProviderID: providerID, CheckedAt: time.Now().UTC()})
	s.avatars.mu.Unlock()
	if err == nil {
		s.scanPendingMedia()
	}
	return err
}

func (s *Service) clearAccountAvatars(ctx context.Context, accountID string) error {
	s.avatars.mu.Lock()
	err := s.repo.ClearAccountAvatars(ctx, accountID)
	s.avatars.mu.Unlock()
	if err == nil {
		s.scanPendingMedia()
	}
	return err
}

func (s *Service) saveAvatar(ctx context.Context, accountID, providerID string, avatar core.Avatar, session core.Session, epoch uint64) error {
	if len(avatar.Data) == 0 || len(avatar.Data) > maxAvatarBytes ||
		(avatar.ContentType != "image/jpeg" && avatar.ContentType != "image/png" && avatar.ContentType != "image/webp") {
		return core.ErrMediaUnavailable
	}
	size := int64(len(avatar.Data))
	if err := s.reserveMediaStorage(ctx, size); err != nil {
		return err
	}
	defer s.releaseMediaStorage(size)
	s.media.mu.Lock()
	if size > s.media.options.MaxTempBytes-s.media.reservedTemp {
		s.media.mu.Unlock()
		return core.ErrMediaBusy
	}
	s.media.reservedTemp += size
	s.media.mu.Unlock()
	defer func() {
		s.media.mu.Lock()
		s.media.reservedTemp -= size
		s.media.mu.Unlock()
	}()
	file, err := os.CreateTemp(s.media.options.TempDir, ".avatar-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if _, err := file.Write(avatar.Data); err != nil {
		return err
	}
	profileID, store := s.media.options.Stores.Active()
	objectKey, err := media.AvatarKey(accountID, uuid.NewString())
	if err != nil {
		return err
	}
	if err := store.Put(ctx, objectKey, file, size); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if cleanupErr := store.Delete(cleanupCtx, objectKey); cleanupErr != nil {
			s.logger.Warn("remove failed avatar upload", "account_id", accountID, "error", cleanupErr)
		}
		cancel()
		return fmt.Errorf("%w: %v", core.ErrMediaStorage, err)
	}
	record := core.AvatarRecord{AccountID: accountID, ProviderID: providerID, PictureID: avatar.PictureID,
		ProfileID: profileID, ObjectKey: objectKey, ContentType: avatar.ContentType, Size: size, CheckedAt: time.Now().UTC()}
	s.avatars.mu.Lock()
	rt, runtimeErr := s.runtime(accountID)
	valid := runtimeErr == nil && s.avatars.epoch[avatarKey(accountID, providerID)] == epoch && avatarSessionCurrent(rt, session)
	var saveErr error
	if valid {
		saveErr = s.repo.SaveAvatar(ctx, record)
	} else {
		saveErr = core.ErrNotConnected
	}
	s.avatars.mu.Unlock()
	if saveErr != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if cleanupErr := store.Delete(cleanupCtx, objectKey); cleanupErr != nil {
			s.logger.Warn("remove unrecorded avatar failed", "account_id", accountID, "error", cleanupErr)
		}
		return saveErr
	}
	s.scanPendingMedia()
	return nil
}

func (s *Service) readAvatar(ctx context.Context, record core.AvatarRecord) (core.Avatar, error) {
	if record.ObjectKey == "" {
		return core.Avatar{}, core.ErrNotFound
	}
	store, ok := s.media.options.Stores.Get(record.ProfileID)
	if !ok {
		return core.Avatar{}, core.ErrMediaStorage
	}
	reader, err := store.Open(ctx, record.ObjectKey, 0, -1)
	if errors.Is(err, media.ErrNotFound) {
		return core.Avatar{}, errAvatarCacheMissing
	}
	if err != nil {
		return core.Avatar{}, fmt.Errorf("%w: %v", core.ErrMediaStorage, err)
	}
	defer reader.Close()
	if record.Size < 1 || record.Size > maxAvatarBytes {
		return core.Avatar{}, errAvatarCacheMissing
	}
	data, err := io.ReadAll(io.LimitReader(reader, record.Size+1))
	if err != nil {
		return core.Avatar{}, fmt.Errorf("%w: %v", core.ErrMediaStorage, err)
	}
	if int64(len(data)) != record.Size {
		return core.Avatar{}, errAvatarCacheMissing
	}
	return core.Avatar{ContentType: record.ContentType, Data: data, PictureID: record.PictureID}, nil
}
