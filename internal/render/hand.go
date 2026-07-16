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

// styleAngle maps draw style → pre-baked rotation in degrees.
// Positive = clockwise. Chosen to match natural pen grip per stroke direction.
var styleAngle = map[string]int{
	"default": 0,
	"pencil":  0,
	"chalk":   5,
	"marker":  -10,
	"eraser":  0,
}

// HandRenderer holds the sprite set and a rotation cache keyed by (style, angle).
type HandRenderer struct {
	Sprites  map[string]image.Image
	rotCache map[string]map[int]image.Image // style → angleDeg → rotated sprite
	TipX     int
	TipY     int
}

func NewHandRenderer(path string, origTipX, origTipY int) (*HandRenderer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}

	origW := img.Bounds().Dx()
	origH := img.Bounds().Dy()

	scaledImg := scaleImage(img, 256, 256)
	sprites := map[string]image.Image{
		"default": scaledImg,
	}

	dir := filepath.Dir(path)
	for _, v := range []string{"pencil", "chalk", "eraser", "marker"} {
		vPath := filepath.Join(dir, "hand_"+v+".png")
		if vf, err := os.Open(vPath); err == nil {
			if vImg, _, err := image.Decode(vf); err == nil {
				sprites[v] = scaleImage(vImg, 256, 256)
			}
			vf.Close()
		}
	}

	scaledTipX := int(math.Round(float64(origTipX) * 256.0 / float64(origW)))
	scaledTipY := int(math.Round(float64(origTipY) * 256.0 / float64(origH)))

	hr := &HandRenderer{
		Sprites: sprites,
		TipX:    scaledTipX,
		TipY:    scaledTipY,
	}
	hr.buildRotCache()
	return hr, nil
}

// cacheBuckets are the pre-computed rotation angles (5° increments, ±30°).
var cacheBuckets = []int{-30, -25, -20, -15, -10, -5, 0, 5, 10, 15, 20, 25, 30}

func (h *HandRenderer) buildRotCache() {
	h.rotCache = make(map[string]map[int]image.Image)
	for style, sprite := range h.Sprites {
		h.rotCache[style] = make(map[int]image.Image)
		for _, deg := range cacheBuckets {
			if deg == 0 {
				h.rotCache[style][deg] = sprite
			} else {
				h.rotCache[style][deg] = rotateSprite(sprite, float64(deg))
			}
		}
	}
}

// rotateSprite rotates a sprite clockwise by angleDeg degrees around its center.
// Uses nearest-neighbor sampling — fast enough for pre-computation at load time.
func rotateSprite(src image.Image, angleDeg float64) image.Image {
	b := src.Bounds()
	cx := float64(b.Dx()) / 2
	cy := float64(b.Dy()) / 2
	rad := angleDeg * math.Pi / 180.0
	cos, sin := math.Cos(rad), math.Sin(rad)

	dst := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			// Inverse rotation to find source pixel
			rx, ry := float64(x)-cx, float64(y)-cy
			srcX := int(math.Round(cx + rx*cos + ry*sin))
			srcY := int(math.Round(cy - rx*sin + ry*cos))
			if srcX >= b.Min.X && srcX < b.Max.X && srcY >= b.Min.Y && srcY < b.Max.Y {
				dst.Set(x, y, src.At(srcX, srcY))
			}
		}
	}
	return dst
}

// snapAngle snaps angleDeg to nearest 5° bucket clamped to ±30°.
func snapAngle(deg int) int {
	snapped := int(math.Round(float64(deg)/5.0)) * 5
	if snapped > 30 {
		snapped = 30
	}
	if snapped < -30 {
		snapped = -30
	}
	return snapped
}

// Draw renders the hand sprite at (x,y) with the tip aligned to that point.
// angleDeg rotates the sprite; frame drives the breathing jitter.
func (h *HandRenderer) Draw(dst draw.Image, x, y int, frame int, style string, angleDeg int) {
	// Breathing jitter: subtle ±3px vertical bob
	jitter := 3.0 * math.Sin(2*math.Pi*float64(frame)/60.0)

	// Resolve sprite from rotation cache
	sprite := h.Sprites["default"]
	if styleCache, ok := h.rotCache[style]; ok {
		bucket := snapAngle(angleDeg)
		if rotated, ok := styleCache[bucket]; ok {
			sprite = rotated
		}
	} else if s, ok := h.Sprites[style]; ok {
		sprite = s
	}

	tipX, tipY := h.TipX, h.TipY
	if style == "eraser" {
		tipX = sprite.Bounds().Dx() / 2
		tipY = sprite.Bounds().Dy() / 2
	} else {
		bucket := snapAngle(angleDeg)
		if bucket != 0 {
			if styleCache, ok := h.rotCache[style]; ok {
				if _, ok := styleCache[bucket]; ok {
					cx := float64(sprite.Bounds().Dx()) / 2
					cy := float64(sprite.Bounds().Dy()) / 2
					rad := float64(bucket) * math.Pi / 180.0
					cos, sin := math.Cos(rad), math.Sin(rad)
					rx := float64(tipX) - cx
					ry := float64(tipY) - cy
					tipX = int(math.Round(cx + rx*cos - ry*sin))
					tipY = int(math.Round(cy + rx*sin + ry*cos))
				}
			}
		}
	}

	offset := image.Pt(x-tipX, y-tipY+int(jitter))
	draw.Draw(dst, sprite.Bounds().Add(offset), sprite, image.Point{}, draw.Over)
}
