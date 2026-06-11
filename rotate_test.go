package main

import (
	"strconv"
	"testing"
)

// TestRotatePlanComposesOrientation checks that a non-Normal EXIF Orientation
// (a display-time rotation) is folded into the physical rotation, so the
// displayed result is rotated by exactly the requested angle.
func TestRotatePlanComposesOrientation(t *testing.T) {
	cases := []struct {
		orientation, angle int
		wantStored         int
		wantReset          bool
	}{
		{0, 90, 90, false},
		{1, 180, 180, false},
		{3, 270, 90, true}, // 3 = displayed rotated 180
		{6, 90, 180, true}, // 6 = displayed rotated 90 CW
		{6, 270, 0, true},  // composition cancels out; only the tag is stale
		{8, 90, 0, true},   // 8 = displayed rotated 270 CW
		{8, 180, 90, true},
	}
	for _, c := range cases {
		outs, err := rotatePlan(srcInfo{orientation: c.orientation}, c.angle)
		if err != nil {
			t.Fatalf("orientation %d angle %d: %v", c.orientation, c.angle, err)
		}
		o := outs[0]
		if want := "_rotated_" + strconv.Itoa(c.angle); o.suffix != want {
			t.Errorf("orientation %d angle %d: suffix = %q, want %q", c.orientation, c.angle, o.suffix, want)
		}
		if o.resetOrientation != c.wantReset {
			t.Errorf("orientation %d angle %d: resetOrientation = %v, want %v", c.orientation, c.angle, o.resetOrientation, c.wantReset)
		}
		if c.wantStored == 0 {
			if len(o.jpegtranArgs) != 0 {
				t.Errorf("orientation %d angle %d: jpegtran args = %v, want none", c.orientation, c.angle, o.jpegtranArgs)
			}
		} else if want := []string{"-perfect", "-rotate", strconv.Itoa(c.wantStored)}; !equalArgs(o.jpegtranArgs, want) {
			t.Errorf("orientation %d angle %d: jpegtran args = %v, want %v", c.orientation, c.angle, o.jpegtranArgs, want)
		}
		// The in-process transform must rotate by the same composed angle:
		// a 3x2 source comes out 2x3 for 90/270 and stays 3x2 for 0/180.
		wantW, wantH := 3, 2
		if c.wantStored == 90 || c.wantStored == 270 {
			wantW, wantH = 2, 3
		}
		if b := o.transform(gradient(3, 2)).Bounds(); b.Dx() != wantW || b.Dy() != wantH {
			t.Errorf("orientation %d angle %d: transformed bounds = %v, want %dx%d", c.orientation, c.angle, b, wantW, wantH)
		}
	}
}

func TestRotatePlanRejectsMirroredOrientation(t *testing.T) {
	for _, o := range []int{2, 4, 5, 7} {
		if _, err := rotatePlan(srcInfo{orientation: o}, 90); err == nil {
			t.Errorf("orientation %d: expected an error (mirror flips cannot be composed)", o)
		}
	}
}
