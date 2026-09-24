package core

import (
	"fmt"
	"mime"
)

func ValidateMediaType(kind MessageKind, value string) error {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return fmt.Errorf("%w: invalid MIME type", ErrInvalid)
	}
	valid := false
	switch kind {
	case MessageKindImage:
		valid = mediaType == "image/jpeg" || mediaType == "image/png" || mediaType == "image/webp"
	case MessageKindVideo:
		valid = mediaType == "video/mp4" || mediaType == "video/3gpp"
	case MessageKindAudio:
		valid = mediaType == "audio/mpeg" || mediaType == "audio/mp4" || mediaType == "audio/ogg" ||
			mediaType == "audio/aac" || mediaType == "audio/amr"
	case MessageKindDocument:
		valid = mediaType != ""
	case MessageKindSticker:
		valid = mediaType == "image/webp"
	}
	if !valid {
		return fmt.Errorf("%w: MIME type is not supported for %s", ErrInvalid, kind)
	}
	return nil
}
