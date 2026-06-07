package main

import (
	"image"
	"image/color"
	"reflect"
	"testing"

	xdraw "golang.org/x/image/draw"
)

func TestTargetSize(t *testing.T) {
	cases := []struct {
		name                  string
		ow, oh, width, height int
		scale                 float64
		wantW, wantH          int
	}{
		{"scale half", 200, 100, 0, 0, 0.5, 100, 50},
		{"width keeps aspect", 200, 100, 100, 0, 0, 100, 50},
		{"height keeps aspect", 200, 100, 0, 50, 0, 100, 50},
		{"both exact", 200, 100, 64, 64, 0, 64, 64},
		{"no change", 200, 100, 0, 0, 0, 200, 100},
		{"scale clamps to 1", 10, 10, 0, 0, 0.001, 1, 1},
		{"rounds to nearest", 99, 100, 0, 0, 0.5, 50, 50},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotW, gotH := targetSize(c.ow, c.oh, c.width, c.height, c.scale)
			if gotW != c.wantW || gotH != c.wantH {
				t.Errorf("targetSize = %dx%d, want %dx%d", gotW, gotH, c.wantW, c.wantH)
			}
		})
	}
}

// TestResampleDepthPreserved checks that resizing keeps a 16-bit source 16-bit
// and grayscale grayscale, and promotes paletted to truecolor (so resampling
// isn't pinned to a fixed palette).
func TestResampleDepthPreserved(t *testing.T) {
	cases := []struct {
		name string
		src  image.Image
		want any
	}{
		{"NRGBA64", image.NewNRGBA64(image.Rect(0, 0, 8, 8)), &image.NRGBA64{}},
		{"RGBA64", image.NewRGBA64(image.Rect(0, 0, 8, 8)), &image.NRGBA64{}},
		{"Gray16", image.NewGray16(image.Rect(0, 0, 8, 8)), &image.Gray16{}},
		{"Gray", image.NewGray(image.Rect(0, 0, 8, 8)), &image.Gray{}},
		{"RGBA", image.NewRGBA(image.Rect(0, 0, 8, 8)), &image.NRGBA{}},
		{"Paletted", image.NewPaletted(image.Rect(0, 0, 8, 8), color.Palette{color.Black, color.White}), &image.NRGBA{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resample(4, 4, xdraw.CatmullRom)(c.src)
			if reflect.TypeOf(got) != reflect.TypeOf(c.want) {
				t.Errorf("resample produced %T, want %T", got, c.want)
			}
			if b := got.Bounds(); b.Dx() != 4 || b.Dy() != 4 {
				t.Errorf("bounds = %v, want 4x4", b)
			}
		})
	}
}
