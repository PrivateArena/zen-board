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
	
	offset := image.Pt(x, y+int(jitter))
	
	// Draw hand sprite
	// Assuming the pen tip is at the top-left of the sprite (0,0 relative to sprite)
	// If the tip is elsewhere, we need an offset here.
	// For most "pen hands", the tip is roughly at (30, 20) or similar.
	// Let's assume (0,0) for now and let user adjust or provide better asset.
	
	draw.Draw(dst, h.Sprite.Bounds().Add(offset), h.Sprite, image.Point{}, draw.Over)
}
