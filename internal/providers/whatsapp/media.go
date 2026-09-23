package whatsapp

import (
	"encoding/json"
	"math"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
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
	if viewOnce || downloadable.GetDirectPath() == "" {
		attachment.Availability = "unavailable"
	}
	if viewOnce {
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
