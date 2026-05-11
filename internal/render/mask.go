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

	for y := 0; y < height; y++ {
		fY := float64(y)
		// Sine-wave zigzag offset on the frontier
		zigzagOffset := config.Amplitude * math.Sin(2*math.Pi*fY/config.Wavelength)
		
		// Frontier moves from -0.1 to 1.1 to ensure full coverage at edges
		frontier := progress*1.2 - 0.1 + zigzagOffset

		for x := 0; x < width; x++ {
			fX := float64(x)
			
			// Diagonal reveal: top-left (0,0) to bottom-right (W,H)
			// At (0,0), normalizedPos = 0
			// At (W,H), normalizedPos = 1
			normalizedPos := (fX/fW + fY/fH) / 2.0
			
			if normalizedPos < frontier {
				mask.SetAlpha(x, y, color.Alpha{A: 255})
			} else if normalizedPos < frontier+config.Feather {
				a := 255 - uint8(255*(normalizedPos-frontier)/config.Feather)
				mask.SetAlpha(x, y, color.Alpha{A: a})
			} else {
				mask.SetAlpha(x, y, color.Alpha{A: 0})
			}
		}
	}
	
	return mask
}

// GetFrontierPoint returns the (x, y) coordinates of the "reveal center" for hand tracking.
func GetFrontierPoint(width, height int, progress float64, config MaskConfig) (int, int) {
	// Simple approximation: midpoint of the frontier diagonal
	// When progress=0.5, we should be around center.
	
	p := progress*1.2 - 0.1
	// normalizedPos = (x/W + y/H) / 2 = p
	// x/W + y/H = 2p
	
	// Let's pick a point on the midline y = H/2
	// x/W + 0.5 = 2p => x/W = 2p - 0.5 => x = W * (2p - 0.5)
	
	x := int(float64(width) * (2*p - 0.5))
	y := height / 2
	
	// Add zigzag to hand too?
	zigzagOffset := config.Amplitude * math.Sin(2*math.Pi*float64(y)/config.Wavelength)
	x += int(zigzagOffset * float64(width))

	if x < 0 { x = 0 }
	if x >= width { x = width - 1 }
	
	return x, y
}
