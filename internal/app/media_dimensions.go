package app

import (
	"bufio"
	"context"
	"image"
	_ "image/gif"
	"io"

	"github.com/notborges/convomeow/internal/core"
)

func imageDimensions(source io.Reader) (uint32, uint32) {
	reader := bufio.NewReader(io.LimitReader(source, 1<<20))
	header, _ := reader.Peek(30)
	if len(header) >= 30 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WEBP" {
		width, height, err := staticWebPDimensions(header)
		if err == nil {
			return width, height
		}
		return 0, 0
	}
	config, _, err := image.DecodeConfig(reader)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return 0, 0
	}
	return uint32(config.Width), uint32(config.Height)
}

func (s *Service) fillMediaDimensions(ctx context.Context, attachment *core.Attachment) {
	if attachment.Width > 0 && attachment.Height > 0 || attachment.Kind != core.MessageKindImage && attachment.Kind != core.MessageKindSticker {
		return
	}
	record, err := s.repo.GetMedia(ctx, attachment.ID)
	if err != nil || record.Availability != "ready" {
		return
	}
	store, ok := s.media.options.Stores.Get(record.StorageProfileID)
	if !ok {
		return
	}
	reader, err := store.Open(ctx, record.ObjectKey, 0, -1)
	if err != nil {
		return
	}
	defer reader.Close()
	width, height := imageDimensions(reader)
	if width == 0 || height == 0 {
		return
	}
	if err := s.repo.SetMediaDimensions(ctx, attachment.ID, width, height); err == nil {
		attachment.Width, attachment.Height = width, height
	}
}
