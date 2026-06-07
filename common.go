package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register the JPEG decoder for image.DecodeConfig
	"image/png"
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

// outputPlan describes one output produced from a source image. JPEG sources
// are transformed losslessly by jpegtran using jpegtranArgs (no re-encode);
// PNG sources are transformed in-process with transform (also lossless). suffix
// is inserted before the file extension. resetOrientation rewrites the EXIF
// Orientation tag of a JPEG output to Normal(1), for transforms (rotate) that
// physically reorient the pixels so a copied-over tag would otherwise be stale.
type outputPlan struct {
	suffix           string
	jpegtranArgs     []string
	transform        func(image.Image) image.Image
	resetOrientation bool
}

// process reads path, asks plan for the outputs to produce (given the source
// dimensions), and writes each into outDir mirroring path's location under
// srcRoot. Nothing is ever decoded for JPEG sources, so their quality is
// preserved exactly; PNG sources are decoded once and re-encoded losslessly.
func process(path, srcRoot, outDir string, plan func(image.Config) ([]outputPlan, error)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	cfg, format, err := image.DecodeConfig(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	outs, err := plan(cfg)
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

	// Only PNG needs its pixels in memory; JPEG is handed to jpegtran as-is.
	// We also keep the raw PNG bytes so the source's EXIF (eXIf chunk) can be
	// carried over — png.Encode does not preserve it.
	var decoded image.Image
	var srcEXIF []byte
	if format == "png" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		decoded, _, err = image.Decode(bytes.NewReader(raw))
		if err != nil {
			return fmt.Errorf("decode: %w", err)
		}
		srcEXIF = pngChunk(raw, "eXIf")
	}

	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	names := make([]string, len(outs))
	for i, o := range outs {
		dst := filepath.Join(dstDir, base+o.suffix+ext)
		if sameFile(dst, path) {
			return fmt.Errorf("refusing to overwrite input %s; pass -out to choose a different directory", path)
		}
		switch format {
		case "jpeg":
			if err := commit(dst, func(tmp string) error {
				if err := writeTo(tmp, func(w io.Writer) error {
					return jpegtranTo(w, path, o.jpegtranArgs)
				}); err != nil {
					return err
				}
				if o.resetOrientation {
					return setOrientationNormal(tmp)
				}
				return nil
			}); err != nil {
				return err
			}
		case "png":
			if err := commit(dst, func(tmp string) error {
				var buf bytes.Buffer
				if err := png.Encode(&buf, o.transform(decoded)); err != nil {
					return err
				}
				out := buf.Bytes()
				if srcEXIF != nil {
					out = insertAfterIHDR(out, srcEXIF)
				}
				if err := os.WriteFile(tmp, out, 0o644); err != nil {
					return err
				}
				// A physical rotation makes a carried-over Orientation stale;
				// reset it (also adds the tag when the source had no EXIF).
				if o.resetOrientation {
					return setOrientationNormal(tmp)
				}
				return nil
			}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported format: %s", format)
		}
		names[i] = filepath.Base(dst)
	}
	logf("%s -> %s\n", path, strings.Join(names, ", "))
	return nil
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

// setOrientationNormal sets the EXIF Orientation tag of the JPEG at path to
// Normal(1), creating an EXIF block if the file has none. Used after a physical
// rotation so the upright pixels aren't reoriented again by EXIF-aware viewers.
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

// pngChunk returns the verbatim bytes (length + type + data + CRC) of the first
// chunk of the given type in a PNG file, or nil if absent or the data is
// malformed. Returning the CRC unchanged keeps the chunk valid when re-spliced.
func pngChunk(data []byte, typ string) []byte {
	const sig = 8
	if len(data) < sig {
		return nil
	}
	for i := sig; i+12 <= len(data); {
		ln := int(binary.BigEndian.Uint32(data[i:]))
		end := i + 12 + ln
		if ln < 0 || end > len(data) {
			return nil
		}
		if string(data[i+4:i+8]) == typ {
			return data[i:end]
		}
		i = end
	}
	return nil
}

// insertAfterIHDR returns png with chunk inserted immediately after the IHDR
// chunk, a position valid for ancillary chunks such as eXIf.
func insertAfterIHDR(png, chunk []byte) []byte {
	const sig = 8
	ihdrLen := int(binary.BigEndian.Uint32(png[sig:]))
	pos := sig + 12 + ihdrLen
	out := make([]byte, 0, len(png)+len(chunk))
	out = append(out, png[:pos]...)
	out = append(out, chunk...)
	out = append(out, png[pos:]...)
	return out
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
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}

	if err := populate(tmpName); err != nil {
		return err
	}
	if err := os.Link(tmpName, dst); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("destination already exists: %s", dst)
		}
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
