package render

import (
	"image"
	"image/color"
	"math"
)


type CameraState struct {
	X, Y, W, H float64
}

func GetPresetViewport(preset string, canvasW, canvasH int) CameraState {
	fW := float64(canvasW)
	fH := float64(canvasH)
	switch preset {
	case "TL":
		return CameraState{0, 0, fW / 2, fH / 2}
	case "TR":
		return CameraState{fW / 2, 0, fW / 2, fH / 2}
	case "BL":
		return CameraState{0, fH / 2, fW / 2, fH / 2}
	case "BR":
		return CameraState{fW / 2, fH / 2, fW / 2, fH / 2}
	case "HT":
		return CameraState{0, 0, fW, fH / 2}
	case "HB":
		return CameraState{0, fH / 2, fW, fH / 2}
	case "LH":
		return CameraState{0, 0, fW / 2, fH}
	case "RH":
		return CameraState{fW / 2, 0, fW / 2, fH}
	default:
		return CameraState{0, 0, fW, fH}
	}
}

func LerpCamera(start, end CameraState, t float64) CameraState {
	// Smoothstep easing: t = t * t * (3 - 2 * t)
	t = t * t * (3 - 2*t)
	return CameraState{
		X: start.X + (end.X-start.X)*t,
		Y: start.Y + (end.Y-start.Y)*t,
		W: start.W + (end.W-start.W)*t,
		H: start.H + (end.H-start.H)*t,
	}
}

func CropAndScale(src *image.RGBA, cam CameraState, targetW, targetH int) *image.RGBA {
	epsilon := 0.5
	if math.Abs(cam.X) < epsilon && math.Abs(cam.Y) < epsilon &&
		math.Abs(cam.W-float64(targetW)) < epsilon && math.Abs(cam.H-float64(targetH)) < epsilon {
		return src
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))

	srcW := float64(src.Bounds().Dx())
	srcH := float64(src.Bounds().Dy())

	xMin := math.Max(0, math.Min(cam.X, srcW-1))
	yMin := math.Max(0, math.Min(cam.Y, srcH-1))
	cropW := math.Max(1, math.Min(cam.W, srcW-xMin))
	cropH := math.Max(1, math.Min(cam.H, srcH-yMin))

	maxX := src.Bounds().Max.X - 1
	maxY := src.Bounds().Max.Y - 1

	for y := 0; y < targetH; y++ {
		fy := yMin + float64(y)*cropH/float64(targetH)
		y0 := int(fy)
		y1 := y0 + 1
		if y1 > maxY {
			y1 = maxY
		}
		yt := fy - float64(y0)

		for x := 0; x < targetW; x++ {
			fx := xMin + float64(x)*cropW/float64(targetW)
			x0 := int(fx)
			x1 := x0 + 1
			if x1 > maxX {
				x1 = maxX
			}
			xt := fx - float64(x0)

			// Bilinear: sample 4 neighbours and blend
			c00 := src.RGBAAt(x0, y0)
			c10 := src.RGBAAt(x1, y0)
			c01 := src.RGBAAt(x0, y1)
			c11 := src.RGBAAt(x1, y1)

			r := (1-xt)*(1-yt)*float64(c00.R) + xt*(1-yt)*float64(c10.R) +
				(1-xt)*yt*float64(c01.R) + xt*yt*float64(c11.R)
			g := (1-xt)*(1-yt)*float64(c00.G) + xt*(1-yt)*float64(c10.G) +
				(1-xt)*yt*float64(c01.G) + xt*yt*float64(c11.G)
			b := (1-xt)*(1-yt)*float64(c00.B) + xt*(1-yt)*float64(c10.B) +
				(1-xt)*yt*float64(c01.B) + xt*yt*float64(c11.B)
			a := (1-xt)*(1-yt)*float64(c00.A) + xt*(1-yt)*float64(c10.A) +
				(1-xt)*yt*float64(c01.A) + xt*yt*float64(c11.A)

			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(a),
			})
		}
	}
	return dst
}

