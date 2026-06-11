package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

var defaultWorkers = runtime.NumCPU()

// srcInfo describes a source image for planning: its path (for messages), its
// dimensions, and its EXIF Orientation tag (0 when absent).
type srcInfo struct {
	path        string
	cfg         image.Config
	orientation int
}

// runCommand collects the images under path, processes each with plan across
// the given number of workers, and writes results to outDir (defaulting to
// <input><dirSuffix> for directories). It is the shared body of every command.
func runCommand(path, out, dirSuffix string, workers int, plan func(srcInfo) ([]outputPlan, error)) error {
	if workers < 1 {
		return errors.New("--workers must be >= 1")
	}
	files, srcRoot, outDir, err := collect(path, out, dirSuffix)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("no images found")
	}
	// The output directory is created lazily per file by process (which mirrors
	// the source tree), so a run where every file fails leaves nothing behind.
	if failed := runJobs(files, workers, func(f string) error {
		return process(f, srcRoot, outDir, plan)
	}); failed > 0 {
		// Individual errors were already reported by the workers as they
		// happened; this summary sets the exit code and the final line.
		return fmt.Errorf("%d of %d files failed", failed, len(files))
	}
	return nil
}

func collect(path, out, dirSuffix string) (files []string, srcRoot, outDir string, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", "", err
	}
	if !info.IsDir() {
		if !isImage(path) {
			return nil, "", "", fmt.Errorf("not a supported image: %s", path)
		}
		srcRoot = filepath.Dir(path)
		outDir = out
		if outDir == "" {
			outDir = srcRoot
		}
		return []string{path}, srcRoot, outDir, nil
	}
	srcRoot = filepath.Clean(path)
	err = filepath.WalkDir(srcRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if isImage(p) {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, "", "", err
	}
	outDir = out
	if outDir == "" {
		outDir = srcRoot + dirSuffix
	}
	return files, srcRoot, outDir, nil
}

func isImage(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png":
		return true
	}
	return false
}

// outputExt chooses the output file extension for the detected format, keeping
// the input's own extension (and its casing) when it already matches and only
// substituting a canonical one when the input was misnamed.
func outputExt(path, format string) string {
	ext := filepath.Ext(path)
	switch format {
	case "jpeg":
		if l := strings.ToLower(ext); l == ".jpg" || l == ".jpeg" {
			return ext
		}
		return ".jpg"
	case "png":
		if strings.EqualFold(ext, ".png") {
			return ext
		}
		return ".png"
	}
	return ext
}

// outputPlan describes one output produced from a source image. JPEG sources
// are transformed losslessly by jpegtran using jpegtranArgs (no re-encode);
// PNG sources are transformed in-process with transform (also lossless). suffix
// is inserted before the file extension. resetOrientation rewrites the EXIF
// Orientation tag of the output to Normal(1); it is set by transforms (rotate)
// when the source carried a non-Normal tag that the physical reorientation has
// made stale. Sources without such a tag skip the rewrite, so they don't need
// exiv2 at all.
//
// resample marks an output whose transform resamples pixels (resize), so it is
// produced by decoding and re-encoding every format — including JPEG, which
// therefore bypasses the lossless jpegtran path. jpegQuality is the quality of
// such a JPEG re-encode.
type outputPlan struct {
	suffix           string
	jpegtranArgs     []string
	transform        func(image.Image) image.Image
	resetOrientation bool
	resample         bool
	jpegQuality      int
}

// process reads path, asks plan for the outputs to produce (given the source
// dimensions and EXIF orientation), and writes each into outDir mirroring
// path's location under srcRoot. JPEG and PNG are dispatched to their own
// lossless writers (resampling outputs are re-encoded instead); nothing ever
// overwrites an existing file.
func process(path, srcRoot, outDir string, plan func(srcInfo) ([]outputPlan, error)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	cfg, format, err := image.DecodeConfig(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	orientation, err := readOrientation(path, format)
	if err != nil {
		return err
	}

	outs, err := plan(srcInfo{path: path, cfg: cfg, orientation: orientation})
	if err != nil {
		return err
	}

	rel, err := filepath.Rel(srcRoot, path)
	if err != nil {
		return err
	}
	dstDir := filepath.Join(outDir, filepath.Dir(rel))
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}

	// Decide whether we need the source pixels. Lossless geometric transforms
	// only decode PNG (JPEG is handed to jpegtran by path); a resampling
	// transform (resize) decodes every format, JPEG included.
	resample := false
	for _, o := range outs {
		if o.resample {
			resample = true
		}
	}
	var decoded image.Image
	var carry []byte
	switch {
	case resample:
		decoded, err = loadDecoded(path)
	case format == "png":
		decoded, carry, err = loadPNG(path)
	}
	if err != nil {
		return err
	}

	// Name outputs from the detected format, not the (possibly misleading)
	// input extension, so a misnamed file still gets a correct one.
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	ext := outputExt(path, format)
	names := make([]string, len(outs))
	written := make([]string, 0, len(outs))
	for i, o := range outs {
		dst := filepath.Join(dstDir, base+o.suffix+ext)
		if sameFile(dst, path) {
			err = fmt.Errorf("refusing to overwrite input %s; pass --out to choose a different directory", path)
		} else {
			switch {
			case o.resample:
				err = writeResampled(dst, o.transform(decoded), format, o.jpegQuality)
			case format == "jpeg":
				err = writeJPEG(dst, path, o.jpegtranArgs, o.resetOrientation)
			case format == "png":
				err = writePNG(dst, o.transform(decoded), carry, o.resetOrientation)
			default:
				err = fmt.Errorf("unsupported format: %s", format)
			}
		}
		if err != nil {
			// Roll back outputs already written for this source so a
			// multi-output command (e.g. cut) is all-or-nothing. commit only
			// ever creates new files, so these are ours to remove.
			for _, w := range written {
				os.Remove(w)
			}
			return err
		}
		written = append(written, dst)
		names[i] = filepath.Base(dst)
	}
	logf("%s -> %s\n", path, strings.Join(names, ", "))
	return nil
}

