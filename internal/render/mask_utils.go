package render

import (
	"image"
	"image/color"
)

func ApplyAlpha(c color.RGBA, visibility float64) color.RGBA {
	c.A = uint8(float64(c.A) * visibility)
	return c
}

func ApplyEasedProgressMask(mask *image.Alpha, easedProgress, visibility float64) {
	for i := range mask.Pix {
		mask.Pix[i] = 255 - mask.Pix[i]
	}
	if easedProgress >= 0.9 {
		factor := (1.0 - easedProgress) / 0.1
		for i := range mask.Pix {
			mask.Pix[i] = uint8(float64(mask.Pix[i]) * factor)
		}
	}
	if visibility < 1.0 {
		for i := range mask.Pix {
			mask.Pix[i] = uint8(float64(mask.Pix[i]) * visibility)
		}
	}
}
