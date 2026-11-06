package render

import (
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var (
	embeddedRegular  = goregular.TTF
	embeddedBold     = gobold.TTF
	embeddedMono     = gomono.TTF
	embeddedMonoBold = gomonobold.TTF
)

func pickEmbeddedFont(fontPreset string, isBold bool) []byte {
	switch fontPreset {
	case "mono":
		if isBold {
			return embeddedMonoBold
		}
		return embeddedMono
	case "serif":
		if isBold {
			return embeddedBold
		}
		return embeddedRegular
	default: // sans
		if isBold {
			return embeddedBold
		}
		return embeddedRegular
	}
}

func unescapeTextEscapes(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				out.WriteByte('\n')
				i++
				continue
			case '\\':
				out.WriteByte('\\')
				i++
				continue
			}
		}
		out.WriteByte(s[i])
	}
	return out.String()
}

// RenderText rasterises text in a single ink colour.
func RenderText(text string, fontPreset string, size float64, isBold bool, fgColor color.Color) (image.Image, error) {
	return RenderTextStyled(TextOptions{
		Text: text, FontPreset: fontPreset, Size: size, Bold: isBold, Colour: fgColor,
	})
}

// TextOptions describes a text raster.
type TextOptions struct {
	Text       string
	FontPreset string
	Size       float64
	Bold       bool
	Colour     color.Color
	// OutlinePx strokes the glyphs by stamping the same glyph run at the 8 neighbour
	// offsets, which is what keeps a caption readable over arbitrary artwork.
	OutlinePx int
	// OutlineColour defaults to near-black ink, the readable choice on light art.
	OutlineColour color.Color
}

// RenderTextStyled rasterises text with an optional outline.
func RenderTextStyled(opts TextOptions) (image.Image, error) {
	fgColor := opts.Colour
	if fgColor == nil {
		fgColor = color.RGBA{A: 255}
	}
	text := opts.Text
	fontPreset := opts.FontPreset
	size := opts.Size
	isBold := opts.Bold
	if text == "" {
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
	}

	text = unescapeTextEscapes(text)

	fontBytes := pickEmbeddedFont(fontPreset, isBold)

	f, err := opentype.Parse(fontBytes)
	if err != nil {
		return nil, err
	}

	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	defer face.Close()

	drawer := &font.Drawer{
		Face: face,
	}

	lines := strings.Split(text, "\n")

	// Calculate bounds for each line
	boundsSlice := make([]fixed.Rectangle26_6, len(lines))
	maxLineWidth := 0
	for i, line := range lines {
		b, _ := drawer.BoundString(line)
		boundsSlice[i] = b
		lineWidth := b.Max.X.Ceil() - b.Min.X.Floor()
		if lineWidth > maxLineWidth {
			maxLineWidth = lineWidth
		}
	}

	pad := 10 + opts.OutlinePx
	// Calculate total dimensions with padding and line gaps
	imgW := maxLineWidth + 2*pad

	lineHeights := make([]int, len(lines))
	totalLineHeight := 0
	for i, b := range boundsSlice {
		h := b.Max.Y.Ceil() - b.Min.Y.Floor()
		lineHeights[i] = h
		totalLineHeight += h
	}

	lineGap := int(size * 0.3)
	if len(lines) > 1 {
		totalLineHeight += lineGap * (len(lines) - 1)
	}

	imgH := totalLineHeight + 2*pad

	dst := image.NewRGBA(image.Rect(0, 0, imgW, imgH))
	drawer.Dst = dst

	// Draw each line, maintaining the same baseline positioning as single-line.
	// The outline pass runs first so the fill lands on top of it.
	positions := make([]fixed.Point26_6, len(lines))
	yOffset := pad - boundsSlice[0].Min.Y.Floor()
	for i, b := range boundsSlice {
		positions[i] = fixed.Point26_6{X: fixed.I(pad - b.Min.X.Floor()), Y: fixed.I(yOffset)}
		if i < len(lines)-1 {
			yOffset += lineHeights[i] + lineGap
		}
	}

	drawPass := func(tint color.Color, dx, dy int) {
		drawer.Src = image.NewUniform(tint)
		for i, dot := range positions {
			drawer.Dot = fixed.Point26_6{X: dot.X + fixed.I(dx), Y: dot.Y + fixed.I(dy)}
			drawer.DrawString(lines[i])
		}
	}

	if opts.OutlinePx > 0 {
		ink := opts.OutlineColour
		if ink == nil {
			ink = color.RGBA{R: 16, G: 16, B: 16, A: 255}
		}
		for dy := -opts.OutlinePx; dy <= opts.OutlinePx; dy++ {
			for dx := -opts.OutlinePx; dx <= opts.OutlinePx; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}
				// A filled square brush: only the ring at the stroke radius is needed,
				// interior offsets would add nothing and cost a pass each.
				if dx*dx+dy*dy > opts.OutlinePx*opts.OutlinePx+1 {
					continue
				}
				drawPass(ink, dx, dy)
			}
		}
	}
	drawPass(fgColor, 0, 0)

	return dst, nil
}
