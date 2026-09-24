package app

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/core"
	"github.com/notborges/convomeow/internal/media"
)

func (s *Service) MaxUploadBytes() int64 { return s.media.options.MaxFileBytes }

func (s *Service) reserveTemp(size int64) error {
	s.media.mu.Lock()
	defer s.media.mu.Unlock()
	if size > s.media.options.MaxTempBytes-s.media.reservedTemp {
		return core.ErrMediaBusy
	}
	s.media.reservedTemp += size
	return nil
}

func (s *Service) releaseTemp(size int64) {
	s.media.mu.Lock()
	s.media.reservedTemp -= size
	s.media.mu.Unlock()
}

func (s *Service) CreateUpload(ctx context.Context, accountID, fileName, declaredType string, data io.Reader) (core.Upload, error) {
	if _, err := s.runtime(accountID); err != nil {
		return core.Upload{}, err
	}
	if s.media.options.Stores == nil {
		return core.Upload{}, core.ErrMediaStorage
	}
	name, err := cleanFileName(fileName)
	if err != nil {
		return core.Upload{}, err
	}
	reserved := s.media.options.MaxFileBytes + (1 << 20)
	if err := s.reserveTemp(reserved); err != nil {
		return core.Upload{}, err
	}
	defer s.releaseTemp(reserved)
	file, err := os.CreateTemp(s.media.options.TempDir, ".upload-*")
	if err != nil {
		return core.Upload{}, fmt.Errorf("%w: create upload staging file: %v", core.ErrMediaStorage, err)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return core.Upload{}, err
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(data, s.media.options.MaxFileBytes+1))
	if err != nil {
		return core.Upload{}, err
	}
	if size > s.media.options.MaxFileBytes {
		return core.Upload{}, core.ErrMediaTooLarge
	}
	if size == 0 {
		return core.Upload{}, fmt.Errorf("%w: file is empty", core.ErrInvalid)
	}
	mediaType, err := detectUploadType(file, declaredType)
	if err != nil {
		return core.Upload{}, err
	}
	if err := s.reserveMediaStorage(ctx, size); err != nil {
		return core.Upload{}, err
	}
	defer s.releaseMediaStorage(size)
	id := uuid.NewString()
	key, err := media.Key(accountID, id)
	if err != nil {
		return core.Upload{}, err
	}
	profileID, store := s.media.options.Stores.Active()
	upload := core.Upload{ID: id, AccountID: accountID, ProfileID: profileID, ObjectKey: key,
		MIMEType: mediaType, FileName: name, Size: size, SHA256: hash.Sum(nil), ExpiresAt: time.Now().UTC().Add(24 * time.Hour)}
	if err := s.repo.CreateUpload(ctx, upload); err != nil {
		return core.Upload{}, err
	}
	if err := store.Put(ctx, key, file, size); err != nil {
		s.discardUpload(upload)
		return core.Upload{}, fmt.Errorf("%w: store upload: %v", core.ErrMediaStorage, err)
	}
	if err := s.repo.MarkUploadReady(ctx, id); err != nil {
		s.discardUpload(upload)
		return core.Upload{}, err
	}
	upload.State = "ready"
	return upload, nil
}

func (s *Service) discardUpload(upload core.Upload) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.repo.MarkUploadDeleting(ctx, upload.AccountID, upload.ID); err != nil {
		s.logger.Warn("mark upload for cleanup failed", "upload_id", upload.ID, "error", err)
	}
	s.scanPendingMedia()
}

func (s *Service) DeleteUpload(ctx context.Context, accountID, uploadID string) error {
	if _, err := s.runtime(accountID); err != nil {
		return err
	}
	if err := s.repo.MarkUploadDeleting(ctx, accountID, uploadID); err != nil {
		return err
	}
	s.scanPendingMedia()
	return nil
}

func (s *Service) cleanUploads(ctx context.Context) {
	uploads, err := s.repo.ListUploadsForCleanup(ctx, time.Now().UTC(), 100)
	if err != nil {
		s.logger.Warn("scan uploads failed", "error", err)
		return
	}
	for _, upload := range uploads {
		store, ok := s.media.options.Stores.Get(upload.ProfileID)
		if !ok {
			continue
		}
		if err := store.Delete(ctx, upload.ObjectKey); err != nil {
			s.logger.Warn("delete upload failed", "upload_id", upload.ID, "error", err)
			continue
		}
		if err := s.repo.ClearUpload(ctx, upload.ID); err != nil {
			s.logger.Warn("clear upload record failed", "upload_id", upload.ID, "error", err)
		}
	}
}

