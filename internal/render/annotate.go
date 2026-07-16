package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strconv"
	"strings"
	"zen-board/internal/model"

	xdraw "golang.org/x/image/draw"
)

// parsePoint parses "x,y" or returns the center of a preset layout
func parsePoint(s string, canvasW, canvasH int) (int, int) {
	if strings.Contains(s, ",") {
		parts := strings.Split(s, ",")
		if len(parts) >= 2 {
			x, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
			y, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
			return x, y
		}
	}
	px, py, pw, ph := model.GetPresetLayout(s, canvasW, canvasH)
	return px + pw/2, py + ph/2
}

// parseRegion parses "x,y,w,h" or returns preset bounds
func parseRegion(s string, canvasW, canvasH int) (int, int, int, int) {
	if strings.Contains(s, ",") {
		parts := strings.Split(s, ",")
		if len(parts) >= 4 {
			x, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
			y, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
			w, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
			h, _ := strconv.Atoi(strings.TrimSpace(parts[3]))
			return x, y, w, h
		}
	}
	return model.GetPresetLayout(s, canvasW, canvasH)
}

func parseHexColor(hex string, def color.RGBA) color.RGBA {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return def
	}
	var r, g, b uint8
	_, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	if err != nil {
		return def
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

func drawBrushPoint(img *image.RGBA, cx, cy int, r int, col color.RGBA) {
	for y := cy - r - 1; y <= cy+r+1; y++ {
		for x := cx - r - 1; x <= cx+r+1; x++ {
			dx := float64(x) - float64(cx)
			dy := float64(y) - float64(cy)
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist <= float64(r) {
				img.Set(x, y, col)
			} else if dist < float64(r)+1.0 {
				alpha := (float64(r) + 1.0 - dist) * float64(col.A)
				c := col
				c.A = uint8(alpha)
				img.Set(x, y, c)
			}
		}
	}
}

func drawDashedLine(img *image.RGBA, x0, y0, x1, y1 int, dashLen, gapLen int, phase int, col color.RGBA) {
	dx := x1 - x0
	dy := y1 - y0
	dist := math.Sqrt(float64(dx*dx + dy*dy))
	if dist == 0 {
		return
	}
	stepX := float64(dx) / dist
	stepY := float64(dy) / dist

	for d := 0.0; d < dist; d += 1.0 {
		currPatternIdx := (int(d) + phase) % (dashLen + gapLen)
		if currPatternIdx < dashLen {
			px := int(float64(x0) + d*stepX)
			py := int(float64(y0) + d*stepY)
			drawBrushPoint(img, px, py, 2, col)
		}
	}
}

func fillTriangle(img *image.RGBA, x0, y0, x1, y1, x2, y2 float64, col color.RGBA) {
	minX := int(math.Min(x0, math.Min(x1, x2)))
	maxX := int(math.Max(x0, math.Max(x1, x2)))
	minY := int(math.Min(y0, math.Min(y1, y2)))
	maxY := int(math.Max(y0, math.Max(y1, y2)))

	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	if maxX >= img.Bounds().Dx() {
		maxX = img.Bounds().Dx() - 1
	}
	if maxY >= img.Bounds().Dy() {
		maxY = img.Bounds().Dy() - 1
	}

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			px, py := float64(x), float64(y)
			denom := (y1-y2)*(x0-x2) + (x2-x1)*(y0-y2)
			if math.Abs(denom) < 1e-6 {
				continue
			}
			w0 := ((y1-y2)*(px-x2) + (x2-x1)*(py-y2)) / denom
			w1 := ((y2-y0)*(px-x2) + (x0-x2)*(py-y2)) / denom
			w2 := 1.0 - w0 - w1

			if w0 >= 0 && w1 >= 0 && w2 >= 0 {
				img.Set(x, y, col)
			}
		}
	}
}

