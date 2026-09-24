package v1

import (
	"strings"
	"testing"

	"github.com/notborges/convomeow/internal/core"
)

func TestMediaResponseSafety(t *testing.T) {
	contentType, disposition := mediaHeaders(core.MediaRecord{Kind: core.MessageKindDocument,
		MIMEType: "image/svg+xml", FileName: "../report\r\nX-Injection: yes.svg"})
	if contentType != "application/octet-stream" || !strings.HasPrefix(disposition, "attachment;") ||
		strings.ContainsAny(disposition, "\r\n") || strings.Contains(disposition, "../") {
		t.Fatalf("unsafe media headers: %q, %q", contentType, disposition)
	}
	contentType, disposition = mediaHeaders(core.MediaRecord{Kind: core.MessageKindImage, MIMEType: "image/jpeg"})
	if contentType != "image/jpeg" || disposition != "inline" {
		t.Fatalf("image headers: %q, %q", contentType, disposition)
	}
	start, length, err := parseMediaRange("bytes=-4", 10)
	if err != nil || start != 6 || length != 4 {
		t.Fatalf("suffix range: %d, %d, %v", start, length, err)
	}
	if _, _, err := parseMediaRange("bytes=0-1,3-4", 10); err == nil {
		t.Fatal("multiple ranges were accepted")
	}
}
