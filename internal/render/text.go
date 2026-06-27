package render

import (
	"image"
	"image/color"

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

func RenderText(text string, fontPreset string, size float64, isBold bool, fgColor color.Color) (image.Image, error) {
	if text == "" {
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
	}

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
	bounds, _ := drawer.BoundString(text)

	xMin := bounds.Min.X.Floor()
	yMin := bounds.Min.Y.Floor()
	xMax := bounds.Max.X.Ceil()
	yMax := bounds.Max.Y.Ceil()

	width := xMax - xMin
	height := yMax - yMin

	pad := 10
	imgW := width + 2*pad
	imgH := height + 2*pad

	dst := image.NewRGBA(image.Rect(0, 0, imgW, imgH))

	drawer.Dst = dst
	drawer.Src = image.NewUniform(fgColor)
	drawer.Dot = fixed.Point26_6{
		X: fixed.I(pad - xMin),
		Y: fixed.I(pad - yMin),
	}

	drawer.DrawString(text)

	return dst, nil
}
