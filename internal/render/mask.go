package render

import (
	"image"
	"image/color"
	"math"
)

type MaskConfig struct {
	Amplitude  float64
	Wavelength float64
	Feather    float64
}

func DefaultMaskConfig() MaskConfig {
	return MaskConfig{
		Amplitude:  0.03,
		Wavelength: 80,
		Feather:    0.02,
	}
}

// GenerateMask creates an alpha mask for a given progress (0.0 to 1.0).
func GenerateMask(width, height int, progress float64, config MaskConfig) *image.Alpha {
	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	
	fW := float64(width)
	fH := float64(height)
	stepX := 1.0 / (2.0 * fW)
	invFeather := 255.0 / config.Feather

	for y := 0; y < height; y++ {
		fY := float64(y)
		// Sine-wave zigzag offset on the frontier
		zigzagOffset := config.Amplitude * math.Sin(2*math.Pi*fY/config.Wavelength)
		
		// Frontier moves from -0.1 to 1.1 to ensure full coverage at edges
		frontier := progress*1.2 - 0.1 + zigzagOffset
		posY := fY / (2.0 * fH)

		normalizedPos := posY
		for x := 0; x < width; x++ {
			if normalizedPos < frontier {
				mask.SetAlpha(x, y, color.Alpha{A: 255})
			} else if normalizedPos < frontier+config.Feather {
				a := 255 - uint8(invFeather*(normalizedPos-frontier))
				mask.SetAlpha(x, y, color.Alpha{A: a})
			} else {
				// Early break: remaining pixels in this row will be 0 alpha (pre-initialized)
				break
			}
			normalizedPos += stepX
		}
	}
	
	return mask
}

// GetFrontierPoint returns the (x, y) coordinates of the "reveal center" for hand tracking.
func GetFrontierPoint(width, height int, progress float64, config MaskConfig) (int, int) {
	// Align hand movement with diagonal reveal mask (Top-Left to Bottom-Right)
	x := int(float64(width) * progress)
	y := int(float64(height) * progress)
	
	// Add horizontal sketching oscillation
	sweep := float64(width) * 0.08 * math.Sin(2*math.Pi*progress*12.0)
	x += int(sweep)

	if x < 0 {
		x = 0
	}
	if x >= width {
		x = width - 1
	}
	if y < 0 {
		y = 0
	}
	if y >= height {
		y = height - 1
	}
	
	return x, y
}