func drawBezierArrow(img *image.RGBA, p0, p1, p2 image.Point, progress float64, style string, col color.RGBA, strokeWidth int) (tipX, tipY int, angle float64) {
	dx01 := float64(p1.X - p0.X)
	dy01 := float64(p1.Y - p0.Y)
	dx12 := float64(p2.X - p1.X)
	dy12 := float64(p2.Y - p1.Y)
	L := math.Sqrt(dx01*dx01+dy01*dy01) + math.Sqrt(dx12*dx12+dy12*dy12)

	steps := int(L * 2)
	if steps < 10 {
		steps = 10
	}

	var currentX, currentY float64
	tStep := 1.0 / float64(steps)

	for i := 0; i <= int(progress*float64(steps)); i++ {
		t := float64(i) * tStep
		if t > 1.0 {
			t = 1.0
		}

		mt := 1.0 - t
		x := mt*mt*float64(p0.X) + 2*mt*t*float64(p1.X) + t*t*float64(p2.X)
		y := mt*mt*float64(p0.Y) + 2*mt*t*float64(p1.Y) + t*t*float64(p2.Y)

		currentX, currentY = x, y

		drawPoint := true
		if style == "dashed" {
			distIdx := int(float64(i) / float64(steps) * L)
			if (distIdx/12)%2 == 1 {
				drawPoint = false
			}
		}

		if drawPoint {
			drawBrushPoint(img, int(x), int(y), strokeWidth/2, col)
		}
	}

	t := progress
	mt := 1.0 - t
	tx := 2*mt*float64(p1.X-p0.X) + 2*t*float64(p2.X-p1.X)
	ty := 2*mt*float64(p1.Y-p0.Y) + 2*t*float64(p2.Y-p1.Y)
	angle = math.Atan2(ty, tx)

	return int(currentX), int(currentY), angle
}

func drawArrowhead(img *image.RGBA, tx, ty int, angle float64, col color.RGBA) {
	arrowSize := 24.0
	arrowAngle := math.Pi / 6.0

	w1x := float64(tx) - arrowSize*math.Cos(angle-arrowAngle)
	w1y := float64(ty) - arrowSize*math.Sin(angle-arrowAngle)
	w2x := float64(tx) - arrowSize*math.Cos(angle+arrowAngle)
	w2y := float64(ty) - arrowSize*math.Sin(angle+arrowAngle)

	fillTriangle(img, float64(tx), float64(ty), w1x, w1y, w2x, w2y, col)
}

func handleArrowEvent(e *Engine, frameNum int, ev model.FrameEvent, buf *image.RGBA, visibility float64) (int, int, bool) {
	p0X, p0Y := parsePoint(ev.ArrowFrom, buf.Bounds().Dx(), buf.Bounds().Dy())
	p2X, p2Y := parsePoint(ev.ArrowTo, buf.Bounds().Dx(), buf.Bounds().Dy())

	p0 := image.Point{X: p0X, Y: p0Y}
	p2 := image.Point{X: p2X, Y: p2Y}

	dx := float64(p2.X - p0.X)
	dy := float64(p2.Y - p0.Y)
	L := math.Sqrt(dx*dx + dy*dy)

	var p1 image.Point
	if ev.ArrowStyle == "curved" && L > 0 {
		midX := float64(p0.X+p2.X) / 2.0
		midY := float64(p0.Y+p2.Y) / 2.0
		offset := math.Min(80.0, L*0.25)
		p1.X = int(midX - (dy/L)*offset)
		p1.Y = int(midY + (dx/L)*offset)
	} else {
		p1.X = (p0.X + p2.X) / 2
		p1.Y = (p0.Y + p2.Y) / 2
	}

	progress := 1.0
	if ev.EventType == "arrow" {
		progress = CalcProgress(frameNum, ev.StartFrame, ev.EndFrame)
	}

	col := color.RGBA{R: 235, G: 64, B: 52, A: 255}
	if ev.ColorHex != "" {
		col = parseHexColor(ev.ColorHex, col)
	}
	col.A = uint8(float64(col.A) * visibility)

	strokeWidth := 6
	tipX, tipY, tipAngle := drawBezierArrow(buf, p0, p1, p2, progress, ev.ArrowStyle, col, strokeWidth)

	if progress > 0.05 {
		drawArrowhead(buf, tipX, tipY, tipAngle, col)
		if ev.ArrowStyle == "double" {
			startAngle := math.Atan2(float64(p0.Y-p1.Y), float64(p0.X-p1.X))
			drawArrowhead(buf, p0.X, p0.Y, startAngle, col)
		}
	}

	return tipX, tipY, true
}

