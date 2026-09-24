package v1

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/notborges/convomeow/internal/core"
)

func (s *Server) getAttachment(w http.ResponseWriter, r *http.Request) {
	record, err := s.service.Media(r.Context(), r.PathValue("id"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, attachmentFromRecord(record))
}

func (s *Server) getAttachmentContent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	record, err := s.service.Media(r.Context(), r.PathValue("id"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	var offset, length int64
	length = -1
	partial := false
	if record.Availability == "ready" && r.Header.Get("Range") != "" {
		offset, length, err = parseMediaRange(r.Header.Get("Range"), record.StoredSize)
		if err != nil {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", record.StoredSize))
			writeProblem(w, r, http.StatusRequestedRangeNotSatisfiable, "invalid_range", "Invalid media byte range.")
			return
		}
		partial = true
	}
	reader, current, pending, err := s.service.OpenMedia(r.Context(), record.AttachmentID, offset, length, r.Method != http.MethodHead)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if pending {
		w.Header().Set("Retry-After", "2")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusAccepted)
		} else {
			writeJSON(w, http.StatusAccepted, attachmentFromRecord(current))
		}
		return
	}
	defer reader.Close()
	contentType, disposition := mediaHeaders(current)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Accept-Ranges", "bytes")
	size := current.StoredSize
	status := http.StatusOK
	if partial {
		status = http.StatusPartialContent
		size = length
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, offset+length-1, current.StoredSize))
	}
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = io.CopyN(w, reader, size)
	}
}

func parseMediaRange(raw string, size int64) (int64, int64, error) {
	if size < 1 || !strings.HasPrefix(raw, "bytes=") || strings.Contains(raw, ",") {
		return 0, 0, core.ErrInvalid
	}
	startText, endText, ok := strings.Cut(strings.TrimPrefix(raw, "bytes="), "-")
	if !ok || startText == "" && endText == "" {
		return 0, 0, core.ErrInvalid
	}
	if startText == "" {
		suffix, err := strconv.ParseInt(endText, 10, 64)
		if err != nil || suffix < 1 {
			return 0, 0, core.ErrInvalid
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, suffix, nil
	}
	start, err := strconv.ParseInt(startText, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, core.ErrInvalid
	}
	end := size - 1
	if endText != "" {
		end, err = strconv.ParseInt(endText, 10, 64)
		if err != nil || end < start {
			return 0, 0, core.ErrInvalid
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end - start + 1, nil
}

func mediaHeaders(record core.MediaRecord) (string, string) {
	allowed := map[string]bool{
		"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true, "image/avif": true,
		"audio/mpeg": true, "audio/ogg": true, "audio/mp4": true, "audio/wav": true, "audio/webm": true,
		"video/mp4": true, "video/webm": true, "video/ogg": true, "video/quicktime": true,
	}
	mediaType, _, err := mime.ParseMediaType(record.MIMEType)
	if err != nil || !allowed[mediaType] || record.Kind == core.MessageKindDocument {
		mediaType = "application/octet-stream"
	}
	disposition := "inline"
	if mediaType == "application/octet-stream" {
		name := safeMediaFilename(record.FileName)
		disposition = mime.FormatMediaType("attachment", map[string]string{"filename": name})
	}
	return mediaType, disposition
}

func safeMediaFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	parts := strings.Split(name, "/")
	name = parts[len(parts)-1]
	var safe strings.Builder
	for _, r := range name {
		if r >= 32 && r != 127 && safe.Len() < 120 {
			safe.WriteRune(r)
		}
	}
	name = strings.TrimSpace(safe.String())
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	return name
}
