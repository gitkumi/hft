package main

import (
	"image"
	"image/draw"
)

// crop returns a transform that extracts rect (in 0-origin coordinates) from an
// image. The extraction is byte-exact: the result keeps the source's concrete
// type — bit depth, alpha representation, and palette are all preserved.
func crop(rect image.Rectangle) func(image.Image) image.Image {
	return func(img image.Image) image.Image {
		sr := rect.Add(img.Bounds().Min)
		return geomTransform(img, sr, rect.Dx(), rect.Dy(),
			func(x, y int) (int, int) { return x, y })
	}
}

// rotateImage rotates src clockwise by angle (90, 180, or 270 degrees),
// returning src unchanged for any other angle. The rotation moves pixels as raw
// bytes, so it is fully lossless regardless of the source's bit depth, alpha
// mode, or palette.
func rotateImage(src image.Image, angle int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	switch angle {
	case 90:
		return geomTransform(src, b, h, w, func(x, y int) (int, int) { return h - 1 - y, x })
	case 180:
		return geomTransform(src, b, w, h, func(x, y int) (int, int) { return w - 1 - x, h - 1 - y })
	case 270:
		return geomTransform(src, b, h, w, func(x, y int) (int, int) { return y, w - 1 - x })
	}
	return src
}

// packed exposes one of the standard library's in-memory image types as a flat
// pixel buffer (whole bytes per pixel), so geometric transforms can move pixels
// by direct slice copy without per-pixel color conversion or precision loss.
type packed struct {
	img    image.Image
	pix    []byte
	stride int
	bpp    int // bytes per pixel
}

// asPacked unwraps img into a packed view and returns a constructor for a fresh
// same-typed image of arbitrary size (carrying the palette for paletted images).
// ok is false for image types it does not recognize; callers fall back to a
// 16-bit conversion that is still lossless for any source up to 16 bits/channel.
func asPacked(img image.Image) (src packed, newLike func(w, h int) packed, ok bool) {
	switch m := img.(type) {
	case *image.RGBA:
		return packed{m, m.Pix, m.Stride, 4}, func(w, h int) packed {
			n := image.NewRGBA(image.Rect(0, 0, w, h))
			return packed{n, n.Pix, n.Stride, 4}
		}, true
	case *image.RGBA64:
		return packed{m, m.Pix, m.Stride, 8}, func(w, h int) packed {
			n := image.NewRGBA64(image.Rect(0, 0, w, h))
			return packed{n, n.Pix, n.Stride, 8}
		}, true
	case *image.NRGBA:
		return packed{m, m.Pix, m.Stride, 4}, func(w, h int) packed {
			n := image.NewNRGBA(image.Rect(0, 0, w, h))
			return packed{n, n.Pix, n.Stride, 4}
		}, true
	case *image.NRGBA64:
		return packed{m, m.Pix, m.Stride, 8}, func(w, h int) packed {
			n := image.NewNRGBA64(image.Rect(0, 0, w, h))
			return packed{n, n.Pix, n.Stride, 8}
		}, true
	case *image.Gray:
		return packed{m, m.Pix, m.Stride, 1}, func(w, h int) packed {
			n := image.NewGray(image.Rect(0, 0, w, h))
			return packed{n, n.Pix, n.Stride, 1}
		}, true
	case *image.Gray16:
		return packed{m, m.Pix, m.Stride, 2}, func(w, h int) packed {
			n := image.NewGray16(image.Rect(0, 0, w, h))
			return packed{n, n.Pix, n.Stride, 2}
		}, true
	case *image.Alpha:
		return packed{m, m.Pix, m.Stride, 1}, func(w, h int) packed {
			n := image.NewAlpha(image.Rect(0, 0, w, h))
			return packed{n, n.Pix, n.Stride, 1}
		}, true
	case *image.Alpha16:
		return packed{m, m.Pix, m.Stride, 2}, func(w, h int) packed {
			n := image.NewAlpha16(image.Rect(0, 0, w, h))
			return packed{n, n.Pix, n.Stride, 2}
		}, true
	case *image.CMYK:
		return packed{m, m.Pix, m.Stride, 4}, func(w, h int) packed {
			n := image.NewCMYK(image.Rect(0, 0, w, h))
			return packed{n, n.Pix, n.Stride, 4}
		}, true
	case *image.Paletted:
		pal := m.Palette
		return packed{m, m.Pix, m.Stride, 1}, func(w, h int) packed {
			n := image.NewPaletted(image.Rect(0, 0, w, h), pal)
			return packed{n, n.Pix, n.Stride, 1}
		}, true
	}
	return packed{}, nil, false
}

// geomTransform builds a new image of the same concrete type as src and of size
// dstW×dstH, copying each pixel of the source sub-rectangle sr (in src's own
// coordinate space) to the destination coordinate produced by fwd. fwd receives
// region-local coordinates (0-based within sr) and returns 0-based destination
// coordinates. Pixels move as raw bytes, so the result is byte-for-byte lossless
// for every recognized image type.
func geomTransform(src image.Image, sr image.Rectangle, dstW, dstH int, fwd func(x, y int) (int, int)) image.Image {
	sr = sr.Intersect(src.Bounds())

	p, newLike, ok := asPacked(src)
	if !ok {
		// Unknown type: convert once to non-premultiplied 16-bit, which holds
		// any standard source without loss, then proceed on the packed buffer.
		src = toNRGBA64(src)
		p, newLike, _ = asPacked(src)
	}

	b := src.Bounds()
	ox, oy := sr.Min.X-b.Min.X, sr.Min.Y-b.Min.Y
	rw, rh := sr.Dx(), sr.Dy()
	bpp := p.bpp

	d := newLike(dstW, dstH)
	for y := range rh {
		srow := p.pix[(oy+y)*p.stride:]
		for x := range rw {
			X, Y := fwd(x, y)
			si := (ox + x) * bpp
			di := Y*d.stride + X*bpp
			copy(d.pix[di:di+bpp], srow[si:si+bpp])
		}
	}
	return d.img
}

// toNRGBA64 renders src into a non-premultiplied 16-bit-per-channel image,
// preserving its bounds. This is the lossless fallback for image types not
// recognized by asPacked.
func toNRGBA64(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewNRGBA64(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	return dst
}