func handleHighlightEvent(e *Engine, frameNum int, ev model.FrameEvent, buf *image.RGBA, visibility float64) {
	x, y, w, h := parseRegion(ev.TargetImage, buf.Bounds().Dx(), buf.Bounds().Dy())

	progress := CalcProgress(frameNum, ev.StartFrame, ev.EndFrame)

	col := color.RGBA{R: 255, G: 165, B: 0, A: 255}
	if ev.ColorHex != "" {
		col = parseHexColor(ev.ColorHex, col)
	}
	col = ApplyAlpha(col, visibility)

	style := ev.HighlightStyle
	if style == "" {
		style = "rect"
	}

	switch style {
	case "rect":
		phase := frameNum * 2
		drawDashedLine(buf, x, y, x+w, y, 12, 6, phase, col)
		drawDashedLine(buf, x+w, y, x+w, y+h, 12, 6, phase, col)
		drawDashedLine(buf, x+w, y+h, x, y+h, 12, 6, phase, col)
		drawDashedLine(buf, x, y+h, x, y, 12, 6, phase, col)

	case "circle":
		rx := float64(w) / 2.0
		ry := float64(h) / 2.0
		cx := float64(x) + rx
		cy := float64(y) + ry

		maxRad := math.Max(rx, ry)
		steps := int(maxRad * 4)
		if steps < 20 {
			steps = 20
		}

		for i := 0; i <= int(progress*float64(steps)); i++ {
			theta := 2.0 * math.Pi * float64(i) / float64(steps)
			px := cx + rx*math.Cos(theta)
			py := cy + ry*math.Sin(theta)
			drawBrushPoint(buf, int(px), int(py), 3, col)
		}

	case "pulse":
		scale := 1.0 + 0.08*math.Sin(2.0*math.Pi*float64(frameNum)/30.0)
		cx := float64(x) + float64(w)/2.0
		cy := float64(y) + float64(h)/2.0
		rx := (float64(w) / 2.0) * scale
		ry := (float64(h) / 2.0) * scale

		steps := int(math.Max(rx, ry) * 4)
		if steps < 20 {
			steps = 20
		}

		for i := 0; i <= steps; i++ {
			theta := 2.0 * math.Pi * float64(i) / float64(steps)
			px := cx + rx*math.Cos(theta)
			py := cy + ry*math.Sin(theta)
			drawBrushPoint(buf, int(px), int(py), 3, col)
		}
	}
}

func scaleImageFill(src image.Image, w, h int) image.Image {
	srcW := src.Bounds().Dx()
	srcH := src.Bounds().Dy()
	ratioSrc := float64(srcW) / float64(srcH)
	ratioTarget := float64(w) / float64(h)

	var drawW, drawH int
	if ratioSrc > ratioTarget {
		drawH = h
		drawW = int(float64(h) * ratioSrc)
	} else {
		drawW = w
		drawH = int(float64(w) / ratioSrc)
	}

	scaled := image.NewRGBA(image.Rect(0, 0, drawW, drawH))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), src, src.Bounds(), xdraw.Over, nil)

	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	startX := (drawW - w) / 2
	startY := (drawH - h) / 2
	draw.Draw(dst, dst.Bounds(), scaled, image.Point{X: startX, Y: startY}, draw.Src)
	return dst
}

func drawRoundedRect(img *image.RGBA, rect image.Rectangle, r int, col color.RGBA) {
	if col.A == 255 {
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				inCorner := false
				cx, cy := -1, -1
				if x < rect.Min.X+r && y < rect.Min.Y+r {
					cx, cy = rect.Min.X+r, rect.Min.Y+r
					inCorner = true
				} else if x >= rect.Max.X-r && y < rect.Min.Y+r {
					cx, cy = rect.Max.X-r, rect.Min.Y+r
					inCorner = true
				} else if x < rect.Min.X+r && y >= rect.Max.Y-r {
					cx, cy = rect.Min.X+r, rect.Max.Y-r
					inCorner = true
				} else if x >= rect.Max.X-r && y >= rect.Max.Y-r {
					cx, cy = rect.Max.X-r, rect.Max.Y-r
					inCorner = true
				}

				if inCorner {
					dx := float64(x - cx)
					dy := float64(y - cy)
					if dx*dx+dy*dy <= float64(r*r) {
						img.SetRGBA(x, y, col)
					}
				} else {
					img.SetRGBA(x, y, col)
				}
			}
		}
		return
	}

	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			inCorner := false
			cx, cy := -1, -1
			if x < rect.Min.X+r && y < rect.Min.Y+r {
				cx, cy = rect.Min.X+r, rect.Min.Y+r
				inCorner = true
			} else if x >= rect.Max.X-r && y < rect.Min.Y+r {
				cx, cy = rect.Max.X-r, rect.Min.Y+r
				inCorner = true
			} else if x < rect.Min.X+r && y >= rect.Max.Y-r {
				cx, cy = rect.Min.X+r, rect.Max.Y-r
				inCorner = true
			} else if x >= rect.Max.X-r && y >= rect.Max.Y-r {
				cx, cy = rect.Max.X-r, rect.Max.Y-r
				inCorner = true
			}

			drawPixel := func() {
				dstCol := img.RGBAAt(x, y)
				aSrc := float64(col.A) / 255.0
				aDst := float64(dstCol.A) / 255.0
				aOut := aSrc + aDst*(1.0-aSrc)

				var rOut, gOut, bOut float64
				if aOut > 0 {
					rOut = (float64(col.R)*aSrc + float64(dstCol.R)*aDst*(1.0-aSrc)) / aOut
					gOut = (float64(col.G)*aSrc + float64(dstCol.G)*aDst*(1.0-aSrc)) / aOut
					bOut = (float64(col.B)*aSrc + float64(dstCol.B)*aDst*(1.0-aSrc)) / aOut
				}
				img.SetRGBA(x, y, color.RGBA{
					R: uint8(rOut),
					G: uint8(gOut),
					B: uint8(bOut),
					A: uint8(aOut * 255.0),
				})
			}

			if inCorner {
				dx := float64(x - cx)
				dy := float64(y - cy)
				if dx*dx+dy*dy <= float64(r*r) {
					drawPixel()
				}
			} else {
				drawPixel()
			}
		}
	}
}

