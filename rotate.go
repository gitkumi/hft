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
			return runCommand(args[0], out, "_rot", workers, func(src srcInfo) ([]outputPlan, error) {
				return rotatePlan(src, angle)
			})
		},
	}
	cmd.Flags().IntVarP(&angle, "angle", "a", 90, "rotation angle in degrees clockwise (90, 180, or 270)")
	addCommonFlags(cmd, &out, &workers, "_rot")
	return cmd
}

// rotatePlan rotates the image as displayed by angle degrees clockwise. The
// physical rotation applies to the stored pixels, so a non-Normal EXIF
// Orientation (which viewers apply at display time) is composed into the
// stored angle, and the tag is reset to Normal(1) afterwards — without the
// composition, rotating a file that is displayed via Orientation would come
// out visibly unchanged. Mirrored orientations cannot be expressed as a pure
// rotation and are an error. JPEG is rotated losslessly by jpegtran in the
// DCT domain (-perfect, with a trimming fallback for non-block-aligned
// dimensions; see writeJPEG); PNG is rotated in-process.
func rotatePlan(src srcInfo, angle int) ([]outputPlan, error) {
	stored := angle
	switch src.orientation {
	case 0, 1: // no tag, or already upright
	case 3:
		stored = (stored + 180) % 360
	case 6:
		stored = (stored + 90) % 360
	case 8:
		stored = (stored + 270) % 360
	default:
		return nil, fmt.Errorf("EXIF orientation %d includes a mirror flip, which rotate cannot compose; normalize the file first", src.orientation)
	}
	// A composed angle of 0 means the stored pixels are already where the user
	// wants them; only the stale Orientation tag needs resetting.
	var args []string
	if stored != 0 {
		args = []string{"-perfect", "-rotate", strconv.Itoa(stored)}
	}
	return []outputPlan{{
		suffix:           fmt.Sprintf("_rotated_%d", angle),
		jpegtranArgs:     args,
		transform:        func(img image.Image) image.Image { return rotateImage(img, stored) },
		resetOrientation: src.orientation > 1,
	}}, nil
}
