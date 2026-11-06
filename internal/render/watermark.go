package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"zen-board/internal/model"
)

// watermarkPositions maps anchor strings to (xFrac, yFrac) — fraction of canvas
// width/height for the watermark's top-left corner.
var watermarkPositions = map[string][2]float64{
	"TL": {0.02, 0.02}, // top-left
	"TR": {0.72, 0.02}, // top-right
	"BL": {0.02, 0.88}, // bottom-left
	"BR": {0.72, 0.88}, // bottom-right
}

// handleWatermarkEvent renders a persistent watermark overlay.
// TargetImage format: "__watermark_<source>|<position>"
// <source> is either plain text or an image path (detected by .png/.jpg/.jpeg/.webp/.gif).
// <position> is one of TL, TR, BL, BR.
func handleWatermarkEvent(e *Engine, frameNum int, ev model.FrameEvent, buf *image.RGBA, cam CameraState) {
	rest := strings.TrimPrefix(ev.TargetImage, "__watermark_")
	parts := strings.SplitN(rest, "|", 2)
	source := parts[0]
	position := "TL"
	if len(parts) > 1 {
		position = strings.ToUpper(parts[1])
	}

	pos, ok := watermarkPositions[position]
	if !ok {
		pos = watermarkPositions["TL"]
	}

	watermarkKey := fmt.Sprintf("__watermark_asset_%s_%s", source, position)
	e.AssetMu.RLock()
	watermark, ok := e.Assets[watermarkKey]
	e.AssetMu.RUnlock()

	if !ok {
		watermark = buildWatermarkImage(source)
		if watermark != nil {
			e.AssetMu.Lock()
			e.Assets[watermarkKey] = watermark
			e.AssetMu.Unlock()
		}
	}

	if watermark == nil {
		return
	}

	watermarkW := watermark.Bounds().Dx()
	watermarkH := watermark.Bounds().Dy()

	// Clamp height to max 8% of canvas height
	maxH := int(float64(e.Height) * 0.08)
	if maxH < 40 {
		maxH = 40
	}
	if watermarkH > maxH {
		scale := float64(maxH) / float64(watermarkH)
		newW := int(float64(watermarkW) * scale)
		if newW <= 0 {
			newW = 1
		}
		scaledKey := fmt.Sprintf("__watermark_scaled_%s_%s_%d", source, position, maxH)
		e.AssetMu.RLock()
		scaled, ok := e.ScaledAssets[scaledKey]
		e.AssetMu.RUnlock()
		if !ok {
			scaled = scaleImage(watermark, newW, maxH)
			e.AssetMu.Lock()
			e.ScaledAssets[scaledKey] = scaled
			e.AssetMu.Unlock()
		}
		watermark = scaled
	}

	watermarkX := int(float64(e.Width) * pos[0])
	watermarkY := int(float64(e.Height) * pos[1])
	watermarkW = watermark.Bounds().Dx()
	watermarkH = watermark.Bounds().Dy()

	destRect := image.Rect(watermarkX, watermarkY, watermarkX+watermarkW, watermarkY+watermarkH)
	draw.Draw(buf, destRect, watermark, image.Point{}, draw.Over)
}

// buildWatermarkImage creates a watermark image from a source string.
// If source has an image extension it loads the file; otherwise renders text.
func buildWatermarkImage(source string) image.Image {
	if isImagePath(source) {
		img, err := loadImageFile(source)
		if err == nil && img != nil {
			return img
		}
	}
	return buildTextWatermark(source)
}

// isImagePath reports whether source looks like an image file path.
func isImagePath(source string) bool {
	lower := strings.ToLower(source)
	return strings.HasSuffix(lower, ".png") ||
		strings.HasSuffix(lower, ".jpg") ||
		strings.HasSuffix(lower, ".jpeg") ||
		strings.HasSuffix(lower, ".webp") ||
		strings.HasSuffix(lower, ".gif")
}

// loadImageFile loads an image from disk. Relative paths are opened as-is
// (the caller may resolve them against the assets directory beforehand).
func loadImageFile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return img, nil
}

// buildTextWatermark renders a text label as a semi-transparent pill badge.
func buildTextWatermark(source string) image.Image {
	source = strings.Trim(source, "\"")

	fontSize := 22.0
	paddingX := 16
	paddingY := 6

	face := parseFontFace(BannerFontBold, fontSize)
	if face == nil {
		return nil
	}
	defer face.Close()

	dummyDrawer := &font.Drawer{Face: face}
	bounds := dummyDrawer.MeasureString(source)
	textW := (bounds.Floor() + 63) / 64
	textH := face.Metrics().Height.Floor()

	imgW := textW + 2*paddingX
	imgH := textH + 2*paddingY
	if imgW <= 0 {
		imgW = 80
	}
	if imgH <= 0 {
		imgH = 40
	}

	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))

	// Semi-transparent dark background
	bg := color.RGBA{R: 0x1A, G: 0x1A, B: 0x1A, A: 0xCC}
	roundedRect(img, 0, 0, imgW, imgH, 8, bg)

	// Left accent stripe
	accent := color.RGBA{R: 0x00, G: 0x8F, B: 0xFF, A: 200}
	roundedRect(img, 0, 0, 4, imgH, 4, accent)

	// White text
	textColor := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 230}
	drawer := &font.Drawer{
		Face: face,
		Src:  image.NewUniform(textColor),
		Dst:  img,
		Dot:  fixed.P(paddingX+2, paddingY+face.Metrics().Ascent.Floor()),
	}
	drawer.DrawString(source)

	return img
}
