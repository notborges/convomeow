package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waMmsRetry"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type mediaReference struct {
	DirectPath    string `json:"direct_path"`
	MediaKey      []byte `json:"media_key"`
	FileSHA256    []byte `json:"file_sha256"`
	FileEncSHA256 []byte `json:"file_enc_sha256"`
	MediaType     string `json:"media_type"`
}

func mediaAttachment(message *waE2E.Message, kind core.MessageKind, viewOnce bool) *core.Attachment {
	var downloadable whatsmeow.DownloadableMessage
	attachment := &core.Attachment{Kind: kind, Availability: "remote"}
	switch kind {
	case core.MessageKindImage:
		media := message.GetImageMessage()
		downloadable, attachment.MIMEType, attachment.Size = media, media.GetMimetype(), media.GetFileLength()
	case core.MessageKindVideo:
		media := message.GetVideoMessage()
		downloadable, attachment.MIMEType, attachment.Size = media, media.GetMimetype(), media.GetFileLength()
	case core.MessageKindAudio:
		media := message.GetAudioMessage()
		downloadable, attachment.MIMEType, attachment.Size = media, media.GetMimetype(), media.GetFileLength()
	case core.MessageKindDocument:
		media := message.GetDocumentMessage()
		downloadable, attachment.MIMEType, attachment.FileName, attachment.Size = media, media.GetMimetype(), media.GetFileName(), media.GetFileLength()
	case core.MessageKindSticker:
		media := message.GetStickerMessage()
		downloadable, attachment.MIMEType, attachment.Size = media, media.GetMimetype(), media.GetFileLength()
	default:
		return nil
	}
	if downloadable == nil {
		return nil
	}
	if attachment.Size > math.MaxInt64 {
		attachment.Size = 0
	}
	if viewOnce || len(downloadable.GetMediaKey()) == 0 || len(downloadable.GetFileSHA256()) != 32 || len(downloadable.GetFileEncSHA256()) != 32 {
		attachment.Availability = "unavailable"
	}
	if attachment.Availability == "unavailable" {
		return attachment
	}
	ref, err := json.Marshal(mediaReference{DirectPath: downloadable.GetDirectPath(), MediaKey: downloadable.GetMediaKey(),
		FileSHA256: downloadable.GetFileSHA256(), FileEncSHA256: downloadable.GetFileEncSHA256(),
		MediaType: string(whatsmeow.GetMediaType(downloadable))})
	if err != nil {
		return attachment
	}
	attachment.ProviderRef = ref
	return attachment
}

type limitedFile struct {
	*os.File
	limit int64
}

func (f *limitedFile) Write(p []byte) (int, error) {
	position, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	if position > f.limit || int64(len(p)) > f.limit-position {
		return 0, core.ErrMediaTooLarge
	}
	return f.File.Write(p)
}

func (f *limitedFile) WriteAt(p []byte, offset int64) (int, error) {
	if offset < 0 || offset > f.limit || int64(len(p)) > f.limit-offset {
		return 0, core.ErrMediaTooLarge
	}
	return f.File.WriteAt(p, offset)
}

func (f *limitedFile) Truncate(size int64) error {
	if size < 0 || size > f.limit {
		return core.ErrMediaTooLarge
	}
	return f.File.Truncate(size)
}

