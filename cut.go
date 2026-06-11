package main

import (
	"errors"
	"fmt"
	"image"

	"github.com/spf13/cobra"
)

func cutCmd() *cobra.Command {
	var (
		bar     int
		out     string
		workers int
	)
	cmd := &cobra.Command{
		Use:   "cut [flags] <path>",
		Short: "Split half-frame scans into left/right halves",
		Long:  "Splits half-frame film scans laid out as [photo][blackbar][photo].",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if bar < 0 {
				return errors.New("--bar must be >= 0")
			}
			return runCommand(args[0], out, "_cut", workers, func(src srcInfo) ([]outputPlan, error) {
				return cutPlan(src, bar)
			})
		},
	}
	cmd.Flags().IntVar(&bar, "bar", 0, "width of the middle black bar in pixels")
	addCommonFlags(cmd, &out, &workers, "_cut")
	return cmd
}

// cutPlan splits the source image into its left and right halves, dropping a
// center band of width bar. When (width-bar) is odd the leftover center column
// is dropped. For JPEG, jpegtran keeps the crop lossless by snapping each
// crop's left edge down to the previous MCU boundary and widening the region
// accordingly, so the right half can come out up to one MCU wider than the
// left and include part of the bar.
func cutPlan(src srcInfo, bar int) ([]outputPlan, error) {
	w, h := src.cfg.Width, src.cfg.Height
	if bar >= w {
		return nil, fmt.Errorf("bar (%d) >= image width (%d)", bar, w)
	}
	// The split runs down the stored pixels; a non-Normal Orientation means
	// viewers show the image reoriented, so _L/_R may not be what's displayed
	// as left/right (for orientations 5-8 they are actually top/bottom).
	if src.orientation > 1 {
		logErrf("%s: warning: EXIF orientation %d is copied verbatim; the split follows the stored pixels, so _L/_R may not match the displayed left/right\n",
			src.path, src.orientation)
	}
	half := (w - bar) / 2
	if half <= 0 {
		return nil, fmt.Errorf("invalid split: half=%d", half)
	}
	return []outputPlan{
		{
			suffix:       "_L",
			jpegtranArgs: []string{"-crop", fmt.Sprintf("%dx%d+%d+%d", half, h, 0, 0)},
			transform:    crop(image.Rect(0, 0, half, h)),
		},
		{
			suffix:       "_R",
			jpegtranArgs: []string{"-crop", fmt.Sprintf("%dx%d+%d+%d", half, h, w-half, 0)},
			transform:    crop(image.Rect(w-half, 0, w, h)),
		},
	}, nil
}
