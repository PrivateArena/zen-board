package render

import "math"

func CalcProgress(frameNum, startFrame, endFrame int) float64 {
	if endFrame <= startFrame {
		return 1.0
	}
	p := float64(frameNum-startFrame) / float64(endFrame-startFrame)
	if p > 1.0 {
		p = 1.0
	}
	return p
}

func EaseInOut(t float64) float64 {
	return t * t * (3 - 2*t)
}

func EaseOutCubic(t float64) float64 {
	return 1.0 - math.Pow(1.0-t, 3.0)
}

func EaseInOutCubic(t float64) float64 {
	if t < 0.5 {
		return 4.0 * t * t * t
	}
	return 1.0 - math.Pow(-2.0*t+2.0, 3.0)/2.0
}
