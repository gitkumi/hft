package main

import (
	"image"
	"image/color"
	"testing"
)

// gradient builds a w×h RGBA image whose pixels encode their own coordinates,
// so a rotation can be checked by mapping coordinates rather than colors.
func gradient(w, h int) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			m.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0, A: 255})
		}
	}
	return m
}

func TestRotateImage(t *testing.T) {
	const w, h = 3, 2
	src := gradient(w, h)

	cases := []struct {
		angle    int
		wantW    int
		wantH    int
		mapCoord func(x, y int) (int, int) // src (x,y) -> expected dst (x,y)
	}{
		{90, h, w, func(x, y int) (int, int) { return h - 1 - y, x }},
		{180, w, h, func(x, y int) (int, int) { return w - 1 - x, h - 1 - y }},
		{270, h, w, func(x, y int) (int, int) { return y, w - 1 - x }},
	}

	for _, c := range cases {
		dst := rotateImage(src, c.angle)
		bd := dst.Bounds()
		if bd.Dx() != c.wantW || bd.Dy() != c.wantH {
			t.Errorf("angle %d: got %dx%d, want %dx%d", c.angle, bd.Dx(), bd.Dy(), c.wantW, c.wantH)
		}
		for y := range h {
			for x := range w {
				dx, dy := c.mapCoord(x, y)
				if got, want := dst.At(dx, dy), src.At(x, y); !sameColor(got, want) {
					t.Errorf("angle %d: src(%d,%d) should map to dst(%d,%d): got %v want %v",
						c.angle, x, y, dx, dy, got, want)
				}
			}
		}
	}
}

func TestRotateImageFullTurn(t *testing.T) {
	src := gradient(4, 3)
	got := rotateImage(rotateImage(rotateImage(rotateImage(src, 90), 90), 90), 90)
	b := src.Bounds()
	if got.Bounds() != b {
		t.Fatalf("four 90° turns changed bounds: %v -> %v", b, got.Bounds())
	}
	for y := range b.Dy() {
		for x := range b.Dx() {
			if !sameColor(got.At(x, y), src.At(x, y)) {
				t.Fatalf("four 90° turns not identity at (%d,%d)", x, y)
			}
		}
	}
}

func sameColor(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func TestCutPlan(t *testing.T) {
	// width 7, bar 1 -> half = 3; left = [0,3), right = [4,7).
	outs, err := cutPlan(image.Config{Width: 7, Height: 2}, 1)
	if err != nil {
		t.Fatalf("cutPlan: %v", err)
	}
	if len(outs) != 2 {
		t.Fatalf("got %d outputs, want 2", len(outs))
	}
	if outs[0].suffix != "_L" || outs[1].suffix != "_R" {
		t.Fatalf("suffixes = %q, %q", outs[0].suffix, outs[1].suffix)
	}

	// jpegtran crop geometry: left at x=0, right at x=4 (after the 1px bar).
	wantL := []string{"-crop", "3x2+0+0"}
	wantR := []string{"-crop", "3x2+4+0"}
	if !equalArgs(outs[0].jpegtranArgs, wantL) {
		t.Errorf("left jpegtran args = %v, want %v", outs[0].jpegtranArgs, wantL)
	}
	if !equalArgs(outs[1].jpegtranArgs, wantR) {
		t.Errorf("right jpegtran args = %v, want %v", outs[1].jpegtranArgs, wantR)
	}

	// In-process (PNG) crop: same geometry, exact pixels.
	src := gradient(7, 2)
	left := outs[0].transform(src)
	right := outs[1].transform(src)
	if got := left.Bounds().Dx(); got != 3 {
		t.Errorf("left width = %d, want 3", got)
	}
	if got := right.Bounds().Dx(); got != 3 {
		t.Errorf("right width = %d, want 3", got)
	}
	if !sameColor(left.At(0, 0), src.At(0, 0)) {
		t.Error("left(0,0) should equal src(0,0)")
	}
	if !sameColor(right.At(0, 0), src.At(4, 0)) {
		t.Error("right(0,0) should equal src(4,0)")
	}
}

func TestCutPlanBarTooWide(t *testing.T) {
	if _, err := cutPlan(image.Config{Width: 4, Height: 2}, 4); err == nil {
		t.Error("expected error when bar >= width")
	}
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
