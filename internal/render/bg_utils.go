package render

import (
	"image"
	"image/color"
	"zen-board/internal/constant"
)

var styleBgs = map[string]image.Image{
	constant.STYLE_BLACKBOARD: image.NewUniform(color.RGBA{15, 15, 15, 255}),
	constant.STYLE_GLASSBOARD: image.NewUniform(color.RGBA{24, 28, 37, 255}),
}
var defaultStyleBg = image.NewUniform(color.White)

func ResolveStyleBg(style string) image.Image {
	if bg, ok := styleBgs[style]; ok {
		return bg
	}
	return defaultStyleBg
}

var styleTextColors = map[string]color.RGBA{
	constant.STYLE_BLACKBOARD: {R: 255, G: 255, B: 255, A: 255},
	constant.STYLE_GLASSBOARD: {R: 255, G: 255, B: 255, A: 255},
}
var defaultStyleTextColor = color.RGBA{R: 0, G: 0, B: 0, A: 255}

var styleBgColors = map[string]color.RGBA{
	constant.STYLE_BLACKBOARD: {R: 20, G: 20, B: 20, A: 220},
	constant.STYLE_GLASSBOARD: {R: 20, G: 20, B: 20, A: 220},
}
var defaultStyleBgColor = color.RGBA{R: 255, G: 255, B: 255, A: 220}

func ResolveStyleTextColor(style string) color.RGBA {
	if c, ok := styleTextColors[style]; ok {
		return c
	}
	return defaultStyleTextColor
}

func ResolveStyleBgColor(style string) color.RGBA {
	if c, ok := styleBgColors[style]; ok {
		return c
	}
	return defaultStyleBgColor
}

func ResolveStr(val, fallback string) string {
	if val != "" {
		return val
	}
	return fallback
}
