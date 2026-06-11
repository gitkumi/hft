# hft

Half frame tools — utilities for working with half-frame film scans.

## Commands

- `hft cut [flags] <path>` — split scans laid out as `[photo][blackbar][photo]` into left/right halves.
  Flags: `--bar` (middle bar width), `-o/--out`, `-j/--workers`.
- `hft rotate [flags] <path>` — rotate images by 90, 180, or 270 degrees clockwise.
  Flags: `-a/--angle`, `-o/--out`, `-j/--workers`.
- `hft resize [flags] <path>` — resample images to a new size (lossy).
  Flags: `-w/--width`, `-H/--height`, `--scale`, `--filter`, `-q/--quality`,
  `-o/--out`, `-j/--workers`. Give `--width` or `--height` alone to preserve the
  aspect ratio, both for an exact size, or `--scale 0.5` for a uniform factor.

`<path>` may be a single image or a directory (walked recursively). Supported
formats are **JPEG** and **PNG**. Flags are POSIX-style (`--angle 90` or `-a 90`).
Run `hft <command> --help` for full details.

## Lossless

`cut` and `rotate` preserve image quality exactly:

- **JPEG** is transformed in the DCT domain by [`jpegtran`](https://linux.die.net/man/1/jpegtran)
  (libjpeg-turbo, must be on your `PATH`) — no decode/re-encode. Lossless transforms
  work on whole DCT blocks, with two consequences:
  - the `cut` split widens each half leftward to the previous block boundary, so the
    right half can come out up to 15 px wider than the left and include more of the bar;
  - `rotate` is exact only when both dimensions are block-aligned. When they aren't,
    hft warns and retries with `-trim`, discarding up to 15 px of untransformable
    edge pixels rather than leaving them visibly out of place.
- **PNG** is decoded and re-encoded in-process, pixel-for-pixel lossless. Bit depth,
  color type, and alpha are preserved exactly; only the byte-level filtering and
  compression are recomputed, so output bytes may differ while every pixel matches.

`resize` is **not** lossless: it resamples (high-quality Catmull-Rom by default;
`--filter nearest|bilinear|catmullrom`) and re-encodes (at `--quality`, default 90,
for JPEG). It keeps the source's bit-depth class and grayscale/color, but no metadata.

## Metadata

For `cut` and `rotate`, metadata is carried over:

- **JPEG** keeps everything (EXIF, ICC, etc.) via `jpegtran -copy all`.
- **PNG** keeps color and metadata chunks (`iCCP`, `sRGB`, `gAMA`, `cHRM`, `pHYs`,
  `tIME`, `eXIf`, text). Chunks tied to the source's exact pixel encoding (`PLTE`,
  `tRNS`, `bKGD`, `sBIT`, …) are not copied; the encoder regenerates the palette
  and transparency as needed for the output's pixel format.

For the `Orientation` tag:

- `cut` copies it verbatim, and warns when it isn't Normal(1) — the split runs
  down the stored pixels, so `_L`/`_R` may not match the displayed left/right.
- `rotate` rotates the image *as displayed*: an existing `Orientation` rotation
  is composed into the physical rotation, and the tag is then reset to Normal(1)
  so EXIF-aware viewers don't re-rotate the result. Mirrored orientations
  (2, 4, 5, 7) can't be expressed as a pure rotation and are reported as errors.
  The reset uses [`exiv2`](https://exiv2.org), which must be on your `PATH` —
  but only for files whose `Orientation` is set to something other than Normal.

`resize` carries no metadata for either format.

## Notes

- Output never overwrites an existing file; an existing destination is reported as
  an error and the original is left untouched. Outputs are named `<base>_L`/`<base>_R`
  (`cut`), `<base>_rotated_<deg>` (`rotate`), and `<base>_<w>x<h>` (`resize`).
- Per-file failures are reported as they happen and the run exits non-zero with an
  `N of M files failed` summary; files that succeeded are kept.