func (s *session) downloadMedia(ctx context.Context, source core.MediaSource, file *os.File, maxBytes int64) ([]byte, error) {
	if maxBytes < 1 || maxBytes > math.MaxInt64-(1<<20) {
		return nil, core.ErrInvalid
	}
	var ref mediaReference
	if len(source.ProviderRef) == 0 || json.Unmarshal(source.ProviderRef, &ref) != nil || ref.MediaType == "" ||
		len(ref.MediaKey) == 0 || len(ref.FileSHA256) != 32 || len(ref.FileEncSHA256) != 32 {
		return nil, core.ErrMediaUnavailable
	}
	bounded := &limitedFile{File: file, limit: maxBytes + (1 << 20)}
	if ref.DirectPath != "" {
		err := s.client.DownloadMediaWithPathToFile(ctx, ref.DirectPath, ref.FileEncSHA256, ref.FileSHA256,
			ref.MediaKey, whatsmeow.MediaType(ref.MediaType), "", false, bounded)
		if err == nil {
			return nil, checkDecryptedSize(file, maxBytes)
		}
		if !errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) && !errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410) {
			return nil, err
		}
	}
	if len(ref.MediaKey) == 0 {
		return nil, core.ErrMediaUnavailable
	}
	path, err := s.retryMedia(ctx, source, ref.MediaKey)
	if err != nil {
		return nil, err
	}
	ref.DirectPath = path
	updated, err := json.Marshal(ref)
	if err != nil {
		return nil, err
	}
	if err := file.Truncate(0); err != nil {
		return updated, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return updated, err
	}
	if err := s.client.DownloadMediaWithPathToFile(ctx, ref.DirectPath, ref.FileEncSHA256, ref.FileSHA256,
		ref.MediaKey, whatsmeow.MediaType(ref.MediaType), "", false, bounded); err != nil {
		return updated, err
	}
	return updated, checkDecryptedSize(file, maxBytes)
}

func checkDecryptedSize(file *os.File, limit int64) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > limit {
		return core.ErrMediaTooLarge
	}
	return nil
}

func (s *session) retryMedia(ctx context.Context, source core.MediaSource, key []byte) (string, error) {
	chat, err := types.ParseJID(source.ChatID)
	if err != nil || chat.IsEmpty() || source.ProviderMessageID == "" {
		return "", core.ErrMediaUnavailable
	}
	var sender types.JID
	if source.SenderID != "" {
		sender, err = types.ParseJID(source.SenderID)
		if err != nil {
			return "", core.ErrMediaUnavailable
		}
	}
	id := types.MessageID(source.ProviderMessageID)
	waiter := make(chan *events.MediaRetry, 1)
	s.retryMu.Lock()
	if s.retryWaiters[id] != nil {
		s.retryMu.Unlock()
		return "", core.ErrConflict
	}
	if len(s.retryWaiters) >= 32 {
		s.retryMu.Unlock()
		return "", core.ErrMediaBusy
	}
	s.retryWaiters[id] = waiter
	s.retryMu.Unlock()
	defer func() {
		s.retryMu.Lock()
		if s.retryWaiters[id] == waiter {
			delete(s.retryWaiters, id)
		}
		s.retryMu.Unlock()
	}()
	info := &types.MessageInfo{MessageSource: types.MessageSource{Chat: chat, Sender: sender,
		IsFromMe: source.Direction == "outbound", IsGroup: chat.Server == types.GroupServer}, ID: id}
	if err := s.client.SendMediaRetryReceipt(ctx, info, key); err != nil {
		return "", err
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case evt, ok := <-waiter:
		if !ok {
			return "", core.ErrNotConnected
		}
		result, err := whatsmeow.DecryptMediaRetryNotification(evt, key)
		if errors.Is(err, whatsmeow.ErrMediaNotAvailableOnPhone) {
			return "", core.ErrMediaUnavailable
		}
		if err != nil {
			return "", err
		}
		if result.GetResult() == waMmsRetry.MediaRetryNotification_NOT_FOUND {
			return "", core.ErrMediaUnavailable
		}
		if result.GetResult() != waMmsRetry.MediaRetryNotification_SUCCESS || result.GetDirectPath() == "" {
			return "", fmt.Errorf("media retry failed: %s", result.GetResult())
		}
		return result.GetDirectPath(), nil
	case <-timer.C:
		return "", context.DeadlineExceeded
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (s *session) closeRetryWaiters() {
	s.retryMu.Lock()
	for id, waiter := range s.retryWaiters {
		close(waiter)
		delete(s.retryWaiters, id)
	}
	s.retryMu.Unlock()
}
