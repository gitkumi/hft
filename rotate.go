package main

import (
	"errors"
	"fmt"
	"image"
	"strconv"

	"github.com/spf13/cobra"
)

func rotateCmd() *cobra.Command {
	var (
		angle   int
		out     string
		workers int
	)
	cmd := &cobra.Command{
		Use:   "rotate [flags] <path>",
		Short: "Rotate images by 90, 180, or 270 degrees clockwise",
		Long:  "Rotates images by 90, 180, or 270 degrees clockwise.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if angle != 90 && angle != 180 && angle != 270 {
				return errors.New("--angle must be 90, 180, or 270")
			}
			return runCommand(args[0], out, "_rot", workers, func(image.Config) ([]outputPlan, error) {
				return rotatePlan(angle), nil
			})
		},
	}
	cmd.Flags().IntVarP(&angle, "angle", "a", 90, "rotation angle in degrees clockwise (90, 180, or 270)")
	addCommonFlags(cmd, &out, &workers, "_rot")
	return cmd
}

// rotatePlan rotates the image clockwise by angle. JPEG is rotated losslessly
// by jpegtran in the DCT domain; PNG is rotated in-process.
func rotatePlan(angle int) []outputPlan {
	return []outputPlan{{
		suffix:           fmt.Sprintf("_rotated_%d", angle),
		jpegtranArgs:     []string{"-rotate", strconv.Itoa(angle)},
		transform:        func(img image.Image) image.Image { return rotateImage(img, angle) },
		resetOrientation: true,
	}}
}
