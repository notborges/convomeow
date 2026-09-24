package whatsapp

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type uploadedMedia struct{ response whatsmeow.UploadResponse }

func (u uploadedMedia) Size() int64    { return int64(u.response.FileLength) }
func (u uploadedMedia) SHA256() []byte { return u.response.FileSHA256 }

func whatsappMediaType(kind core.MessageKind) (whatsmeow.MediaType, error) {
	switch kind {
	case core.MessageKindImage, core.MessageKindSticker:
		return whatsmeow.MediaImage, nil
	case core.MessageKindVideo:
		return whatsmeow.MediaVideo, nil
	case core.MessageKindAudio:
		return whatsmeow.MediaAudio, nil
	case core.MessageKindDocument:
		return whatsmeow.MediaDocument, nil
	default:
		return "", fmt.Errorf("%w: unsupported media kind", core.ErrInvalid)
	}
}

func (s *session) UploadMedia(ctx context.Context, kind core.MessageKind, data io.Reader, scratch *os.File) (core.UploadedMedia, error) {
	mediaType, err := whatsappMediaType(kind)
	if err != nil {
		return nil, err
	}
	response, err := s.client.UploadReader(ctx, data, scratch, mediaType)
	if err != nil {
		return nil, err
	}
	return uploadedMedia{response: response}, nil
}

func (s *session) SendMedia(ctx context.Context, prepared core.PreparedMessage, media core.OutgoingMedia, uploaded core.UploadedMedia) (core.SentMessage, error) {
	jid, err := recipientJID(prepared.ChatID)
	if err != nil {
		return core.SentMessage{}, err
	}
	if prepared.ProviderMessageID == "" {
		return core.SentMessage{}, fmt.Errorf("%w: missing provider message ID", core.ErrInvalid)
	}
	token, ok := uploaded.(uploadedMedia)
	if !ok {
		return core.SentMessage{}, fmt.Errorf("%w: invalid media upload", core.ErrInvalid)
	}
	message, err := buildMediaMessage(media, token.response)
	if err != nil {
		return core.SentMessage{}, err
	}
	response, err := s.client.SendMessage(ctx, jid, message, whatsmeow.SendRequestExtra{ID: types.MessageID(prepared.ProviderMessageID)})
	if err != nil {
		return core.SentMessage{}, err
	}
	chat := response.Chat
	if chat.IsEmpty() {
		chat = jid
	}
	return core.SentMessage{ChatID: chat.String(), ProviderMessageID: string(response.ID), SenderID: prepared.SenderID, Timestamp: response.Timestamp}, nil
}

func buildMediaMessage(media core.OutgoingMedia, token whatsmeow.UploadResponse) (*waE2E.Message, error) {
	if err := core.ValidateMediaType(media.Kind, media.MIMEType); err != nil {
		return nil, err
	}
	if token.URL == "" || token.DirectPath == "" || len(token.MediaKey) == 0 || len(token.FileSHA256) != 32 ||
		len(token.FileEncSHA256) != 32 || token.FileLength == 0 {
		return nil, fmt.Errorf("%w: incomplete media upload", core.ErrInvalid)
	}
	message := &waE2E.Message{}
	switch media.Kind {
	case core.MessageKindImage:
		message.ImageMessage = &waE2E.ImageMessage{URL: proto.String(token.URL), DirectPath: proto.String(token.DirectPath),
			MediaKey: token.MediaKey, FileSHA256: token.FileSHA256, FileEncSHA256: token.FileEncSHA256,
			FileLength: proto.Uint64(token.FileLength), Mimetype: proto.String(media.MIMEType), Caption: proto.String(media.Caption)}
	case core.MessageKindVideo:
		message.VideoMessage = &waE2E.VideoMessage{URL: proto.String(token.URL), DirectPath: proto.String(token.DirectPath),
			MediaKey: token.MediaKey, FileSHA256: token.FileSHA256, FileEncSHA256: token.FileEncSHA256,
			FileLength: proto.Uint64(token.FileLength), Mimetype: proto.String(media.MIMEType), Caption: proto.String(media.Caption)}
	case core.MessageKindAudio:
		if media.Caption != "" {
			return nil, core.ErrInvalid
		}
		message.AudioMessage = &waE2E.AudioMessage{URL: proto.String(token.URL), DirectPath: proto.String(token.DirectPath),
			MediaKey: token.MediaKey, FileSHA256: token.FileSHA256, FileEncSHA256: token.FileEncSHA256,
			FileLength: proto.Uint64(token.FileLength), Mimetype: proto.String(media.MIMEType), PTT: proto.Bool(false)}
	case core.MessageKindDocument:
		message.DocumentMessage = &waE2E.DocumentMessage{URL: proto.String(token.URL), DirectPath: proto.String(token.DirectPath),
			MediaKey: token.MediaKey, FileSHA256: token.FileSHA256, FileEncSHA256: token.FileEncSHA256,
			FileLength: proto.Uint64(token.FileLength), Mimetype: proto.String(media.MIMEType), FileName: proto.String(media.FileName), Caption: proto.String(media.Caption)}
	case core.MessageKindSticker:
		if media.Caption != "" {
			return nil, core.ErrInvalid
		}
		if media.Width == 0 || media.Height == 0 {
			return nil, fmt.Errorf("%w: missing sticker dimensions", core.ErrInvalid)
		}
		message.StickerMessage = &waE2E.StickerMessage{URL: proto.String(token.URL), DirectPath: proto.String(token.DirectPath),
			MediaKey: token.MediaKey, FileSHA256: token.FileSHA256, FileEncSHA256: token.FileEncSHA256,
			FileLength: proto.Uint64(token.FileLength), Mimetype: proto.String(media.MIMEType), IsAnimated: proto.Bool(false),
			Width: proto.Uint32(media.Width), Height: proto.Uint32(media.Height)}
	default:
		return nil, core.ErrInvalid
	}
	return message, nil
}
