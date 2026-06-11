package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"
)

// pngMetaChunks lists ancillary PNG chunk types that carry color or metadata
// information independent of how the pixels are encoded, so they stay valid
// across a re-encode. Chunks tied to the source's exact pixel encoding (PLTE,
// tRNS, bKGD, sBIT, hIST, sPLT) are intentionally excluded: the encoder
// regenerates the palette and transparency as needed for the output's own
// pixel format, and the rest could misdescribe it.
// pHYs (physical resolution) is kept verbatim; for the near-universal case of a
// square DPI this is exact, and only anisotropic DPI under a 90°/270° rotate
// would be slightly off.
var pngMetaChunks = map[string]bool{
	"iCCP": true, // ICC color profile
	"sRGB": true, // standard RGB rendering intent
	"gAMA": true, // gamma
	"cHRM": true, // chromaticities
	"pHYs": true, // physical pixel dimensions (DPI)
	"tIME": true, // last-modification time
	"eXIf": true, // EXIF metadata
	"tEXt": true, // textual metadata
	"iTXt": true,
	"zTXt": true,
}

// loadPNG decodes the PNG at path and returns its pixels along with the verbatim
// bytes of every preserved metadata chunk (see pngMetaChunks), concatenated in
// source order. The chunks are carried separately because png.Encode writes
// none of them.
func loadPNG(path string) (image.Image, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, fmt.Errorf("decode: %w", err)
	}
	return img, pngCarryChunks(raw), nil
}

// pngEncoder re-encodes pixels at maximum compression. Re-encoding is
// pixel-lossless (the concrete image type, and thus bit depth and palette, is
// preserved by the in-process transforms); only the byte-level filtering and
// compression differ from the source, so we choose the smallest output.
var pngEncoder = png.Encoder{CompressionLevel: png.BestCompression}

// writePNG encodes img to dst losslessly, re-attaching the carried metadata
// chunks (if any) after IHDR and optionally resetting the EXIF Orientation. The
// output never overwrites an existing file.
func writePNG(dst string, img image.Image, carry []byte, resetOrientation bool) error {
	return commit(dst, func(tmp string) error {
		var buf bytes.Buffer
		if err := pngEncoder.Encode(&buf, img); err != nil {
			return err
		}
		out := buf.Bytes()
		if len(carry) > 0 {
			out = insertAfterIHDR(out, carry)
		}
		if err := os.WriteFile(tmp, out, 0o644); err != nil {
			return err
		}
		// A physical rotation makes a carried-over Orientation stale; reset it
		// (also adds the tag when the source had no EXIF).
		if resetOrientation {
			return setOrientationNormal(tmp)
		}
		return nil
	})
}

// pngCarryChunks returns the verbatim bytes of all preserved metadata chunks in
// the PNG, concatenated in source order, or nil if there are none.
func pngCarryChunks(data []byte) []byte {
	var out []byte
	eachPNGChunk(data, func(typ string, chunk []byte) bool {
		if pngMetaChunks[typ] {
			out = append(out, chunk...)
		}
		return true
	})
	return out
}

// eachPNGChunk calls fn with the type and verbatim bytes (length + type + data +
// CRC) of each chunk in a PNG, stopping early if fn returns false or the data is
// malformed. Copying chunk bytes verbatim, CRC included, keeps them valid when
// re-spliced.
func eachPNGChunk(data []byte, fn func(typ string, chunk []byte) bool) {
	const sig = 8
	if len(data) < sig {
		return
	}
	for i := sig; i+12 <= len(data); {
		ln := binary.BigEndian.Uint32(data[i:])
		// Compare in uint64 against the bytes remaining after the 12-byte
		// framing (length+type+CRC) so a hostile length can't overflow int.
		if uint64(ln) > uint64(len(data)-(i+12)) {
			return
		}
		end := i + 12 + int(ln)
		if !fn(string(data[i+4:i+8]), data[i:end]) {
			return
		}
		i = end
	}
}

// insertAfterIHDR returns png with chunk inserted immediately after the IHDR
// chunk, a position valid for ancillary chunks such as iCCP and eXIf.
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
