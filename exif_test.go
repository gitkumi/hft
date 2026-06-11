package main

import (
	"bytes"
	"encoding/binary"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// tiffWithOrientation builds a minimal TIFF block whose IFD0 holds only an
// Orientation tag.
func tiffWithOrientation(littleEndian bool, orientation uint16) []byte {
	var bo binary.AppendByteOrder = binary.BigEndian
	b := []byte{'M', 'M'}
	if littleEndian {
		bo = binary.LittleEndian
		b = []byte{'I', 'I'}
	}
	b = bo.AppendUint16(b, 42)
	b = bo.AppendUint32(b, 8)      // IFD0 starts right after the header
	b = bo.AppendUint16(b, 1)      // one entry
	b = bo.AppendUint16(b, 0x0112) // Orientation
	b = bo.AppendUint16(b, 3)      // type SHORT
	b = bo.AppendUint32(b, 1)      // count
	b = bo.AppendUint16(b, orientation)
	b = bo.AppendUint16(b, 0) // value field padding
	b = bo.AppendUint32(b, 0) // no next IFD
	return b
}

// jpegWithEXIF wraps a TIFF block in a minimal JPEG (SOI, APP1, EOI) — enough
// for the segment scanner, which never reaches the image data.
func jpegWithEXIF(tiff []byte) []byte {
	payload := append([]byte("Exif\x00\x00"), tiff...)
	b := []byte{0xff, 0xd8, 0xff, 0xe1}
	b = binary.BigEndian.AppendUint16(b, uint16(len(payload)+2))
	b = append(b, payload...)
	return append(b, 0xff, 0xd9)
}

func TestJPEGOrientation(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name         string
		littleEndian bool
		want         int
	}{
		{"little-endian", true, 6},
		{"big-endian", false, 8},
	}
	for _, c := range cases {
		p := filepath.Join(dir, c.name+".jpg")
		if err := os.WriteFile(p, jpegWithEXIF(tiffWithOrientation(c.littleEndian, uint16(c.want))), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := readOrientation(p, "jpeg")
		if err != nil || got != c.want {
			t.Errorf("%s: orientation = %d, %v; want %d", c.name, got, err, c.want)
		}
	}
}

func TestJPEGOrientationAbsent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plain.jpg")
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, gradient(8, 8), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := readOrientation(p, "jpeg"); err != nil || got != 0 {
		t.Errorf("orientation = %d, %v; want 0 (absent)", got, err)
	}
}

func TestPNGOrientation(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := png.Encode(&buf, gradient(4, 4)); err != nil {
		t.Fatal(err)
	}

	plain := filepath.Join(dir, "plain.png")
	if err := os.WriteFile(plain, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := readOrientation(plain, "png"); err != nil || got != 0 {
		t.Errorf("plain: orientation = %d, %v; want 0 (absent)", got, err)
	}

	withExif := filepath.Join(dir, "exif.png")
	data := insertAfterIHDR(buf.Bytes(), makeChunk("eXIf", tiffWithOrientation(true, 6)))
	if err := os.WriteFile(withExif, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := readOrientation(withExif, "png"); err != nil || got != 6 {
		t.Errorf("eXIf: orientation = %d, %v; want 6", got, err)
	}
}

func TestTIFFOrientationMalformed(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte("short"),
		[]byte("XX\x00\x2a\x00\x00\x00\x08"), // bad byte-order mark
		[]byte("MM\x00\x2b\x00\x00\x00\x08"), // bad magic
		[]byte("MM\x00\x2a\xff\xff\xff\xff"), // IFD offset overruns
		append(tiffWithOrientation(false, 6)[:12], 0x99), // entry table truncated
		tiffWithOrientation(true, 0),                     // value out of range
		tiffWithOrientation(true, 9),                     // value out of range
	}
	for i, c := range cases {
		if got := tiffOrientation(c); got != 0 {
			t.Errorf("case %d: got %d, want 0", i, got)
		}
	}
}
