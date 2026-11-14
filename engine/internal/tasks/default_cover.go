package tasks

import (
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"limmr/internal/dbutil"
	"os"
	"path/filepath"
)

// Three fixed Paper covers for clients that need downloadable pixels.
// The native app typesets its own adaptive placeholder for these same IDs.
// No model, network, personal content or chat recency participates.
func defaultCoverID(id string) string { return "default:" + string('0'+dbutil.Digest(id)[0]%3) }
func (s *Service) defaultCover(id string) (string, error) {
	key := defaultCoverID(id)
	dir := filepath.Join(s.Hub.Home, "media")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "task-paper-v1-"+key[len(key)-1:]+".jpg")
	if regularFile(path) {
		return path, nil
	}
	img := paperDefaultCover(int(key[len(key)-1] - '0'))
	f, err := os.CreateTemp(dir, "task-cover-*.jpg")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
		f.Close()
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

// A quiet checklist imprint on warm paper. Keep the top-left clear for client
// badges. These are semantic strokes, not simulated document text or content.
func paperDefaultCover(variant int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 640, 800))
	paper := color.RGBA{233, 229, 221, 255} // LimmrTheme.surface2, light
	ink := color.RGBA{151, 148, 142, 255}
	rule := color.RGBA{195, 191, 184, 255}
	draw.Draw(img, img.Bounds(), image.NewUniform(paper), image.Point{}, draw.Src)
	stroke := func(x, y, w, h int, c color.RGBA) {
		draw.Draw(img, image.Rect(x, y, x+w, y+h), image.NewUniform(c), image.Point{}, draw.Src)
	}
	x, y := 80, 400
	if variant == 1 {
		x, y = 404, 424
	}
	if variant == 2 {
		x, y = 252, 376
	}
	// Three empty checklist boxes and their rules are the downloadable counterpart
	// of the app's small checklist glyph, never a claim about the task's progress.
	for row := 0; row < 3; row++ {
		top := y + row*32
		stroke(x, top, 14, 2, ink)
		stroke(x, top+12, 14, 2, ink)
		stroke(x, top, 2, 14, ink)
		stroke(x+12, top, 2, 14, ink)
		stroke(x+28, top+6, 84, 2, ink)
	}
	if variant == 1 {
		stroke(x-24, y-8, 2, 96, rule)
	} else {
		stroke(x, y+112, 112, 2, rule)
	}
	return img
}
