package main

// Integration tests for the external-tool paths (jpegtran, exiv2). Each test
// skips when the tool isn't on PATH, so the suite still passes on bare
// machines while exercising the real pipelines where it can.

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not on PATH", name)
	}
}

func writeJPEGFile(t *testing.T, path string, img image.Image) {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func decodeJPEGFile(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatalf("%s: decode: %v", path, err)
	}
	return img
}

// TestWriteJPEGRotate runs a block-aligned rotation, which -perfect accepts
// outright.
func TestWriteJPEGRotate(t *testing.T) {
	requireTool(t, "jpegtran")
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jpg")
	writeJPEGFile(t, src, gradient(32, 16)) // multiples of the 16px iMCU
	outs, err := rotatePlan(srcInfo{}, 90)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst.jpg")
	if err := writeJPEG(dst, src, outs[0].jpegtranArgs, false); err != nil {
		t.Fatalf("writeJPEG: %v", err)
	}
	if b := decodeJPEGFile(t, dst).Bounds(); b.Dx() != 16 || b.Dy() != 32 {
		t.Errorf("rotated bounds = %v, want 16x32", b)
	}
}

// TestWriteJPEGTrimFallback rotates an image whose dimensions are not
// block-aligned: -perfect must fail and the -trim retry must produce a valid,
// no-larger output.
func TestWriteJPEGTrimFallback(t *testing.T) {
	requireTool(t, "jpegtran")
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jpg")
	writeJPEGFile(t, src, gradient(20, 20)) // not a multiple of the 16px iMCU
	outs, err := rotatePlan(srcInfo{}, 90)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst.jpg")
	if err := writeJPEG(dst, src, outs[0].jpegtranArgs, false); err != nil {
		t.Fatalf("writeJPEG (trim fallback): %v", err)
	}
	b := decodeJPEGFile(t, dst).Bounds()
	if b.Dx() < 1 || b.Dx() > 20 || b.Dy() < 1 || b.Dy() > 20 {
		t.Errorf("trimmed bounds = %v, want within 20x20", b)
	}
}

// TestSetOrientationNormal round-trips an Orientation tag through exiv2 and
// our own EXIF reader.
func TestSetOrientationNormal(t *testing.T) {
	requireTool(t, "exiv2")
	dir := t.TempDir()
	p := filepath.Join(dir, "img.jpg")
	writeJPEGFile(t, p, gradient(16, 16))
	if out, err := exec.Command("exiv2", "-M", "set Exif.Image.Orientation 6", p).CombinedOutput(); err != nil {
		t.Fatalf("exiv2 set: %v (%s)", err, out)
	}
	if got, err := readOrientation(p, "jpeg"); err != nil || got != 6 {
		t.Fatalf("after set: orientation = %d, %v; want 6", got, err)
	}
	if err := setOrientationNormal(p); err != nil {
		t.Fatalf("setOrientationNormal: %v", err)
	}
	if got, err := readOrientation(p, "jpeg"); err != nil || got != 1 {
		t.Errorf("after reset: orientation = %d, %v; want 1", got, err)
	}
}
