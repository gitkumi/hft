# hft

Half frame tools — utilities for working with half-frame film scans.

## Commands

- `hft cut [flags] <path>` — split scans laid out as `[photo][blackbar][photo]` into left/right halves.
- `hft rotate [flags] <path>` — rotate images by 90, 180, or 270 degrees clockwise.

`<path>` may be a single image or a directory (walked recursively).

## Lossless

Both commands preserve image quality exactly:

- **JPEG** is transformed in the DCT domain by [`jpegtran`](https://linux.die.net/man/1/jpegtran)
  (from libjpeg-turbo) — no decode/re-encode, so pixels are bit-for-bit
  preserved. `jpegtran` must be on your `PATH`. Because lossless JPEG transforms
  work on 8/16px block boundaries, the `cut` split snaps to the nearest block.
- **PNG** is decoded and re-encoded losslessly in-process (no external tools).

## EXIF metadata

EXIF (camera, date, GPS, etc.) is carried over to the output for both JPEG and
PNG. For PNG it is preserved via the standard `eXIf` chunk — the source chunk is
copied verbatim into the re-encoded file.

- `cut` copies metadata verbatim, including the `Orientation` tag.
- `rotate` physically reorients the pixels, then resets `Orientation` to
  Normal(1) so EXIF-aware viewers don't rotate the already-upright image again
  (adding the tag if the source had none). This step uses
  [`exiv2`](https://exiv2.org), which must be on your `PATH`.

## Notes

- Output never overwrites an existing file. If a destination already exists,
  that image is reported as an error and the original is left untouched.
  `cut` names its outputs `<base>_L`/`<base>_R`, and `rotate` names its output
  `<base>_rotated_<deg>`, so neither collides with the source.
