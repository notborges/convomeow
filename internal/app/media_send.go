package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/notborges/convomeow/internal/core"
	"github.com/notborges/convomeow/internal/media"
)

func (s *Service) SendMedia(ctx context.Context, conversationID string, kind core.MessageKind, uploadID, caption, key string) (core.Message, error) {
	if kind != core.MessageKindImage && kind != core.MessageKindVideo && kind != core.MessageKindAudio &&
		kind != core.MessageKindDocument && kind != core.MessageKindSticker {
		return core.Message{}, fmt.Errorf("%w: unsupported media kind", core.ErrInvalid)
	}
	if len(caption) > 1024 {
		return core.Message{}, fmt.Errorf("%w: caption exceeds 1024 bytes", core.ErrInvalid)
	}
	if (kind == core.MessageKindAudio || kind == core.MessageKindSticker) && caption != "" {
		return core.Message{}, fmt.Errorf("%w: this media kind does not support a caption", core.ErrInvalid)
	}
	if err := validateSendKey(key); err != nil {
		return core.Message{}, err
	}
	conversation, err := s.repo.GetConversation(ctx, conversationID)
	if err != nil {
		return core.Message{}, err
	}
	hash := sha256.Sum256([]byte(conversationID + "\x00" + string(kind) + "\x00" + uploadID + "\x00" + caption))
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
		Kind: kind, Text: strings.TrimSpace(caption), OccurredAt: now, IngestedAt: now}
	reserved, created, err := s.repo.ReserveMediaSend(ctx, message, actorID, key, requestHash, uploadID)
	if err != nil {
		return core.Message{}, err
	}
	if !created {
		return s.repo.GetMessage(ctx, reserved.ID)
	}
	s.scanPendingMedia()
	return reserved, nil
}

func validateSendKey(key string) error {
	if len(key) < 8 || len(key) > 128 {
		return fmt.Errorf("%w: Idempotency-Key must be 8-128 characters", core.ErrInvalid)
	}
	for _, char := range key {
		if char < 33 || char > 126 {
			return fmt.Errorf("%w: Idempotency-Key must contain printable ASCII without spaces", core.ErrInvalid)
		}
	}
	return nil
}

func (s *Service) scanMediaSends(ctx context.Context) {
	ids, err := s.repo.ListPendingMediaSends(ctx, time.Now().UTC(), 1000)
	if err != nil {
		s.logger.Warn("scan outgoing media failed", "error", err)
		return
	}
	for _, id := range ids {
		if !s.queueMediaSend(id) {
			break
		}
	}
}

func (s *Service) queueMediaSend(id string) bool {
	if s.media.sendQueue == nil || s.ctx.Err() != nil {
		return false
	}
	s.media.mu.Lock()
	defer s.media.mu.Unlock()
	if s.media.sendInflight[id] {
		return true
	}
	select {
	case s.media.sendQueue <- id:
		s.media.sendInflight[id] = true
		return true
	default:
		return false
	}
}

func (s *Service) mediaSendWorker() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case id := <-s.media.sendQueue:
			claimed := s.processMediaSend(id)
			s.media.mu.Lock()
			delete(s.media.sendInflight, id)
			s.media.mu.Unlock()
			if claimed {
				s.scanPendingMedia()
			}
		}
	}
}

