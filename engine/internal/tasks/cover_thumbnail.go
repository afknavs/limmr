package tasks

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"limmr/internal/dbutil"
	"os"
	"path/filepath"
)

// Only bounded, locally generated pixels reach the app. Originals remain untouched.
func (s *Service) coverThumbnail(path string, thumb []byte) (string, error) {
	if regularFile(path) {
		info, err := os.Stat(path)
		if err == nil && info.Size() <= 100<<20 {
			cached := path + ".cover-1280.jpg"
			if ci, e := os.Stat(cached); e == nil && ci.Size() > 0 && !ci.ModTime().Before(info.ModTime()) {
				return cached, nil
			}
			f, e := os.Open(path)
			if e == nil {
				defer f.Close()
				if e = writeCoverThumbnail(f, cached); e == nil {
					return cached, nil
				}
			}
		}
	}
	if len(thumb) == 0 || len(thumb) > 4<<20 {
		return "", fmt.Errorf("cover pixels unavailable")
	}
	dir := filepath.Join(s.Hub.Home, "media")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	cached := filepath.Join(dir, dbutil.Digest(string(thumb))+".cover-1280.jpg")
	if regularFile(cached) {
		return cached, nil
	}
	return cached, writeCoverThumbnail(bytes.NewReader(thumb), cached)
}

func writeCoverThumbnail(r io.ReadSeeker, path string) error {
	config, _, err := image.DecodeConfig(r)
	if err != nil {
		return err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		return fmt.Errorf("cover dimensions exceed limit")
	}
	if _, err = r.Seek(0, io.SeekStart); err != nil {
		return err
	}
	src, _, err := image.Decode(r)
	if err != nil {
		return err
	}
	w, h := config.Width, config.Height
	if max(w, h) > 1280 {
		w, h = max(1, w*1280/max(config.Width, config.Height)), max(1, h*1280/max(config.Width, config.Height))
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	bounds := src.Bounds()
	// Average source cells while shrinking, avoiding aliasing on posters and text.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			x0, x1 := x*config.Width/w, max(x*config.Width/w+1, (x+1)*config.Width/w)
			y0, y1 := y*config.Height/h, max(y*config.Height/h+1, (y+1)*config.Height/h)
			var rr, gg, bb, aa, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					r, g, b, a := coverRGBA(src, bounds.Min.X+sx, bounds.Min.Y+sy)
					rr += uint64(r)
					gg += uint64(g)
					bb += uint64(b)
					aa += uint64(a)
					n++
				}
			}
			i := dst.PixOffset(x, y)
			// Flatten transparent pixels onto white before JPEG encoding.
			dst.Pix[i] = uint8((rr/n + 65535 - aa/n) >> 8)
			dst.Pix[i+1] = uint8((gg/n + 65535 - aa/n) >> 8)
			dst.Pix[i+2] = uint8((bb/n + 65535 - aa/n) >> 8)
			dst.Pix[i+3] = 255
		}
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cover-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = jpeg.Encode(f, dst, &jpeg.Options{Quality: 85})
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

// Common decoders expose packed pixels; avoid image.Image.At and color allocations
// for every source pixel in a large photo. Keep a fallback for other image types.
func coverRGBA(src image.Image, x, y int) (r, g, b, a uint32) {
	switch src := src.(type) {
	case *image.YCbCr:
		yi, ci := src.YOffset(x, y), src.COffset(x, y)
		// Same 16-bit conversion as YCbCr.At, without boxing a color per pixel.
		return color.YCbCr{Y: src.Y[yi], Cb: src.Cb[ci], Cr: src.Cr[ci]}.RGBA()
	case *image.RGBA:
		i := src.PixOffset(x, y)
		return uint32(src.Pix[i]) * 257, uint32(src.Pix[i+1]) * 257, uint32(src.Pix[i+2]) * 257, uint32(src.Pix[i+3]) * 257
	case *image.NRGBA:
		i := src.PixOffset(x, y)
		a = uint32(src.Pix[i+3])
		return uint32(src.Pix[i]) * 257 * a / 255, uint32(src.Pix[i+1]) * 257 * a / 255, uint32(src.Pix[i+2]) * 257 * a / 255, a * 257
	case *image.Gray:
		v := uint32(src.Pix[src.PixOffset(x, y)]) * 257
		return v, v, v, 65535
	default:
		return src.At(x, y).RGBA()
	}
}
