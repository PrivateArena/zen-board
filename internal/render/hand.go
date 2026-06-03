package render

import (
	"image"
	"image/draw"
	"math"
	"os"
	"path/filepath"

	_ "image/jpeg"
	_ "image/png"
)

type HandRenderer struct {
	Sprites map[string]image.Image
	TipX    int
	TipY    int
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

	scaledImg := scaleImage(img, 256, 256)
	sprites := map[string]image.Image{
		"default": scaledImg,
	}

	// Try loading pencil, chalk, eraser, marker variants from the same directory
	dir := filepath.Dir(path)
	variants := []string{"pencil", "chalk", "eraser", "marker"}
	for _, v := range variants {
		vPath := filepath.Join(dir, "hand_"+v+".png")
		if vf, err := os.Open(vPath); err == nil {
			if vImg, _, err := image.Decode(vf); err == nil {
				sprites[v] = scaleImage(vImg, 256, 256)
			}
			vf.Close()
		}
	}

	return &HandRenderer{Sprites: sprites}, nil
}

func (h *HandRenderer) Draw(dst draw.Image, x, y int, frame int, style string) {
	// Add "breathing" jitter
	jitter := 3.0 * math.Sin(2*math.Pi*float64(frame)/60.0)
	
	sprite := h.Sprites["default"]
	if s, ok := h.Sprites[style]; ok {
		sprite = s
	}

	tipX, tipY := h.TipX, h.TipY
	if style == "eraser" {
		// Eraser tip is centered
		tipX = sprite.Bounds().Dx() / 2
		tipY = sprite.Bounds().Dy() / 2
	}
	
	offset := image.Pt(x-tipX, y-tipY+int(jitter))

	// Draw hand sprite
	draw.Draw(dst, sprite.Bounds().Add(offset), sprite, image.Point{}, draw.Over)
}

