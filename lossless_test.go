package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestRotatePreservesConcreteType checks that geometric transforms keep the
// source's concrete type (and therefore its bit depth / alpha mode / palette)
// rather than flattening everything to 8-bit RGBA.
func TestRotatePreservesConcreteType(t *testing.T) {
	cases := []struct {
		name string
		img  image.Image
	}{
		{"NRGBA64", image.NewNRGBA64(image.Rect(0, 0, 3, 2))},
		{"RGBA64", image.NewRGBA64(image.Rect(0, 0, 3, 2))},
		{"Gray16", image.NewGray16(image.Rect(0, 0, 3, 2))},
		{"Gray", image.NewGray(image.Rect(0, 0, 3, 2))},
		{"NRGBA", image.NewNRGBA(image.Rect(0, 0, 3, 2))},
		{"Paletted", image.NewPaletted(image.Rect(0, 0, 3, 2), color.Palette{color.Black, color.White})},
	}
	for _, c := range cases {
		gotR := reflect.TypeOf(rotateImage(c.img, 90))
		gotC := reflect.TypeOf(crop(image.Rect(0, 0, 2, 2))(c.img))
		want := reflect.TypeOf(c.img)
		if gotR != want {
			t.Errorf("%s: rotate produced %v, want %v", c.name, gotR, want)
		}
		if gotC != want {
			t.Errorf("%s: crop produced %v, want %v", c.name, gotC, want)
		}
	}
}

// TestRotate16BitLossless rotates a 16-bit image and confirms full-precision
// pixel values survive (the old RGBA path truncated these to 8 bits).
func TestRotate16BitLossless(t *testing.T) {
	const w, h = 3, 2
	src := image.NewNRGBA64(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			src.SetNRGBA64(x, y, color.NRGBA64{
				R: uint16(0x0100 + x), G: uint16(0x0200 + y), B: 0x3456, A: 0xfffe,
			})
		}
	}
	dst := rotateImage(src, 90).(*image.NRGBA64) // 90° CW: src(x,y) -> dst(h-1-y, x)
	if dst.Bounds().Dx() != h || dst.Bounds().Dy() != w {
		t.Fatalf("bounds = %v, want %dx%d", dst.Bounds(), h, w)
	}
	for y := range h {
		for x := range w {
			got := dst.NRGBA64At(h-1-y, x)
			want := src.NRGBA64At(x, y)
			if got != want {
				t.Errorf("dst(%d,%d) = %+v, want %+v", h-1-y, x, got, want)
			}
		}
	}
}

// TestPalettedCropLossless confirms the palette and exact indices are preserved.
func TestPalettedCropLossless(t *testing.T) {
	pal := color.Palette{color.Black, color.RGBA{1, 2, 3, 255}, color.White}
	src := image.NewPaletted(image.Rect(0, 0, 4, 2), pal)
	for y := range 2 {
		for x := range 4 {
			src.SetColorIndex(x, y, uint8((x+y)%3))
		}
	}
	dst := crop(image.Rect(1, 0, 4, 2))(src).(*image.Paletted)
	if !reflect.DeepEqual(dst.Palette, pal) {
		t.Fatalf("palette changed: %v", dst.Palette)
	}
	for y := range 2 {
		for x := range 3 {
			if got, want := dst.ColorIndexAt(x, y), src.ColorIndexAt(x+1, y); got != want {
				t.Errorf("index(%d,%d) = %d, want %d", x, y, got, want)
			}
		}
	}
}

// TestWritePNG16BitRoundTrip exercises the full PNG pipeline (load -> transform
// -> encode -> decode) and verifies a 16-bit image keeps its bit depth and
// exact pixel values on disk.
func TestWritePNG16BitRoundTrip(t *testing.T) {
	const w, h = 4, 3
	src := image.NewNRGBA64(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			src.SetNRGBA64(x, y, color.NRGBA64{
				R: uint16(0x1111 * (x + 1)), G: uint16(0x2222 + y), B: 0xabcd, A: uint16(0x8000 + x),
			})
		}
	}

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, carry, err := loadPNG(srcPath)
	if err != nil {
		t.Fatalf("loadPNG: %v", err)
	}
	rotated := rotateImage(loaded, 180)

	dstPath := filepath.Join(dir, "dst.png")
	if err := writePNG(dstPath, rotated, carry, false); err != nil {
		t.Fatalf("writePNG: %v", err)
	}

	out, err := os.Open(dstPath)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	decoded, err := png.Decode(out)
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	// A non-opaque 16-bit source must round-trip as a 16-bit type; the old
	// path would have produced an 8-bit *image.NRGBA.
	switch decoded.(type) {
	case *image.NRGBA64, *image.RGBA64:
	default:
		t.Fatalf("output is %T, want a 16-bit image (bit depth not preserved)", decoded)
	}
	// 180° CW: src(x,y) -> dst(w-1-x, h-1-y).
	for y := range h {
		for x := range w {
			got := color.NRGBA64Model.Convert(decoded.At(w-1-x, h-1-y)).(color.NRGBA64)
			want := src.NRGBA64At(x, y)
			if got != want {
				t.Errorf("dst(%d,%d) = %+v, want %+v", w-1-x, h-1-y, got, want)
			}
		}
	}
}

// TestCommitRefusesOverwrite confirms commit never clobbers an existing file.
func TestCommitRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.bin")
	if err := os.WriteFile(dst, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := commit(dst, func(tmp string) error {
		return os.WriteFile(tmp, []byte("replacement"), 0o644)
	})
	if err == nil {
		t.Fatal("commit overwrote an existing file")
	}
	if b, _ := os.ReadFile(dst); string(b) != "original" {
		t.Fatalf("destination was modified: %q", b)
	}
}
