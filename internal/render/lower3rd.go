package render

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	gobold "golang.org/x/image/font/gofont/gobold"
	goregular "golang.org/x/image/font/gofont/goregular"
)

var (
	Lower3rdFontBold    = gobold.TTF
	Lower3rdFontRegular = goregular.TTF
)

// RenderLower3rdPanel draws a lower-third panel on a canvas-sized RGBA image.
// title/subtitle/colorHex are encoded in TargetImage as "title|subtitle|#color".
func RenderLower3rdPanel(canvasW, canvasH int, title, subtitle string, colorHex string) *image.RGBA {
	panelH := int(float64(canvasH) * 0.13)
	if panelH < 80 {
		panelH = 80
	}
	if panelH > 160 {
		panelH = 160
	}
	panelW := int(float64(canvasW) * 0.85)
	panelX := (canvasW - panelW) / 2
	panelY := canvasH - panelH - int(float64(canvasH)*0.04)

	panel := image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))

	panelBg := color.RGBA{R: 0x1A, G: 0x1A, B: 0x1A, A: 0xCC}
	roundedRect(panel, panelX, panelY, panelW, panelH, 14, panelBg)

	accent := parseColorHex(colorHex)
	if accent.A == 0 {
		accent = color.RGBA{R: 0x00, G: 0x8F, B: 0xFF, A: 255}
	}
	roundedRect(panel, panelX, panelY, 7, panelH, 14, accent)

	titleFace := parseFontFace(Lower3rdFontBold, 50)
	defer titleFace.Close()
	subFace := parseFontFace(Lower3rdFontRegular, 31)
	defer subFace.Close()

	left := panelX + 31
	topY := panelY + panelH/2 - 24
	midY := panelY + panelH/2 + 22

	titleDrawer := &font.Drawer{Face: titleFace, Src: image.NewUniform(color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}), Dst: panel, Dot: fixed.P(left, topY)}
	titleDrawer.DrawString(truncateText(title, 64))

	subDrawer := &font.Drawer{Face: subFace, Src: image.NewUniform(color.RGBA{0xCC, 0xCC, 0xCC, 0xFF}), Dst: panel, Dot: fixed.P(left, midY)}
	subDrawer.DrawString(truncateText(subtitle, 90))

	return panel
}

func parseFontFace(data []byte, size float64) font.Face {
	f, err := opentype.Parse(data)
	if err != nil {
		return nil
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil
	}
	return face
}

func parseColorHex(s string) color.RGBA {
	s = strings.TrimPrefix(s, "#")
	s = strings.TrimSpace(s)
	if len(s) == 6 {
		var r, g, b uint8
		if _, err := fmt.Sscanf(s, "%02X%02X%02X", &r, &g, &b); err == nil {
			return color.RGBA{R: r, G: g, B: b, A: 255}
		}
	}
	return color.RGBA{}
}

func truncateText(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func roundedRect(dst *image.RGBA, x, y, w, h, r int, c color.RGBA) {
	for py := y; py < y+h; py++ {
		for px := x; px < x+w; px++ {
			if inRoundedRect(px, py, x, y, w, h, r) {
				dst.Set(px, py, c)
			}
		}
	}
}

func inRoundedRect(px, py, rx, ry, rw, rh, rr int) bool {
	if px < rx || px >= rx+rw || py < ry || py >= ry+rh {
		return false
	}
	cx, cy := rx+rr, ry+rr
	cx2, cy2 := rx+rw-rr, ry+rh-rr
	dx, dy := 0, 0
	if px < cx {
		dx = cx - px
	} else if px > cx2 {
		dx = px - cx2
	}
	if py < cy {
		dy = cy - py
	} else if py > cy2 {
		dy = py - cy2
	}
	return dx*dx+dy*dy <= rr*rr || (px >= cx && px <= cx2) || (py >= cy && py <= cy2)
}
