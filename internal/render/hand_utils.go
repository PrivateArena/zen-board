package render

import "math"

func ComputeHandAngle(dx, dy int) int {
	if dx == 0 && dy == 0 {
		return 0
	}
	angRad := math.Atan2(float64(dy), float64(dx))
	ang := int(angRad * 180 / math.Pi)
	if ang > 25 {
		ang = 25
	}
	if ang < -25 {
		ang = -25
	}
	return ang
}

func HandOffset(dx, dy, renderW, renderH int) (int, int) {
	handOffX, handOffY := 0, 0
	if dx > 0 {
		handOffX = renderW / 3
	} else if dx < 0 {
		handOffX = -renderW / 3
	}
	if dy > 0 {
		handOffY = renderH / 3
	} else if dy < 0 {
		handOffY = -renderH / 3
	}
	return handOffX, handOffY
}
