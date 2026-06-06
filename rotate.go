package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"strconv"
)

func cmdRotate(args []string) {
	fs := flag.NewFlagSet("rotate", flag.ExitOnError)
	angle := fs.Int("angle", 90, "rotation angle in degrees clockwise (90, 180, or 270)")
	out := fs.String("out", "", "output directory (default: <input>_rot for dirs, alongside input for single files)")
	workers := fs.Int("j", defaultWorkers, "number of parallel workers")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s rotate [flags] <path>\n", filepath.Base(os.Args[0]))
		fmt.Fprintln(os.Stderr, "Rotates images by 90, 180, or 270 degrees clockwise.")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if *angle != 90 && *angle != 180 && *angle != 270 {
		fatal(errors.New("-angle must be 90, 180, or 270"))
	}
	if *workers < 1 {
		fatal(errors.New("-j must be >= 1"))
	}

	files, srcRoot, outDir, err := collect(fs.Arg(0), *out, "_rot")
	if err != nil {
		fatal(err)
	}
	if len(files) == 0 {
		fatal(errors.New("no images found"))
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatal(err)
	}

	failed := runJobs(files, *workers, func(f string) error {
		return process(f, srcRoot, outDir, func(image.Config) ([]outputPlan, error) {
			return rotatePlan(*angle), nil
		})
	})
	if failed > 0 {
		os.Exit(1)
	}
}

// rotatePlan rotates the image clockwise by angle. JPEG is rotated losslessly
// by jpegtran in the DCT domain; PNG is rotated in-process.
func rotatePlan(angle int) []outputPlan {
	return []outputPlan{{
		suffix:       fmt.Sprintf("_rotated_%d", angle),
		jpegtranArgs: []string{"-rotate", strconv.Itoa(angle)},
		transform:    func(img image.Image) image.Image { return rotateImage(img, angle) },
	}}
}

// rotateImage rotates src clockwise by angle (90, 180, or 270 degrees),
// returning src unchanged for any other angle. The source is first normalized
// into a packed RGBA buffer so the rotation loop copies pixels via direct slice
// indexing rather than per-pixel interface dispatch.
func rotateImage(src image.Image, angle int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	s := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(s, s.Bounds(), src, b.Min, draw.Src)

	switch angle {
	case 90:
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := range h {
			srow := s.Pix[y*s.Stride:]
			for x := range w {
				di := x*dst.Stride + (h-1-y)*4
				copy(dst.Pix[di:di+4], srow[x*4:x*4+4])
			}
		}
		return dst
	case 180:
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			srow := s.Pix[y*s.Stride:]
			for x := range w {
				di := (h-1-y)*dst.Stride + (w-1-x)*4
				copy(dst.Pix[di:di+4], srow[x*4:x*4+4])
			}
		}
		return dst
	case 270:
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := range h {
			srow := s.Pix[y*s.Stride:]
			for x := range w {
				di := (w-1-x)*dst.Stride + y*4
				copy(dst.Pix[di:di+4], srow[x*4:x*4+4])
			}
		}
		return dst
	}
	return src
}
