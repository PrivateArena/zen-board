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
		Amplitude:  0.025, // slightly reduced for cleaner edge
		Wavelength: 60,    // shorter cycles = more hand-drawn texture
		Feather:    0.06,  // 3× wider than before — natural pencil edge
	}
}

// GenerateMask creates an alpha mask for a given progress (0.0 to 1.0) and style.
func GenerateMask(width, height int, progress float64, style string, config MaskConfig) *image.Alpha {
	mask := image.NewAlpha(image.Rect(0, 0, width, height))

	fW := float64(width)
	fH := float64(height)

	if style == "ltr" {
		featherPx := config.Feather * fW
		if featherPx < 1.0 {
			featherPx = 1.0
		}
		invFeather := 255.0 / featherPx
		bandX := progress*1.2*fW - 0.1*fW

		for y := 0; y < height; y++ {
			fY := float64(y)
			sineOffset := config.Amplitude * fW * math.Sin(2*math.Pi*fY/config.Wavelength)
			threshold := bandX + sineOffset
			for x := 0; x < width; x++ {
				fX := float64(x)
				if fX < threshold-featherPx {
					mask.SetAlpha(x, y, color.Alpha{A: 255})
				} else if fX < threshold {
					a := 255 - uint8(invFeather*(fX-(threshold-featherPx)))
					mask.SetAlpha(x, y, color.Alpha{A: a})
				}
			}
		}
	} else if style == "diagonal" {
		// Diagonal TL→BR: pixel revealed when d = x/W + y/H < frontier (d ranges 0..2).
		// Sine wave runs along the perpendicular axis (x-y) for a hand-drawn edge.
		featherD := config.Feather * 2.0
		if featherD < 0.001 {
			featherD = 0.001
		}
		invFeather := 255.0 / featherD
		baseFrontier := 2.0 * progress

		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				t := float64(x - y)
				sineOffset := config.Amplitude * 2.0 * math.Sin(2*math.Pi*t/config.Wavelength)
				thresh := baseFrontier + sineOffset
				d := float64(x)/fW + float64(y)/fH
				if d < thresh-featherD {
					mask.SetAlpha(x, y, color.Alpha{A: 255})
				} else if d < thresh {
					a := 255 - uint8(invFeather*(d-(thresh-featherD)))
					mask.SetAlpha(x, y, color.Alpha{A: a})
				}
			}
		}
	} else {
		// "ttb": Top-to-Bottom horizontal band sweep.
		featherPx := config.Feather * fH
		if featherPx < 1.0 {
			featherPx = 1.0
		}
		invFeather := 255.0 / featherPx
		bandY := progress*1.2*fH - 0.1*fH

		for y := 0; y < height; y++ {
			fY := float64(y)
			for x := 0; x < width; x++ {
				fX := float64(x)
				sineOffset := config.Amplitude * fH * math.Sin(2*math.Pi*fX/config.Wavelength)
				threshold := bandY + sineOffset
				if fY < threshold-featherPx {
					mask.SetAlpha(x, y, color.Alpha{A: 255})
				} else if fY < threshold {
					a := 255 - uint8(invFeather*(fY-(threshold-featherPx)))
					mask.SetAlpha(x, y, color.Alpha{A: a})
				}
			}
		}
	}

	return mask
}

// GetFrontierPoint returns (x, y) of the pencil tip — always ON the actual mask boundary.
func GetFrontierPoint(width, height int, progress float64, style string, config MaskConfig) (int, int) {
	var x, y int
	fW := float64(width)
	fH := float64(height)

	if style == "ltr" {
		// Hand tracks the vertical-center of the LTR sweep band with a gentle vertical oscillation.
		bandX := progress*1.2*fW - 0.1*fW
		x = int(math.Max(0, math.Min(fW-1, bandX)))
		// Gentle vertical sweep: 1 cycle across progress
		sweep := fH * 0.35 * math.Sin(2*math.Pi*progress*1.0)
		y = int(fH/2 + sweep)

	} else if style == "diagonal" {
		// Hand sits on the actual diagonal mask boundary by solving for y given oscillating x.
		// Frontier: x/W + y/H = 2*progress + Amplitude*2*sin(2π*(x-y)/Wavelength)
		//
		// Choose x oscillating across the image (1.5 cycles)
		numCycles := 1.5
		oscFrac := 0.5 + 0.4*math.Sin(2*math.Pi*progress*numCycles)
		hx := oscFrac * fW

		// First-order approximation: ignore sine for initial hy estimate.
		hy0 := (2*progress - hx/fW) * fH
		// Refine: compute sine offset at (hx, hy0) and solve.
		t := hx - hy0
		sineOffset := config.Amplitude * 2.0 * math.Sin(2*math.Pi*t/config.Wavelength)
		hy := (2*progress + sineOffset - hx/fW) * fH

		x = int(hx)
		y = int(hy)

	} else {
		// "ttb": hand tracks the actual bandY position with a gentle horizontal sweep.
		bandY := progress*1.2*fH - 0.1*fH
		y = int(math.Max(0, math.Min(fH-1, bandY)))
		// Gentle x sweep: 1 cycle, covers 60% of width
		sweep := fW * 0.30 * math.Sin(2*math.Pi*progress*1.0)
		x = int(fW/2 + sweep)
	}

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