func (s *Service) processMediaSend(id string) (claimed bool) {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Minute)
	defer cancel()
	message, err := s.repo.GetMessage(ctx, id)
	if err != nil {
		return
	}
	rt, err := s.runtime(message.AccountID)
	if err != nil {
		return
	}
	rt.mu.RLock()
	state, session := rt.state, rt.session
	rt.mu.RUnlock()
	if state != "connected" || session == nil {
		return
	}
	job, err := s.repo.ClaimMediaSend(ctx, id)
	if err != nil {
		return
	}
	claimed = true
	reserved := s.media.options.MaxFileBytes + (1 << 20)
	if err := s.reserveTemp(reserved); err != nil {
		s.retryMediaSend(job, err)
		return
	}
	defer s.releaseTemp(reserved)
	file, err := os.CreateTemp(s.media.options.TempDir, ".encrypt-*")
	if err != nil {
		s.retryMediaSend(job, err)
		return
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		s.retryMediaSend(job, err)
		return
	}
	store, ok := s.media.options.Stores.Get(job.ProfileID)
	if !ok {
		s.retryMediaSend(job, core.ErrMediaStorage)
		return
	}
	reader, err := store.Open(ctx, job.ObjectKey, 0, -1)
	if err != nil {
		if errors.Is(err, media.ErrNotFound) {
			s.failMediaSend(job, err)
			return
		}
		s.retryMediaSend(job, err)
		return
	}
	defer reader.Close()
	var source io.Reader = reader
	var width, height uint32
	if job.Kind == core.MessageKindSticker {
		var header [30]byte
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			s.failMediaSend(job, err)
			return
		}
		width, height, err = staticWebPDimensions(header[:])
		if err != nil {
			s.failMediaSend(job, err)
			return
		}
		source = io.MultiReader(bytes.NewReader(header[:]), reader)
	}
	uploaded, err := session.UploadMedia(ctx, job.Kind, io.LimitReader(source, job.Size+1), file)
	if err != nil {
		s.retryMediaSend(job, err)
		return
	}
	if uploaded == nil || uploaded.Size() != job.Size || !bytes.Equal(uploaded.SHA256(), job.SHA256) {
		s.failMediaSend(job, errors.New("stored file differs from uploaded file"))
		return
	}
	rt.mu.RLock()
	stillConnected := rt.state == "connected" && rt.session == session
	rt.mu.RUnlock()
	if !stillConnected {
		s.retryMediaSend(job, core.ErrNotConnected)
		return
	}
	if err := s.repo.BeginMediaSend(ctx, id); err != nil {
		s.retryMediaSend(job, err)
		return
	}
	prepared := core.PreparedMessage{ChatID: job.ChatID, ProviderMessageID: job.ProviderMessageID, SenderID: job.SenderID}
	mediaDescription := core.OutgoingMedia{Kind: job.Kind, MIMEType: job.MIMEType, FileName: job.FileName,
		Caption: job.Caption, Width: width, Height: height}
	sent, sendErr := session.SendMedia(ctx, prepared, mediaDescription, uploaded)
	result := "sent"
	if sendErr != nil || sent.ProviderMessageID != job.ProviderMessageID {
		result = "outcome_unknown"
		if errors.Is(sendErr, core.ErrInvalid) {
			result = "failed"
		}
		if sendErr != nil {
			s.logger.Warn("outgoing media send uncertain", "message_id", id, "error", sendErr)
		}
	}
	recordCtx, recordCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer recordCancel()
	if err := s.repo.CompleteMediaSend(recordCtx, id, result, sent); err != nil {
		s.logger.Error("record outgoing media result failed", "message_id", id, "error", err)
	}
	return
}

func (s *Service) retryMediaSend(job core.OutgoingMediaJob, cause error) {
	if s.ctx.Err() != nil {
		return
	}
	rt, err := s.runtime(job.AccountID)
	if err != nil {
		return
	}
	rt.mu.RLock()
	state := rt.state
	rt.mu.RUnlock()
	if state == "needs_login" {
		return
	}
	if errors.Is(cause, core.ErrInvalid) || errors.Is(cause, core.ErrMediaTooLarge) {
		s.failMediaSend(job, cause)
		return
	}
	attempts := job.Attempts + 1
	delay := time.Duration(1<<min(attempts, 5)) * 15 * time.Second
	if state != "connected" || errors.Is(cause, core.ErrNotConnected) {
		attempts = job.Attempts
		delay = 0
	}
	if attempts >= 5 {
		s.failMediaSend(job, cause)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.repo.RetryMediaSend(ctx, job.MessageID, time.Now().UTC().Add(delay), attempts); err != nil {
		s.logger.Error("schedule outgoing media retry failed", "message_id", job.MessageID, "error", err)
	}
	if delay != 0 {
		s.logger.Warn("outgoing media upload failed", "message_id", job.MessageID, "error", cause)
	}
}

func (s *Service) failMediaSend(job core.OutgoingMediaJob, cause error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.repo.CompleteMediaSend(ctx, job.MessageID, "failed", core.SentMessage{}); err != nil {
		s.logger.Error("record outgoing media failure failed", "message_id", job.MessageID, "error", err)
	}
	s.logger.Warn("outgoing media failed", "message_id", job.MessageID, "error", cause)
}
