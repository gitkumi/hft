package main

import (
	"bytes"
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
// is inserted before the file extension.
type outputPlan struct {
	suffix       string
	jpegtranArgs []string
	transform    func(image.Image) image.Image
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
	var decoded image.Image
	if format == "png" {
		df, err := os.Open(path)
		if err != nil {
			return err
		}
		decoded, _, err = image.Decode(df)
		df.Close()
		if err != nil {
			return fmt.Errorf("decode: %w", err)
		}
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
			if err := commit(dst, func(w io.Writer) error {
				return jpegtranTo(w, path, o.jpegtranArgs)
			}); err != nil {
				return err
			}
		case "png":
			if err := commit(dst, func(w io.Writer) error {
				return png.Encode(w, o.transform(decoded))
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
// w. Metadata is dropped (-copy none) to match the previous decode/encode
// behavior and to avoid stale EXIF orientation after a rotate.
func jpegtranTo(w io.Writer, src string, args []string) error {
	full := make([]string, 0, len(args)+3)
	full = append(full, "-copy", "none")
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

// commit writes to dst without ever overwriting an existing file. It writes via
// a sibling temp file (so a failed write never leaves a truncated result) and
// then hard-links it into place; the link fails atomically if dst already
// exists, even under concurrent writers.
func commit(dst string, write func(io.Writer) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".hft-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the link below succeeds

	if err := write(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
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
