package render

import (
	"image"
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

	for y := 0; y < targetH; y++ {
		srcY := yMin + (float64(y) * cropH / float64(targetH))
		srcYInt := int(math.Floor(srcY))
		if srcYInt >= src.Bounds().Max.Y {
			srcYInt = src.Bounds().Max.Y - 1
		}

		for x := 0; x < targetW; x++ {
			srcX := xMin + (float64(x) * cropW / float64(targetW))
			srcXInt := int(math.Floor(srcX))
			if srcXInt >= src.Bounds().Max.X {
				srcXInt = src.Bounds().Max.X - 1
			}

			// Nearest-neighbor sampling
			dst.Set(x, y, src.At(srcXInt, srcYInt))
		}
	}
	return dst
}
