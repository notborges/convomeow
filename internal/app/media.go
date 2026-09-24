package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/notborges/convomeow/internal/core"
	"github.com/notborges/convomeow/internal/media"
)

type MediaOptions struct {
	Stores        *media.Registry
	TempDir       string
	MaxFileBytes  int64
	MaxTotalBytes int64
	MaxTempBytes  int64
	Workers       int
}

type mediaState struct {
	options        MediaOptions
	queue          chan string
	sendQueue      chan string
	wake           chan struct{}
	mu             sync.Mutex
	inflight       map[string]bool
	sendInflight   map[string]bool
	reservedTemp   int64
	reservedStored int64
}

func (s *Service) startMediaWorkers() error {
	options := s.media.options
	if options.Workers < 1 || options.MaxFileBytes < 1 || options.MaxFileBytes >= 5<<30 || options.MaxTotalBytes < options.MaxFileBytes ||
		options.MaxTempBytes < options.MaxFileBytes+(1<<20) || options.TempDir == "" {
		return fmt.Errorf("%w: invalid media worker configuration", core.ErrInvalid)
	}
	if err := os.MkdirAll(s.media.options.TempDir, 0700); err != nil {
		return fmt.Errorf("create media staging directory: %w", err)
	}
	if err := os.Chmod(s.media.options.TempDir, 0700); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.media.options.TempDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasPrefix(entry.Name(), ".download-") || strings.HasPrefix(entry.Name(), ".upload-") || strings.HasPrefix(entry.Name(), ".encrypt-")) {
			if err := os.Remove(filepath.Join(s.media.options.TempDir, entry.Name())); err != nil {
				return fmt.Errorf("remove abandoned media staging file: %w", err)
			}
		}
	}
	for i := 0; i < s.media.options.Workers; i++ {
		s.workWG.Add(1)
		go func() {
			defer s.workWG.Done()
			s.mediaWorker()
		}()
	}
	for i := 0; i < s.media.options.Workers; i++ {
		s.workWG.Add(1)
		go func() {
			defer s.workWG.Done()
			s.mediaSendWorker()
		}()
	}
	s.workWG.Add(1)
	go func() {
		defer s.workWG.Done()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		s.scanMedia()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
			case <-s.media.wake:
			}
			s.scanMedia()
		}
	}()
	return nil
}

func (s *Service) scanPendingMedia() {
	if s.media.wake == nil {
		return
	}
	select {
	case s.media.wake <- struct{}{}:
	default:
	}
}

func (s *Service) scanMedia() {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	ids, err := s.repo.ListPendingMedia(ctx, time.Now().UTC(), 1000)
	if err != nil {
		if s.ctx.Err() == nil {
			s.logger.Warn("scan pending media failed", "error", err)
		}
		return
	}
	for _, id := range ids {
		if !s.queueMedia(id) {
			break
		}
	}
	s.scanMediaSends(ctx)
	s.cleanUploads(ctx)
	s.cleanMediaOrphans(ctx)
}

func (s *Service) queueMedia(id string) bool {
	if s.media.queue == nil || s.ctx.Err() != nil {
		return false
	}
	s.media.mu.Lock()
	defer s.media.mu.Unlock()
	if s.media.inflight[id] {
		return true
	}
	select {
	case s.media.queue <- id:
		s.media.inflight[id] = true
		return true
	default:
		return false
	}
}

func (s *Service) mediaWorker() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case id := <-s.media.queue:
			s.processMedia(id)
			s.media.mu.Lock()
			delete(s.media.inflight, id)
			s.media.mu.Unlock()
			s.scanPendingMedia()
		}
	}
}

