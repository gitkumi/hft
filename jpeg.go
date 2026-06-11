package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg" // registers the JPEG decoder and provides the re-encoder
	"io"
	"os/exec"
	"slices"
	"strings"
)

// encodeJPEG re-encodes img as a baseline JPEG at the given quality (1–100).
// Used only by the resampling path; geometric transforms use jpegtran instead
// to stay lossless.
func encodeJPEG(w io.Writer, img image.Image, quality int) error {
	return jpeg.Encode(w, img, &jpeg.Options{Quality: quality})
}

// writeJPEG produces dst by running a lossless jpegtran transform on src,
// optionally resetting the EXIF Orientation afterwards. Rotations pass
// -perfect so jpegtran refuses a transform that would leave untransformable
// edge blocks visibly out of place (dimensions not a multiple of the iMCU
// size); when that happens the transform is retried with -trim, which
// discards those edge blocks (up to 15 px) — visually clean but no longer
// strictly lossless, so the user is warned. The output never overwrites an
// existing file.
func writeJPEG(dst, src string, jpegtranArgs []string, resetOrientation bool) error {
	return commit(dst, func(tmp string) error {
		run := func(args []string) error {
			return writeTo(tmp, func(w io.Writer) error {
				return jpegtranTo(w, src, args)
			})
		}
		err := run(jpegtranArgs)
		if err != nil && slices.Contains(jpegtranArgs, "-perfect") && strings.Contains(err.Error(), "not perfect") {
			logErrf("%s: warning: dimensions are not a multiple of the JPEG block size; trimming up to 15 edge pixels\n", src)
			trimmed := slices.Clone(jpegtranArgs)
			trimmed[slices.Index(trimmed, "-perfect")] = "-trim"
			err = run(trimmed)
		}
		if err != nil {
			return err
		}
		if resetOrientation {
			return setOrientationNormal(tmp)
		}
		return nil
	})
}

// jpegtranTo runs a lossless jpegtran transform on src and writes the result to
// w. All metadata is copied (-copy all) so EXIF such as camera and date are
// preserved; transforms that physically reorient pixels reset the now-stale
// Orientation tag afterwards (see setOrientationNormal).
func jpegtranTo(w io.Writer, src string, args []string) error {
	full := make([]string, 0, len(args)+3)
	full = append(full, "-copy", "all")
	full = append(full, args...)
	full = append(full, src)

	cmd := exec.Command("jpegtran", full...)
	cmd.Stdout = w
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("jpegtran not found in PATH (required for lossless JPEG transforms); install libjpeg-turbo")
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("jpegtran: %s", msg)
		}
		return fmt.Errorf("jpegtran: %w", err)
	}
	return nil
}
