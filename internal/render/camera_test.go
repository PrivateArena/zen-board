package render

import (
	"image"
	"image/color"
	"testing"
)

func TestGetPresetViewport(t *testing.T) {
	width, height := 1920, 1080
	
	// Test TL (Top Left)
	tl := GetPresetViewport("TL", width, height)
	if tl.X != 0 || tl.Y != 0 || tl.W != 960 || tl.H != 540 {
		t.Errorf("Expected TL preset (0, 0, 960, 540), got %+v", tl)
	}

	// Test BR (Bottom Right)
	br := GetPresetViewport("BR", width, height)
	if br.X != 960 || br.Y != 540 || br.W != 960 || br.H != 540 {
		t.Errorf("Expected BR preset (960, 540, 960, 540), got %+v", br)
	}
}

func TestLerpCamera(t *testing.T) {
	start := CameraState{X: 0, Y: 0, W: 1920, H: 1080}
	end := CameraState{X: 100, Y: 200, W: 960, H: 540}
	
	// At t = 0.5, smoothstep (0.5 * 0.5 * (3 - 2 * 0.5) = 0.5) is exactly 0.5
	mid := LerpCamera(start, end, 0.5)
	if mid.X != 50 || mid.Y != 100 || mid.W != 1440 || mid.H != 810 {
		t.Errorf("Expected midpoint (50, 100, 1440, 810), got %+v", mid)
	}
}

func TestCropAndScale(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 100))
	src.Set(10, 10, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	
	// Zoom to first quadrant (0, 0, 50, 50)
	cam := CameraState{X: 0, Y: 0, W: 50, H: 50}
	dst := CropAndScale(src, cam, 100, 100, false)
	
	if dst.Bounds().Dx() != 100 || dst.Bounds().Dy() != 100 {
		t.Errorf("Expected output bounds 100x100, got %dx%d", dst.Bounds().Dx(), dst.Bounds().Dy())
	}
	
	// pixel at (10, 10) in original 50x50 area scales up to (20, 20) in 100x100
	c := dst.RGBAAt(20, 20)
	if c.R != 255 {
		t.Errorf("Expected pixel to be red, got %+v", c)
	}
}