func handleCompareEvent(e *Engine, frameNum int, ev model.FrameEvent, buf *image.RGBA, visibility float64, style string) {
	e.AssetMu.RLock()
	leftImg, okL := e.Assets[ev.CompareLeft]
	rightImg, okR := e.Assets[ev.CompareRight]
	e.AssetMu.RUnlock()

	if !okL || !okR {
		return
	}

	canvasW := buf.Bounds().Dx()
	canvasH := buf.Bounds().Dy()

	halfW := canvasW / 2
	borderW := 4

	leftDest := image.Rect(0, 0, halfW, canvasH)
	rightDest := image.Rect(halfW, 0, canvasW, canvasH)

	scaledLeft := scaleImageFill(leftImg, halfW, canvasH)
	scaledRight := scaleImageFill(rightImg, halfW, canvasH)

	bg := ResolveStyleBg(style)

	if visibility >= 1.0 {
		draw.Draw(buf, leftDest, bg, image.Point{}, draw.Src)
		draw.Draw(buf, rightDest, bg, image.Point{}, draw.Src)
		DrawWithMask(buf, leftDest, scaledLeft, 1.0)
		DrawWithMask(buf, rightDest, scaledRight, 1.0)
	} else {
		mask := image.NewUniform(color.Alpha{A: uint8(visibility * 255)})
		draw.DrawMask(buf, leftDest, bg, image.Point{}, mask, image.Point{}, draw.Over)
		draw.DrawMask(buf, rightDest, bg, image.Point{}, mask, image.Point{}, draw.Over)
		DrawWithMask(buf, leftDest, scaledLeft, visibility)
		DrawWithMask(buf, rightDest, scaledRight, visibility)
	}

	colBorder := color.RGBA{R: 240, G: 240, B: 240, A: 255}
	if style == "blackboard" || style == "glassboard" {
		colBorder = color.RGBA{R: 40, G: 40, B: 40, A: 255}
	}
	colBorder.A = uint8(float64(colBorder.A) * visibility)
	draw.Draw(buf, image.Rect(halfW-borderW/2, 0, halfW+borderW/2, canvasH), image.NewUniform(colBorder), image.Point{}, draw.Over)

	if ev.LabelLeft != "" {
		drawCompareLabel(buf, ev.LabelLeft, 40, canvasH-80, true, style, visibility)
	}
	if ev.LabelRight != "" {
		drawCompareLabel(buf, ev.LabelRight, halfW+40, canvasH-80, false, style, visibility)
	}
}

func drawCompareLabel(buf *image.RGBA, text string, x, y int, isLeft bool, style string, visibility float64) {
	colText := ResolveStyleTextColor(style)
	colBg := ResolveStyleBgColor(style)
	colText = ApplyAlpha(colText, visibility)
	colBg = ApplyAlpha(colBg, visibility)

	txtImg, err := RenderText(text, "sans", 28, true, colText)
	if err != nil {
		return
	}

	tw := txtImg.Bounds().Dx()
	th := txtImg.Bounds().Dy()

	padX := 20
	padY := 10
	pillW := tw + padX*2
	pillH := th + padY*2

	pillRect := image.Rect(x, y-padY, x+pillW, y-padY+pillH)
	drawRoundedRect(buf, pillRect, pillH/2, colBg)

	draw.Draw(buf, image.Rect(x+padX, y, x+padX+tw, y+th), txtImg, image.Point{}, draw.Over)
}

