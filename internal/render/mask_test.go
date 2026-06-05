package render

import (
	"testing"
)

func TestDiagonalGetFrontierPoint(t *testing.T) {
	width := 256
	height := 256
	config := DefaultMaskConfig()

	// Let's test progress from 0.0 to 1.0 at steps of 0.05
	for i := 0; i <= 100; i++ {
		progress := float64(i) / 100.0
		x, y := GetFrontierPoint(width, height, progress, "diagonal", config)

		// 1. Coordinates must always be within bounds [0, width-1] and [0, height-1]
		if x < 0 || x >= width || y < 0 || y >= height {
			t.Errorf("Progress %.2f: coordinates out of bounds: (%d, %d)", progress, x, y)
		}

		// 2. Print coordinates for visual/log validation of the trajectory
		if i%10 == 0 {
			t.Logf("Progress %.2f -> x: %d, y: %d", progress, x, y)
		}
	}

	// Verify starting point is at (0, 0)
	startX, startY := GetFrontierPoint(width, height, 0.0, "diagonal", config)
	if startX != 0 || startY != 0 {
		t.Errorf("Expected start at (0, 0), got (%d, %d)", startX, startY)
	}

	// Verify end point is at (width-1, height-1)
	endX, endY := GetFrontierPoint(width, height, 1.0, "diagonal", config)
	if endX != width-1 || endY != height-1 {
		t.Errorf("Expected end at (%d, %d), got (%d, %d)", width-1, height-1, endX, endY)
	}
}
