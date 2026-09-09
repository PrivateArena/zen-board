package render

import (
	"image/color"
	"os"
	"testing"
)

const (
	symbolFontPath = "/usr/share/fonts/truetype/noto/NotoSansSymbols2-Regular.ttf"
)

// TestRenderTextEmptyInput pins the empty-string contract: RenderText returns a
// 1x1 image rather than a zero-sized or panicking image.
func TestRenderTextEmptyInput(t *testing.T) {
	img, err := RenderText("", "sans", 48, true, color.Black)
	if err != nil {
		t.Fatalf("RenderText empty: %v", err)
	}
	if img.Bounds().Dx() != 1 || img.Bounds().Dy() != 1 {
		t.Fatalf("empty text should be a 1x1 image, got %v", img.Bounds())
	}
}

// TestRenderTextMultiLine verifies RenderText now splits on '\n': a multi-line
// string renders taller than a single-line one (previously '\n' was ignored and
// the following text was appended on the same baseline). This is the behavior
// the scripted text-asset pipeline (builder/timeline.go PrepareAssets) now
// benefits from automatically.
func TestRenderTextMultiLine(t *testing.T) {
	one, err := RenderText("single line", "sans", 48, true, color.Black)
	if err != nil {
		t.Fatalf("RenderText one line: %v", err)
	}
	multi, err := RenderText("Strix Halo's performance Qwen 3.8:\n- Official llama.cpp\n- strix-llama.cpp", "sans", 48, true, color.Black)
	if err != nil {
		t.Fatalf("RenderText multi line: %v", err)
	}

	ob := one.Bounds()
	mb := multi.Bounds()
	if mb.Dy() <= ob.Dy() {
		t.Fatalf("multi-line height (%d) must exceed single-line height (%d)", mb.Dy(), ob.Dy())
	}
	if mb.Dy() < ob.Dy()*2 {
		t.Fatalf("3-line height %d not ~3x single-line %d", mb.Dy(), ob.Dy())
	}
	if mb.Dx() <= 0 || mb.Dy() <= 0 {
		t.Fatalf("render bounds invalid: %v", mb)
	}
}

// TestRenderTextWidthMatchesWidestLine pins that multi-line output is as wide
// as its widest line, not the sum of all lines.
func TestRenderTextWidthMatchesWidestLine(t *testing.T) {
	single, err := RenderText("abcdefgh", "sans", 48, true, color.Black)
	if err != nil {
		t.Fatalf("RenderText widest line: %v", err)
	}
	multi, err := RenderText("ab\nabcdefgh", "sans", 48, true, color.Black)
	if err != nil {
		t.Fatalf("RenderText multi: %v", err)
	}
	if multi.Bounds().Dx() != single.Bounds().Dx() {
		t.Fatalf("block width (%d) should equal widest-line width (%d)", multi.Bounds().Dx(), single.Bounds().Dx())
	}
}

// TestRenderTextRendersNonEmptyPixels guards against degenerate all-transparent
// output for realistic multi-line content.
func TestRenderTextRendersNonEmptyPixels(t *testing.T) {
	img, err := RenderText("headline\n- item one\n- item two", "sans", 48, true, color.Black)
	if err != nil {
		t.Fatalf("RenderText: %v", err)
	}
	drawn := 0
	for i := 0; i < img.Bounds().Dy(); i++ {
		for j := 0; j < img.Bounds().Dx(); j++ {
			if _, _, _, a := img.At(j, i).RGBA(); a != 0 {
				drawn++
			}
		}
	}
	if drawn == 0 {
		t.Fatal("RenderText produced an all-transparent image")
	}
}

func TestSetTextFallbackFontsRejectsMissingFile(t *testing.T) {
	if err := SetTextFallbackFonts([]string{"/nonexistent/font.ttf"}); err == nil {
		t.Fatal("expected error registering a missing fallback font, got nil")
	}
}

// TestRenderTextGlyphFallback verifies glyph-fallback resolution end-to-end
// within RenderText: a registered symbol font must be selected for runes the
// embedded Go fonts lack, the embedded font must stay preferred for runes it
// covers, runes covered by neither must fall back to the primary, and the
// fallback-selected runes must actually be drawable. Skipped when the symbol
// font is absent on the host.
func TestRenderTextGlyphFallback(t *testing.T) {
	if _, err := os.Stat(symbolFontPath); err != nil {
		t.Skipf("symbol font not available: %v", err)
	}

	prev := textFallbackFonts
	defer func() {
		textFallbackMu.Lock()
		textFallbackFonts = prev
		textFallbackMu.Unlock()
	}()

	if err := SetTextFallbackFonts([]string{symbolFontPath}); err != nil {
		t.Fatalf("SetTextFallbackFonts: %v", err)
	}

	primary, fallbacks, err := buildTextFaces(pickEmbeddedFont("sans", true), 48)
	if err != nil {
		t.Fatalf("buildTextFaces: %v", err)
	}
	defer primary.Close()
	for _, fb := range fallbacks {
		defer fb.Close()
	}
	if len(fallbacks) == 0 {
		t.Fatal("expected at least one registered fallback face")
	}

	// 'A' is covered by the embedded Go font → primary must win.
	if got := faceForRune(primary, fallbacks, 'A'); got != primary {
		t.Fatal("rune covered by primary must resolve to primary, not a fallback")
	}
	// '✓' U+2713 is NOT covered by the Go fonts but IS in NotoSansSymbols2 → fallback wins.
	if got := faceForRune(primary, fallbacks, '\u2713'); got == primary {
		t.Fatal("rune missing from primary but present in fallback must resolve to the fallback face")
	}
	// '❌' U+274C is covered by neither → primary (which renders) must win.
	if got := faceForRune(primary, fallbacks, '\u274c'); got != primary {
		t.Fatal("rune covered by neither face must resolve to primary")
	}

	// End-to-end: fallback-selected glyphs must be drawable without degenerating
	// the image (guards the segmented DrawString path inside RenderText).
	img, err := RenderText("ok \u2713 nope \u274c", "sans", 48, true, color.Black)
	if err != nil {
		t.Fatalf("RenderText with symbols: %v", err)
	}
	if img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		t.Fatalf("symbol render had invalid bounds: %v", img.Bounds())
	}
}
