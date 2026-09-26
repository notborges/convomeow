package app

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestImageDimensions(t *testing.T) {
	for _, size := range []image.Point{{X: 20, Y: 60}, {X: 120, Y: 20}} {
		var data bytes.Buffer
		if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, size.X, size.Y))); err != nil {
			t.Fatal(err)
		}
		w, h := imageDimensions(&data)
		if w != uint32(size.X) || h != uint32(size.Y) {
			t.Fatalf("got %dx%d, want %v", w, h, size)
		}
	}
	if w, h := imageDimensions(strings.NewReader("not an image")); w != 0 || h != 0 {
		t.Fatal("invalid file returned dimensions")
	}
}