func (s *Service) processMedia(id string) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()
	record, err := s.repo.GetMedia(ctx, id)
	if err != nil || record.Availability != "remote" {
		return
	}
	if len(record.ProviderRef) == 0 {
		s.mediaFailure(record, "reference", core.ErrMediaUnavailable)
		return
	}
	if record.DeclaredSize > uint64(s.media.options.MaxFileBytes) {
		s.blockMedia(record, "too_large", int64(record.DeclaredSize))
		return
	}
	rt, err := s.runtime(record.AccountID)
	if err != nil {
		return
	}
	rt.mu.RLock()
	state, session := rt.state, rt.session
	rt.mu.RUnlock()
	if state != "connected" || session == nil {
		_ = s.repo.ScheduleMediaRetry(ctx, id, time.Now().UTC().Add(30*time.Second), record.AttemptCount)
		return
	}
	reserved := s.media.options.MaxFileBytes + (1 << 20)
	s.media.mu.Lock()
	if reserved > s.media.options.MaxTempBytes-s.media.reservedTemp {
		s.media.mu.Unlock()
		_ = s.repo.ScheduleMediaRetry(ctx, id, time.Now().UTC().Add(5*time.Second), record.AttemptCount)
		return
	}
	s.media.reservedTemp += reserved
	s.media.mu.Unlock()
	defer func() {
		s.media.mu.Lock()
		s.media.reservedTemp -= reserved
		s.media.mu.Unlock()
	}()
	file, err := os.CreateTemp(s.media.options.TempDir, ".download-*")
	if err != nil {
		s.mediaFailure(record, "staging", err)
		return
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		s.mediaFailure(record, "staging", err)
		return
	}
	updatedRef, err := session.DownloadMedia(ctx, core.MediaSource{ChatID: record.ChatID,
		ProviderMessageID: record.ProviderMessageID, Direction: record.Direction,
		SenderID: record.SenderID, ProviderRef: record.ProviderRef}, file, s.media.options.MaxFileBytes)
	if len(updatedRef) > 0 {
		if saveErr := s.repo.UpdateMediaRef(ctx, id, updatedRef); saveErr != nil {
			s.mediaFailure(record, "reference", saveErr)
			return
		}
	}
	if err != nil {
		if errors.Is(err, core.ErrMediaTooLarge) {
			s.blockMedia(record, "too_large", s.media.options.MaxFileBytes+1)
			return
		}
		s.mediaFailure(record, "download", err)
		return
	}
	info, err := file.Stat()
	if err != nil {
		s.mediaFailure(record, "staging", err)
		return
	}
	if info.Size() > s.media.options.MaxFileBytes {
		s.blockMedia(record, "too_large", info.Size())
		return
	}
	if err := s.reserveMediaStorage(ctx, info.Size()); err != nil {
		if errors.Is(err, core.ErrMediaQuota) {
			s.blockMedia(record, "quota", max(info.Size(), 1))
			return
		}
		s.mediaFailure(record, "quota", err)
		return
	}
	defer s.releaseMediaStorage(info.Size())
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		s.mediaFailure(record, "staging", err)
		return
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		s.mediaFailure(record, "hash", err)
		return
	}
	profileID, store := s.media.options.Stores.Active()
	key, err := media.Key(record.AccountID, record.AttachmentID)
	if err != nil {
		s.mediaFailure(record, "key", err)
		return
	}
	if err := store.Put(ctx, key, file, info.Size()); err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = store.Delete(cleanupCtx, key)
		cleanupCancel()
		s.mediaFailure(record, "store", err)
		return
	}
	if err := s.repo.MarkMediaReady(ctx, id, profileID, key, info.Size(), hash.Sum(nil)); err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		current, readErr := s.repo.GetMedia(cleanupCtx, id)
		if readErr == nil && (current.Availability != "ready" || current.ObjectKey != key || current.StorageProfileID != profileID) {
			if cleanupErr := store.Delete(cleanupCtx, key); cleanupErr != nil {
				s.logger.Warn("remove unrecorded media failed", "attachment_id", id)
			}
		}
		s.mediaFailure(record, "record", err)
	}
}

func (s *Service) reserveMediaStorage(ctx context.Context, size int64) error {
	s.media.mu.Lock()
	defer s.media.mu.Unlock()
	used, err := s.repo.StoredMediaBytes(ctx)
	if err != nil {
		return err
	}
	available := s.media.options.MaxTotalBytes - used
	if available < 0 || s.media.reservedStored > available || size > available-s.media.reservedStored {
		return core.ErrMediaQuota
	}
	s.media.reservedStored += size
	return nil
}

func (s *Service) releaseMediaStorage(size int64) {
	s.media.mu.Lock()
	s.media.reservedStored -= size
	s.media.mu.Unlock()
}

func (s *Service) mediaFailure(record core.MediaRecord, stage string, err error) {
	if s.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	switch {
	case errors.Is(err, core.ErrMediaUnavailable):
		if saveErr := s.repo.MarkMediaUnavailable(ctx, record.AttachmentID); saveErr != nil {
			s.logger.Warn("record unavailable media failed", "attachment_id", record.AttachmentID)
		}
	default:
		attempt := record.AttemptCount + 1
		next := time.Time{}
		if attempt < 5 {
			delay := time.Duration(1<<min(attempt, 5)) * 15 * time.Second
			next = time.Now().UTC().Add(delay)
		}
		if saveErr := s.repo.ScheduleMediaRetry(ctx, record.AttachmentID, next, attempt); saveErr != nil {
			s.logger.Warn("record media retry failed", "attachment_id", record.AttachmentID)
		}
		args := []any{"attachment_id", record.AttachmentID, "account_id", record.AccountID, "stage", stage}
		if stage != "download" && stage != "reference" {
			args = append(args, "error", err)
		}
		s.logger.Warn("media download failed", args...)
	}
}

