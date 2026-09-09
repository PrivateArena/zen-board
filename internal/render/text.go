package render

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"
	"sync"

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

	primary, fallbacks, err := buildTextFaces(pickEmbeddedFont(fontPreset, isBold), size)
	if err != nil {
		return nil, err
	}
	defer primary.Close()
	for _, fb := range fallbacks {
		defer fb.Close()
	}

	// Split on '\n' and stack lines at the font's natural (ascent + descent)
	// pitch, so multi-line text renders as a block instead of a single run.
	lines := strings.Split(text, "\n")
	metrics := primary.Metrics()
	lineStep := metrics.Ascent + metrics.Descent
	lineStepPx := lineStep.Ceil()

	var maxW fixed.Int26_6
	for _, ln := range lines {
		if w := measureTextWidth(primary, fallbacks, ln); w > maxW {
			maxW = w
		}
	}

	pad := 10
	imgW := maxW.Ceil() + 2*pad
	imgH := lineStepPx*len(lines) + 2*pad
	dst := image.NewRGBA(image.Rect(0, 0, imgW, imgH))

	baseline := fixed.I(pad) + metrics.Ascent
	for _, ln := range lines {
		drawTextLine(dst, primary, fallbacks, fgColor, ln, fixed.Point26_6{X: fixed.I(pad), Y: baseline})
		baseline += lineStep
	}

	return dst, nil
}

// textFallbackMu guards the additionally registered fallback fonts.
// RenderText may be called concurrently from the worker pool, so the
// slice is read under a read lock on every call.
// slice is read under a read lock on every call.
var (
	textFallbackMu    sync.RWMutex
	textFallbackFonts []*opentype.Font
)

// SetTextFallbackFonts registers extra TTF/OTF files (given by filesystem
// path, never hardcoded) whose glyphs are consulted as a fallback whenever the
// embedded Go fonts lack a rune. The first registered face that covers a rune
// wins. Passing an empty slice clears all fallbacks and restores pristine
// embedded-font-only behavior. This is intended for symbol/emoji fonts; note
// that color-bitmap emoji fonts (e.g. NotoColorEmoji's CBDT strikes) contain
// no outline glyphs and therefore cannot be rasterized by x/image/font/opentype.
func SetTextFallbackFonts(paths []string) error {
	fonts := make([]*opentype.Font, 0, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("reading fallback font %s: %w", p, err)
		}
		f, err := opentype.Parse(b)
		if err != nil {
			return fmt.Errorf("parsing fallback font %s: %w", p, err)
		}
		fonts = append(fonts, f)
	}
	textFallbackMu.Lock()
	textFallbackFonts = fonts
	textFallbackMu.Unlock()
	return nil
}

func newTextFace(ttf []byte, size float64) (font.Face, error) {
	f, err := opentype.Parse(ttf)
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
	return face, nil
}

// buildTextFaces creates the primary face (from the embedded font) plus one
// face per registered fallback font, all at the requested size. Callers must
// Close every returned face.
func buildTextFaces(fontBytes []byte, size float64) (font.Face, []font.Face, error) {
	primary, err := newTextFace(fontBytes, size)
	if err != nil {
		return nil, nil, err
	}
	textFallbackMu.RLock()
	registered := textFallbackFonts
	textFallbackMu.RUnlock()
	fallbacks := make([]font.Face, 0, len(registered))
	for _, tf := range registered {
		fb, err := opentype.NewFace(tf, &opentype.FaceOptions{
			Size:    size,
			DPI:     72,
			Hinting: font.HintingFull,
		})
		if err != nil {
			continue
		}
		fallbacks = append(fallbacks, fb)
	}
	return primary, fallbacks, nil
}

// faceHasGlyph reports whether the face actually contains an outline for rune
// r. GlyphAdvance alone is unreliable: a face that lacks a glyph substitutes
// .notdef and still reports a nonzero advance, so GlyphBounds is used instead.
func faceHasGlyph(f font.Face, r rune) bool {
	_, _, ok := f.GlyphBounds(r)
	return ok
}

// faceForRune returns the first face (primary then fallbacks, in registration
// order) that covers r, falling back to the primary face when none do.
func faceForRune(primary font.Face, fallbacks []font.Face, r rune) font.Face {
	if faceHasGlyph(primary, r) {
		return primary
	}
	for _, fb := range fallbacks {
		if fb != nil && faceHasGlyph(fb, r) {
			return fb
		}
	}
	return primary
}

// measureTextWidth accumulates per-rune advances, honouring the same face
// selection used at draw time so measured width matches rendered width.
func measureTextWidth(primary font.Face, fallbacks []font.Face, s string) fixed.Int26_6 {
	var w fixed.Int26_6
	for _, r := range s {
		f := faceForRune(primary, fallbacks, r)
		a, _ := f.GlyphAdvance(r)
		w += a
	}
	return w
}

// drawTextLine draws s at the given baseline, splitting the runes into
// consecutive segments so that each rune uses the face that covers it.
func drawTextLine(dst *image.RGBA, primary font.Face, fallbacks []font.Face, col color.Color, s string, baseline fixed.Point26_6) {
	drawer := &font.Drawer{Dst: dst, Src: image.NewUniform(col)}
	runes := []rune(s)
	x := baseline.X
	for i := 0; i < len(runes); {
		f := faceForRune(primary, fallbacks, runes[i])
		j := i
		for j < len(runes) && faceForRune(primary, fallbacks, runes[j]) == f {
			j++
		}
		drawer.Face = f
		drawer.Dot = fixed.Point26_6{X: x, Y: baseline.Y}
		drawer.DrawString(string(runes[i:j]))
		for k := i; k < j; k++ {
			a, _ := f.GlyphAdvance(runes[k])
			x += a
		}
		i = j
	}
}