// loadDecoded fully decodes the image at path using whichever registered format
// matches. Used by the resampling path, which re-encodes every format and so
// needs the pixels regardless of source type (including JPEG).
func loadDecoded(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return img, nil
}

// writeResampled re-encodes an already-resampled image into dst in the source's
// format, never overwriting an existing file. Unlike the geometric writers this
// is not lossless: JPEG is re-encoded at jpegQuality, and metadata is not
// carried (the pixels have changed, so a copied-over EXIF thumbnail or
// dimensions would be stale).
func writeResampled(dst string, img image.Image, format string, jpegQuality int) error {
	return commit(dst, func(tmp string) error {
		return writeTo(tmp, func(w io.Writer) error {
			switch format {
			case "jpeg":
				return encodeJPEG(w, img, jpegQuality)
			case "png":
				return pngEncoder.Encode(w, img)
			default:
				return fmt.Errorf("unsupported format: %s", format)
			}
		})
	})
}

// setOrientationNormal sets the EXIF Orientation tag of the image at path to
// Normal(1), creating an EXIF block if the file has none. Used after a physical
// rotation so the upright pixels aren't reoriented again by EXIF-aware viewers.
// Works for both JPEG and PNG (exiv2 edits the eXIf chunk for the latter).
func setOrientationNormal(path string) error {
	cmd := exec.Command("exiv2", "-M", "set Exif.Image.Orientation 1", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("exiv2 not found in PATH (required to set EXIF orientation); install exiv2")
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("exiv2: %s", msg)
		}
		return fmt.Errorf("exiv2: %w", err)
	}
	return nil
}

// sameFile reports whether a and b resolve to the same existing file,
// accounting for symlinks and equivalent path spellings.
func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// commit produces dst without ever overwriting an existing file. It reserves a
// sibling temp file, lets populate fill it in by path (so external tools like
// jpegtran/exiv2 can operate on it), and then hard-links it into place; the
// link fails atomically if dst already exists, even under concurrent writers.
// A failed populate leaves dst untouched.
func commit(dst string, populate func(tmpPath string) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".hft-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	tmp.Close()              // populate owns the file from here, by path
	defer os.Remove(tmpName) // no-op once the link below succeeds

	if err := populate(tmpName); err != nil {
		return err
	}
	// Set permissions after populate: tools like exiv2 rewrite the file
	// in place (new inode), which would otherwise discard an earlier chmod.
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	if err := os.Link(tmpName, dst); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("destination already exists: %s", dst)
		}
		// Hard links may be unsupported by the destination filesystem (some
		// FUSE, FAT/exFAT, or network mounts). Fall back to an exclusive
		// create + copy, which still refuses to overwrite an existing file.
		if cErr := copyExclusive(tmpName, dst); cErr != nil {
			return fmt.Errorf("hard link failed (%v); copy fallback: %w", err, cErr)
		}
	}
	return nil
}

// copyExclusive copies srcPath to dst, failing if dst already exists. The
// O_EXCL create is atomic, so the no-overwrite guarantee holds even under
// concurrent writers; a partial copy is removed so a failure leaves no output.
func copyExclusive(srcPath, dst string) error {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("destination already exists: %s", dst)
		}
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	// Match the hard-link path, which fixes the output at 0644 regardless of
	// umask; the O_EXCL create above is umask-masked.
	if err := os.Chmod(dst, 0o644); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

// writeTo truncates the file at path and streams write's output into it.
func writeTo(path string, write func(io.Writer) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := write(f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

var logMu sync.Mutex

// logf and logErrf serialize writes so progress lines from concurrent workers
// don't interleave.
func logf(format string, args ...any) {
	logMu.Lock()
	fmt.Fprintf(os.Stdout, format, args...)
	logMu.Unlock()
}

func logErrf(format string, args ...any) {
	logMu.Lock()
	fmt.Fprintf(os.Stderr, format, args...)
	logMu.Unlock()
}

func runJobs(files []string, workers int, work func(string) error) int {
	n := max(min(workers, len(files)), 1)
	jobs := make(chan string)
	var failed atomic.Uint64
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				if err := work(f); err != nil {
					logErrf("%s: %v\n", f, err)
					failed.Add(1)
				}
			}
		}()
	}
	for _, f := range files {
		jobs <- f
	}
	close(jobs)
	wg.Wait()
	return int(failed.Load())
}
