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

// GenerateMask creates an alpha mask for a given progress (0.0 to 1.0) and style ("diagonal", "ltr", "ttb").
func GenerateMask(width, height int, progress float64, style string, config MaskConfig) *image.Alpha {
	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	
	fW := float64(width)
	fH := float64(height)

	if style == "ltr" {
		// Left-to-Right sweep
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
		// Diagonal TL→BR sweep: pixel is revealed when d = x/W + y/H < frontier (d ranges 0..2)
		// Sine wave runs along the perpendicular axis (x-y) for a hand-drawn edge.
		featherD := config.Feather * 2.0
		if featherD < 0.001 {
			featherD = 0.001
		}
		invFeather := 255.0 / featherD
		baseFrontier := 2.0 * progress

		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				// Perpendicular direction to diagonal: use (x-y) as the along-frontier parameter
				t := float64(x-y)
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
		// Default: "ttb" (Top-to-Bottom horizontal band sweep)
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

// GetFrontierPoint returns the (x, y) coordinates of the "reveal center" for hand tracking.
func GetFrontierPoint(width, height int, progress float64, style string, config MaskConfig) (int, int) {
	var x, y int

	if style == "ltr" {
		// Sweep left-to-right with writing oscillation
		x = int(progress * float64(width))
		y = height / 2
		
		// Add vertical drawing/writing oscillation (16 loops across width)
		sweep := float64(height) * 0.15 * math.Sin(2*math.Pi*progress*16.0)
		y += int(sweep)
	} else if style == "diagonal" {
		// Pencil tracks the TL→BR diagonal frontier with a zigzag perpendicular to it.
		// Base position: along the diagonal at current progress.
		bx := progress * float64(width)
		by := progress * float64(height)

		// Perpendicular direction to diagonal (1,1): normalized (W, -H) vector.
		diagLen := math.Sqrt(float64(width*width + height*height))
		perpX := float64(width) / diagLen
		perpY := -float64(height) / diagLen

		// Zigzag: 10 cycles across progress 0→1, amplitude = 18% of shorter dimension.
		short := float64(width)
		if height < width {
			short = float64(height)
		}
		amp := 0.18 * short
		osc := amp * math.Sin(2*math.Pi*progress*10.0)

		x = int(bx + osc*perpX)
		y = int(by + osc*perpY)
	} else {
		// Top-to-bottom band sweep
		y = int(progress * float64(height))
		
		// x sweeps left-to-right repeatedly to simulate drawing stroke lines
		strokeProgress := math.Mod(progress*4.0, 1.0)
		x = int(strokeProgress * float64(width))
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

