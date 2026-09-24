package app

import (
	"encoding/binary"
	"testing"
)

func TestStaticWebPDimensions(t *testing.T) {
	header := make([]byte, 30)
	copy(header[:4], "RIFF")
	copy(header[8:12], "WEBP")
	copy(header[12:16], "VP8X")
	binary.LittleEndian.PutUint32(header[16:20], 10)
	header[24], header[27] = 1, 2
	width, height, err := staticWebPDimensions(header)
	if err != nil || width != 2 || height != 3 {
		t.Fatalf("dimensions: %d x %d, %v", width, height, err)
	}
	header[20] = 0x02
	if _, _, err := staticWebPDimensions(header); err == nil {
		t.Fatal("animated WebP accepted")
	}
}