func cleanFileName(value string) (string, error) {
	name := path.Base(strings.ReplaceAll(value, "\\", "/"))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("%w: file name is required", core.ErrInvalid)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: file name contains control characters", core.ErrInvalid)
		}
	}
	if !utf8.ValidString(name) || len(name) > 200 {
		return "", fmt.Errorf("%w: file name is too long or invalid", core.ErrInvalid)
	}
	return name, nil
}

func detectUploadType(file *os.File, declared string) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	var first [512]byte
	n, err := file.Read(first[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	sniffed := http.DetectContentType(first[:n])
	if n >= 12 && string(first[:4]) == "RIFF" && string(first[8:12]) == "WEBP" {
		sniffed = "image/webp"
	}
	declared, _, err = mime.ParseMediaType(declared)
	if err != nil || declared == "" {
		return "", fmt.Errorf("%w: file Content-Type is invalid", core.ErrInvalid)
	}
	if declared == "application/octet-stream" && sniffed != "application/octet-stream" {
		declared = sniffed
	}
	compatible := (sniffed == "application/ogg" && declared == "audio/ogg") ||
		(sniffed == "video/mp4" && (declared == "audio/mp4" || declared == "video/3gpp"))
	if sniffed != "application/octet-stream" && sniffed != "text/plain; charset=utf-8" && declared != sniffed && !compatible {
		return "", fmt.Errorf("%w: file Content-Type does not match its data", core.ErrInvalid)
	}
	if declared == "image/jpeg" || declared == "image/png" {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return "", err
		}
		config, format, err := image.DecodeConfig(file)
		if err != nil || config.Width < 1 || config.Height < 1 || "image/"+format != declared {
			return "", fmt.Errorf("%w: invalid image", core.ErrInvalid)
		}
	}
	if declared == "image/webp" {
		if _, _, err := staticWebPDimensions(first[:n]); err != nil {
			return "", err
		}
	}
	return declared, nil
}

func staticWebPDimensions(header []byte) (uint32, uint32, error) {
	if len(header) < 30 || string(header[:4]) != "RIFF" || string(header[8:12]) != "WEBP" {
		return 0, 0, fmt.Errorf("%w: invalid WebP", core.ErrInvalid)
	}
	var width, height uint32
	switch string(header[12:16]) {
	case "VP8X":
		if binary.LittleEndian.Uint32(header[16:20]) < 10 || header[20]&0x02 != 0 {
			return 0, 0, fmt.Errorf("%w: animated or invalid WebP", core.ErrInvalid)
		}
		width = 1 + uint32(header[24]) + uint32(header[25])<<8 + uint32(header[26])<<16
		height = 1 + uint32(header[27]) + uint32(header[28])<<8 + uint32(header[29])<<16
	case "VP8 ":
		if string(header[23:26]) != "\x9d\x01\x2a" {
			return 0, 0, fmt.Errorf("%w: invalid WebP", core.ErrInvalid)
		}
		width = uint32(binary.LittleEndian.Uint16(header[26:28]) & 0x3fff)
		height = uint32(binary.LittleEndian.Uint16(header[28:30]) & 0x3fff)
	case "VP8L":
		if header[20] != 0x2f {
			return 0, 0, fmt.Errorf("%w: invalid WebP", core.ErrInvalid)
		}
		width = 1 + uint32(header[21]) + uint32(header[22]&0x3f)<<8
		height = 1 + uint32(header[22]>>6) + uint32(header[23])<<2 + uint32(header[24]&0x0f)<<10
	default:
		return 0, 0, fmt.Errorf("%w: invalid WebP", core.ErrInvalid)
	}
	if width == 0 || height == 0 || width > 16384 || height > 16384 {
		return 0, 0, fmt.Errorf("%w: invalid WebP dimensions", core.ErrInvalid)
	}
	return width, height, nil
}
