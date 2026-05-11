package render

import (
	"image"
	"image/draw"
	"math"
	"os"

	_ "image/png"
)

type HandRenderer struct {
	Sprite image.Image
	TipX   int
	TipY   int
}

func NewHandRenderer(path string) (*HandRenderer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}

	return &HandRenderer{Sprite: img}, nil
}

func (h *HandRenderer) Draw(dst draw.Image, x, y int, frame int) {
	// Add "breathing" jitter
	// cycle of 60 frames (2 seconds at 30fps)
	jitter := 3.0 * math.Sin(2*math.Pi*float64(frame)/60.0)
	
	offset := image.Pt(x-h.TipX, y-h.TipY+int(jitter))

	// Draw hand sprite
	draw.Draw(dst, h.Sprite.Bounds().Add(offset), h.Sprite, image.Point{}, draw.Over)
}
