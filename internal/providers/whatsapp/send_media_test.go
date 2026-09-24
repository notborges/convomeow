package whatsapp

import (
	"testing"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow"
)

func TestBuildMediaMessage(t *testing.T) {
	token := whatsmeow.UploadResponse{URL: "https://example.invalid/media", DirectPath: "/media/path", MediaKey: []byte("key"),
		FileSHA256: make([]byte, 32), FileEncSHA256: make([]byte, 32), FileLength: 42}
	for _, test := range []struct {
		kind core.MessageKind
		mime string
	}{
		{core.MessageKindImage, "image/png"}, {core.MessageKindVideo, "video/mp4"},
		{core.MessageKindAudio, "audio/mpeg"}, {core.MessageKindDocument, "application/pdf"},
		{core.MessageKindSticker, "image/webp"},
	} {
		caption := "caption"
		if test.kind == core.MessageKindAudio || test.kind == core.MessageKindSticker {
			caption = ""
		}
		media := core.OutgoingMedia{Kind: test.kind, MIMEType: test.mime, FileName: "file.pdf", Caption: caption, Width: 512, Height: 512}
		message, err := buildMediaMessage(media, token)
		if err != nil {
			t.Fatalf("%s: %v", test.kind, err)
		}
		switch test.kind {
		case core.MessageKindImage:
			if message.GetImageMessage().GetCaption() != caption || message.GetImageMessage().GetFileLength() != 42 {
				t.Fatal("image payload")
			}
		case core.MessageKindVideo:
			if message.GetVideoMessage().GetCaption() != caption || message.GetVideoMessage().GetFileLength() != 42 {
				t.Fatal("video payload")
			}
		case core.MessageKindAudio:
			if message.GetAudioMessage().GetPTT() || message.GetAudioMessage().GetFileLength() != 42 {
				t.Fatal("audio payload")
			}
		case core.MessageKindDocument:
			if message.GetDocumentMessage().GetFileName() != "file.pdf" || message.GetDocumentMessage().GetCaption() != caption {
				t.Fatal("document payload")
			}
		case core.MessageKindSticker:
			if message.GetStickerMessage().GetIsAnimated() || message.GetStickerMessage().GetFileLength() != 42 || message.GetStickerMessage().GetWidth() != 512 {
				t.Fatal("sticker payload")
			}
		}
	}
	if _, err := buildMediaMessage(core.OutgoingMedia{Kind: core.MessageKindSticker, MIMEType: "image/png"}, token); err == nil {
		t.Fatal("PNG sticker accepted")
	}
	if _, err := buildMediaMessage(core.OutgoingMedia{Kind: core.MessageKindImage, MIMEType: "image/png"}, whatsmeow.UploadResponse{}); err == nil {
		t.Fatal("incomplete upload accepted")
	}
}