func (s *Service) blockMedia(record core.MediaRecord, reason string, requiredBytes int64) {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	if err := s.repo.BlockMedia(ctx, record.AttachmentID, reason, requiredBytes); err != nil && s.ctx.Err() == nil {
		s.logger.Warn("record media limit failed", "attachment_id", record.AttachmentID)
	}
}

func (s *Service) cleanMediaOrphans(ctx context.Context) {
	objects, err := s.repo.ListMediaOrphans(ctx, 100)
	if err != nil {
		return
	}
	for _, object := range objects {
		store, ok := s.media.options.Stores.Get(object.ProfileID)
		if !ok || store.Delete(ctx, object.Key) != nil {
			continue
		}
		_ = s.repo.ClearMediaOrphan(ctx, object)
	}
}

func (s *Service) Media(ctx context.Context, id string) (core.MediaRecord, error) {
	return s.repo.GetMedia(ctx, id)
}

func (s *Service) OpenMedia(ctx context.Context, id string, offset, length int64, fetch bool) (io.ReadCloser, core.MediaRecord, bool, error) {
	record, err := s.repo.GetMedia(ctx, id)
	if err != nil {
		return nil, core.MediaRecord{}, false, err
	}
	if record.Availability == "unavailable" {
		return nil, record, false, core.ErrMediaUnavailable
	}
	if s.media.options.Stores == nil {
		return nil, record, false, core.ErrMediaStorage
	}
	if record.Availability == "ready" {
		store, ok := s.media.options.Stores.Get(record.StorageProfileID)
		if !ok {
			return nil, record, false, core.ErrMediaStorage
		}
		reader, err := store.Open(ctx, record.ObjectKey, offset, length)
		if err == nil {
			return reader, record, false, nil
		}
		if !errors.Is(err, media.ErrNotFound) {
			return nil, record, false, fmt.Errorf("%w: %v", core.ErrMediaStorage, err)
		}
		if len(record.ProviderRef) == 0 {
			if err := s.repo.MarkMediaUnavailable(ctx, id); err != nil {
				return nil, record, false, err
			}
			return nil, record, false, core.ErrMediaUnavailable
		}
		if err := s.repo.MarkMediaRemote(ctx, id, record.Version); err != nil {
			if errors.Is(err, core.ErrNotFound) {
				return nil, record, false, core.ErrMediaBusy
			}
			return nil, record, false, err
		}
		record.Availability = "remote"
	}
	if !fetch {
		return nil, record, true, nil
	}
	if len(record.ProviderRef) == 0 {
		return nil, record, false, core.ErrMediaUnavailable
	}
	if record.DeclaredSize > uint64(s.media.options.MaxFileBytes) {
		return nil, record, false, core.ErrMediaTooLarge
	}
	if record.FailureCode != "" {
		limit := s.media.options.MaxFileBytes
		failure := core.ErrMediaTooLarge
		if record.FailureCode == "quota" {
			s.media.mu.Lock()
			used, err := s.repo.StoredMediaBytes(ctx)
			available := s.media.options.MaxTotalBytes - used
			if available < 0 || s.media.reservedStored > available {
				available = 0
			} else {
				available -= s.media.reservedStored
			}
			s.media.mu.Unlock()
			if err != nil {
				return nil, record, false, err
			}
			limit = available
			failure = core.ErrMediaQuota
		}
		if record.RequiredBytes > limit {
			return nil, record, false, failure
		}
		if err := s.repo.ClearMediaBlock(ctx, id); err != nil {
			return nil, record, false, err
		}
	}
	rt, err := s.runtime(record.AccountID)
	if err != nil {
		return nil, record, false, err
	}
	rt.mu.RLock()
	connected := rt.state == "connected" && rt.session != nil
	rt.mu.RUnlock()
	if !connected {
		return nil, record, false, core.ErrNotConnected
	}
	if !s.queueMedia(id) {
		return nil, record, false, core.ErrMediaBusy
	}
	return nil, record, true, nil
}
