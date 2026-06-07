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
			return runCommand(args[0], out, "_cut", workers, func(cfg image.Config) ([]outputPlan, error) {
				return cutPlan(cfg, bar)
			})
		},
	}
	cmd.Flags().IntVar(&bar, "bar", 0, "width of the middle black bar in pixels")
	addCommonFlags(cmd, &out, &workers, "_cut")
	return cmd
}

// cutPlan splits a cfg-sized image into its left and right halves, dropping a
// center band of width bar. When (width-bar) is odd the leftover center column
// is dropped. For JPEG the split snaps to the nearest MCU boundary so the crop
// stays lossless.
func cutPlan(cfg image.Config, bar int) ([]outputPlan, error) {
	w, h := cfg.Width, cfg.Height
	if bar >= w {
		return nil, fmt.Errorf("bar (%d) >= image width (%d)", bar, w)
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
