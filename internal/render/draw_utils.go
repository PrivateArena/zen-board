package render

import (
	"image"
	"image/color"
	"image/draw"
)

func DrawWithMask(dst draw.Image, r image.Rectangle, src image.Image, visibility float64) {
	if visibility >= 1.0 {
		draw.Draw(dst, r, src, image.Point{}, draw.Over)
	} else {
		maskUniform := image.NewUniform(color.Alpha{A: uint8(visibility * 255)})
		draw.DrawMask(dst, r, src, image.Point{}, maskUniform, image.Point{}, draw.Over)
	}
}