func handleOverlayEvent(e *Engine, frameNum int, ev model.FrameEvent, buf *image.RGBA, visibility float64) {
	e.AssetMu.RLock()
	img, ok := e.Assets[ev.TargetImage]
	e.AssetMu.RUnlock()
	if !ok {
		return
	}

	canvasW := buf.Bounds().Dx()
	canvasH := buf.Bounds().Dy()

	var ox, oy, ow, oh int
	preset := ev.ZoomFocus
	if preset == "fullscreen" || preset == "" {
		ox, oy, ow, oh = 0, 0, canvasW, canvasH
	} else {
		ox, oy, ow, oh = model.GetPresetLayout(preset, canvasW, canvasH)
	}

	scaled := scaleImage(img, ow, oh)
	destRect := image.Rect(ox, oy, ox+ow, oy+oh)

	DrawWithMask(buf, destRect, scaled, ev.Opacity*visibility)
}

func handleTransitionEvent(e *Engine, frameNum int, ev model.FrameEvent, buf *image.RGBA, visibility float64) {
	progress := CalcProgress(frameNum, ev.StartFrame, ev.EndFrame)

	var opacity float64
	if progress < 0.5 {
		opacity = progress * 2.0
	} else {
		opacity = (1.0 - progress) * 2.0
	}

	col := color.RGBA{R: 0, G: 0, B: 0, A: uint8(opacity * 255 * visibility)}
	if ev.TransitionType == "flash" || ev.TransitionType == "fade-white" {
		col = color.RGBA{R: 255, G: 255, B: 255, A: uint8(opacity * 255 * visibility)}
	}

	draw.Draw(buf, buf.Bounds(), image.NewUniform(col), image.Point{}, draw.Over)
}

func handleCounterEvent(e *Engine, frameNum int, ev model.FrameEvent, buf *image.RGBA, visibility float64, style string) {
	progress := CalcProgress(frameNum, ev.StartFrame, ev.EndFrame)

	val := ev.CounterStart + (ev.CounterEnd-ev.CounterStart)*progress
	text := formatValue(val, ev.CounterFormat)

	colText := ResolveStyleTextColor(style)
	colText = ApplyAlpha(colText, visibility)

	canvasW := buf.Bounds().Dx()
	canvasH := buf.Bounds().Dy()
	cx, cy, cw, ch := model.GetPresetLayout(ev.ZoomFocus, canvasW, canvasH)

	txtImg, err := RenderText(text, "sans", 72, true, colText)
	if err != nil {
		return
	}

	tx := cx + (cw-txtImg.Bounds().Dx())/2
	ty := cy + (ch-txtImg.Bounds().Dy())/2
	destRect := image.Rect(tx, ty, tx+txtImg.Bounds().Dx(), ty+txtImg.Bounds().Dy())

	draw.Draw(buf, destRect, txtImg, image.Point{}, draw.Over)
}

func formatValue(val float64, format string) string {
	isFloat := strings.Contains(format, "f") || strings.Contains(format, "g")
	hasCommas := strings.Contains(format, ",")
	cleanFormat := strings.Replace(format, ",", "", -1)

	var formatted string
	if isFloat {
		formatted = fmt.Sprintf(cleanFormat, val)
	} else {
		formatted = fmt.Sprintf(cleanFormat, int64(val))
	}

	if hasCommas {
		formatted = addCommas(formatted)
	}
	return formatted
}

func addCommas(s string) string {
	var digits strings.Builder
	var prefix strings.Builder
	var suffix strings.Builder

	inDigits := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			inDigits = true
			digits.WriteRune(r)
		} else {
			if !inDigits {
				prefix.WriteRune(r)
			} else {
				suffix.WriteRune(r)
			}
		}
	}

	dStr := digits.String()
	var result []string
	for len(dStr) > 3 {
		result = append([]string{dStr[len(dStr)-3:]}, result...)
		dStr = dStr[:len(dStr)-3]
	}
	if len(dStr) > 0 {
		result = append([]string{dStr}, result...)
	}

	return prefix.String() + strings.Join(result, ",") + suffix.String()
}
