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

func RenderText(text string, fontPreset string, size float64, isBold bool, fgColor color.Color) (image.Image, error) {
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

	// Calculate total dimensions with padding and line gaps
	pad := 10
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
	drawer.Src = image.NewUniform(fgColor)

	// Draw each line, maintaining the same baseline positioning as single-line
	yOffset := pad - boundsSlice[0].Min.Y.Floor()
	for i, b := range boundsSlice {
		xOffset := pad - b.Min.X.Floor()

		drawer.Dot = fixed.Point26_6{
			X: fixed.I(xOffset),
			Y: fixed.I(yOffset),
		}
		drawer.DrawString(lines[i])

		if i < len(lines)-1 {
			yOffset += lineHeights[i] + lineGap
		}
	}

	return dst, nil
}
