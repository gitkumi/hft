package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"os"
	"path/filepath"
)

func cmdCut(args []string) {
	fs := flag.NewFlagSet("cut", flag.ExitOnError)
	bar := fs.Int("bar", 0, "width of the middle black bar in pixels")
	out := fs.String("out", "", "output directory (default: <input>_cut for dirs, alongside input for single files)")
	workers := fs.Int("j", defaultWorkers, "number of parallel workers")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s cut [flags] <path>\n", filepath.Base(os.Args[0]))
		fmt.Fprintln(os.Stderr, "Splits half-frame film scans laid out as [photo][blackbar][photo].")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if *bar < 0 {
		fatal(errors.New("-bar must be >= 0"))
	}
	if *workers < 1 {
		fatal(errors.New("-j must be >= 1"))
	}

	files, srcRoot, outDir, err := collect(fs.Arg(0), *out, "_cut")
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
		return process(f, srcRoot, outDir, func(cfg image.Config) ([]outputPlan, error) {
			return cutPlan(cfg, *bar)
		})
	})
	if failed > 0 {
		os.Exit(1)
	}
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

// crop returns a transform that extracts rect (in 0-origin coordinates) from an
// image into a fresh RGBA, regardless of the source's concrete type.
func crop(rect image.Rectangle) func(image.Image) image.Image {
	return func(img image.Image) image.Image {
		src := rect.Add(img.Bounds().Min)
		dst := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
		draw.Draw(dst, dst.Bounds(), img, src.Min, draw.Src)
		return dst
	}
}
