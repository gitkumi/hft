package main

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"math"

	"github.com/spf13/cobra"
	xdraw "golang.org/x/image/draw"
)

// scalers maps --filter names to x/image/draw interpolators, cheapest to best.
var scalers = map[string]xdraw.Interpolator{
	"nearest":    xdraw.NearestNeighbor,
	"bilinear":   xdraw.BiLinear,
	"catmullrom": xdraw.CatmullRom,
}

const scalerNames = "nearest, bilinear, catmullrom"

func resizeCmd() *cobra.Command {
	var (
		width   int
		height  int
		scale   float64
		filter  string
		quality int
		out     string
		workers int
	)
	cmd := &cobra.Command{
		Use:   "resize [flags] <path>",
		Short: "Resize images with high-quality resampling",
		Long: "Resizes images. Unlike cut and rotate, resize resamples pixels and is " +
			"therefore lossy: every format (JPEG included) is decoded and re-encoded, " +
			"and metadata is not carried over.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, ok := scalers[filter]
			if !ok {
				return fmt.Errorf("unknown --filter %q (choose from: %s)", filter, scalerNames)
			}
			if quality < 1 || quality > 100 {
				return errors.New("--quality must be between 1 and 100")
			}
			if width < 0 || height < 0 {
				return errors.New("--width and --height must be >= 0")
			}
			if scale < 0 {
				return errors.New("--scale must be >= 0")
			}
			if scale == 0 && width == 0 && height == 0 {
				return errors.New("specify --scale, --width, and/or --height")
			}
			if scale != 0 && (width != 0 || height != 0) {
				return errors.New("--scale cannot be combined with --width/--height")
			}
			return runCommand(args[0], out, "_resized", workers, func(cfg image.Config) ([]outputPlan, error) {
				return resizePlan(cfg, width, height, scale, sc, quality)
			})
		},
	}
	cmd.Flags().IntVarP(&width, "width", "w", 0, "target width in pixels (0 = derive from aspect ratio)")
	cmd.Flags().IntVarP(&height, "height", "H", 0, "target height in pixels (0 = derive from aspect ratio)")
	cmd.Flags().Float64Var(&scale, "scale", 0, "uniform scale factor, e.g. 0.5 (overrides --width/--height)")
	cmd.Flags().StringVar(&filter, "filter", "catmullrom", "resampling filter: "+scalerNames)
	cmd.Flags().IntVarP(&quality, "quality", "q", 90, "JPEG output quality (1-100)")
	addCommonFlags(cmd, &out, &workers, "_resized")
	return cmd
}

// resizePlan computes the target size for a cfg-sized image and returns a single
// resampling output named "_<w>x<h>".
func resizePlan(cfg image.Config, width, height int, scale float64, sc xdraw.Interpolator, quality int) ([]outputPlan, error) {
	tw, th := targetSize(cfg.Width, cfg.Height, width, height, scale)
	if tw < 1 || th < 1 {
		return nil, fmt.Errorf("invalid target size %dx%d", tw, th)
	}
	return []outputPlan{{
		suffix:      fmt.Sprintf("_%dx%d", tw, th),
		transform:   resample(tw, th, sc),
		resample:    true,
		jpegQuality: quality,
	}}, nil
}

// targetSize resolves the requested dimensions against an ow×oh source. A
// uniform scale wins; otherwise a missing (zero) width or height is derived from
// the other to preserve the aspect ratio. Results are clamped to at least 1.
func targetSize(ow, oh, width, height int, scale float64) (int, int) {
	switch {
	case scale > 0:
		return clamp1(round(float64(ow) * scale)), clamp1(round(float64(oh) * scale))
	case width > 0 && height > 0:
		return width, height
	case width > 0:
		return width, clamp1(round(float64(oh) * float64(width) / float64(ow)))
	case height > 0:
		return clamp1(round(float64(ow) * float64(height) / float64(oh))), height
	}
	return ow, oh
}

// resample returns a transform that scales any image to w×h with sc, preserving
// the source's bit-depth class (8- vs 16-bit) and grayscale-ness so the output
// encoder isn't forced to inflate or flatten the result.
func resample(w, h int, sc xdraw.Interpolator) func(image.Image) image.Image {
	return func(src image.Image) image.Image {
		dst := resampleDst(src, w, h)
		sc.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
		return dst
	}
}

// resampleDst allocates a destination matched to the source's depth: 16-bit
// sources stay 16-bit, grayscale stays grayscale, everything else becomes 8-bit
// NRGBA (paletted sources are promoted so resampling isn't pinned to a palette).
func resampleDst(src image.Image, w, h int) draw.Image {
	r := image.Rect(0, 0, w, h)
	switch src.(type) {
	case *image.Gray16:
		return image.NewGray16(r)
	case *image.Gray:
		return image.NewGray(r)
	case *image.RGBA64, *image.NRGBA64:
		return image.NewNRGBA64(r)
	default:
		return image.NewNRGBA(r)
	}
}

func round(f float64) int { return int(math.Round(f)) }

func clamp1(n int) int { return max(n, 1) }
