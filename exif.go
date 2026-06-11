package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"os"
)

// readOrientation returns the EXIF Orientation tag (1-8) of the image at path,
// or 0 when the file carries no usable tag. Malformed metadata is treated as
// absent rather than an error: the transform itself will surface real
// corruption, and a broken EXIF block shouldn't fail an otherwise good image.
func readOrientation(path, format string) (int, error) {
	switch format {
	case "jpeg":
		return jpegOrientation(path)
	case "png":
		return pngOrientation(path)
	}
	return 0, nil
}

// jpegOrientation scans the segments before the image data for an APP1 EXIF
// block and extracts the Orientation tag.
func jpegOrientation(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var soi [2]byte
	if _, err := io.ReadFull(r, soi[:]); err != nil || soi != [2]byte{0xff, 0xd8} {
		return 0, nil
	}
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, nil
		}
		if b != 0xff {
			continue
		}
		marker, err := r.ReadByte()
		if err != nil {
			return 0, nil
		}
		for marker == 0xff { // fill bytes before a marker are legal
			if marker, err = r.ReadByte(); err != nil {
				return 0, nil
			}
		}
		switch {
		case marker == 0x01 || (marker >= 0xd0 && marker <= 0xd8): // standalone, no length
			continue
		case marker == 0xd9 || marker == 0xda: // EOI / start of scan: no EXIF ahead
			return 0, nil
		}
		var lnb [2]byte
		if _, err := io.ReadFull(r, lnb[:]); err != nil {
			return 0, nil
		}
		n := int(binary.BigEndian.Uint16(lnb[:])) - 2 // length includes itself
		if n < 0 {
			return 0, nil
		}
		if marker != 0xe1 {
			if _, err := r.Discard(n); err != nil {
				return 0, nil
			}
			continue
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil {
			return 0, nil
		}
		if bytes.HasPrefix(body, []byte("Exif\x00\x00")) {
			return tiffOrientation(body[6:]), nil
		}
	}
}

// pngOrientation extracts the Orientation tag from a PNG's eXIf chunk, whose
// payload is a raw TIFF block.
func pngOrientation(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	orientation := 0
	eachPNGChunk(raw, func(typ string, chunk []byte) bool {
		if typ != "eXIf" {
			return true
		}
		orientation = tiffOrientation(chunk[8 : len(chunk)-4]) // strip length+type and CRC
		return false
	})
	return orientation, nil
}

// tiffOrientation walks IFD0 of a TIFF block (as embedded in EXIF) and returns
// the Orientation tag's value (1-8), or 0 if absent or malformed.
func tiffOrientation(b []byte) int {
	if len(b) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch {
	case b[0] == 'I' && b[1] == 'I':
		bo = binary.LittleEndian
	case b[0] == 'M' && b[1] == 'M':
		bo = binary.BigEndian
	default:
		return 0
	}
	if bo.Uint16(b[2:]) != 42 {
		return 0
	}
	ifd := int64(bo.Uint32(b[4:]))
	if ifd < 8 || ifd+2 > int64(len(b)) {
		return 0
	}
	n := int64(bo.Uint16(b[ifd:]))
	for i := range n {
		e := ifd + 2 + i*12
		if e+12 > int64(len(b)) {
			return 0
		}
		if bo.Uint16(b[e:]) != 0x0112 { // Orientation
			continue
		}
		// Must be type SHORT with count 1; the value then sits left-justified
		// in the 4-byte value field for both byte orders.
		if bo.Uint16(b[e+2:]) != 3 || bo.Uint32(b[e+4:]) != 1 {
			return 0
		}
		if v := int(bo.Uint16(b[e+8:])); v >= 1 && v <= 8 {
			return v
		}
		return 0
	}
	return 0
}
