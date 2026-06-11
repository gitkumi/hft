package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGradientPNG encodes a w×h gradient PNG at path.
func writeGradientPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, gradient(w, h)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cutPlanFunc(bar int) func(srcInfo) ([]outputPlan, error) {
	return func(s srcInfo) ([]outputPlan, error) { return cutPlan(s, bar) }
}

func TestProcessEndToEnd(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "scan.png")
	writeGradientPNG(t, src, 8, 4)
	outDir := filepath.Join(dir, "out")
	if err := process(src, dir, outDir, cutPlanFunc(0)); err != nil {
		t.Fatalf("process: %v", err)
	}
	for _, name := range []string{"scan_L.png", "scan_R.png"} {
		f, err := os.Open(filepath.Join(outDir, name))
		if err != nil {
			t.Fatalf("missing output %s: %v", name, err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		if b := img.Bounds(); b.Dx() != 4 || b.Dy() != 4 {
			t.Errorf("%s: bounds = %v, want 4x4", name, b)
		}
	}
}

// TestProcessRollsBackOnPartialFailure pre-creates the second output so the _R
// write fails after _L succeeded, and checks the multi-output write is
// all-or-nothing.
func TestProcessRollsBackOnPartialFailure(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "scan.png")
	writeGradientPNG(t, src, 8, 4)
	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(outDir, "scan_R.png")
	if err := os.WriteFile(blocker, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := process(src, dir, outDir, cutPlanFunc(0)); err == nil {
		t.Fatal("process succeeded despite an existing destination")
	}
	if _, err := os.Stat(filepath.Join(outDir, "scan_L.png")); !os.IsNotExist(err) {
		t.Error("scan_L.png was not rolled back")
	}
	if b, _ := os.ReadFile(blocker); string(b) != "existing" {
		t.Error("pre-existing scan_R.png was modified")
	}
}

// TestProcessRefusesToOverwriteInput uses a plan whose only output resolves to
// the input path itself.
func TestProcessRefusesToOverwriteInput(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "scan.png")
	writeGradientPNG(t, src, 4, 4)
	plan := func(srcInfo) ([]outputPlan, error) {
		return []outputPlan{{suffix: "", transform: func(img image.Image) image.Image { return img }}}, nil
	}
	err := process(src, dir, dir, plan)
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("err = %v, want a refusal to overwrite the input", err)
	}
	if fi, statErr := os.Stat(src); statErr != nil || fi.Size() == 0 {
		t.Errorf("input damaged: %v, %v", fi, statErr)
	}
}

func TestOutputExt(t *testing.T) {
	cases := []struct{ path, format, want string }{
		{"a.jpg", "jpeg", ".jpg"},
		{"a.JPG", "jpeg", ".JPG"},
		{"a.jpeg", "jpeg", ".jpeg"},
		{"a.png", "jpeg", ".jpg"}, // misnamed: corrected to the detected format
		{"a.png", "png", ".png"},
		{"a.PNG", "png", ".PNG"},
		{"a.jpg", "png", ".png"}, // misnamed: corrected to the detected format
		{"a.bmp", "gif", ".bmp"}, // unknown format: extension passed through
	}
	for _, c := range cases {
		if got := outputExt(c.path, c.format); got != c.want {
			t.Errorf("outputExt(%q, %q) = %q, want %q", c.path, c.format, got, c.want)
		}
	}
}

func TestCopyExclusive(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst")
	if err := copyExclusive(src, dst); err != nil {
		t.Fatalf("copyExclusive: %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "payload" {
		t.Errorf("dst content = %q, want %q", b, "payload")
	}
	if fi, err := os.Stat(dst); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("dst mode = %v (%v), want 0644", fi.Mode().Perm(), err)
	}
	if err := copyExclusive(src, dst); err == nil {
		t.Fatal("copyExclusive overwrote an existing file")
	}
	if b, _ := os.ReadFile(dst); string(b) != "payload" {
		t.Error("existing dst was modified by the refused copy")
	}
}
