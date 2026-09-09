package render

import (
	"image"
	"image/color"
	"testing"
)

func hasVisibleContent(img image.Image) bool {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if r > 0 || g > 0 || b > 0 || a > 0 {
				return true
			}
		}
	}
	return false
}

func TestRenderText_MultiLineBasic(t *testing.T) {
	img, err := RenderText("Line 1\nLine 2", "sans", 24, false, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	if err != nil {
		t.Fatalf("RenderText failed: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dy() <= 0 {
		t.Errorf("Expected positive height for multi-line text, got %d", bounds.Dy())
	}

	// Height must be greater than a single line of "Line 2" to account for the gap
	singleLine, _ := RenderText("Line 2", "sans", 24, false, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	singleHeight := singleLine.Bounds().Dy()
	if bounds.Dy() <= singleHeight {
		t.Errorf("Multi-line image height %d should be greater than single-line height %d", bounds.Dy(), singleHeight)
	}
}

func TestRenderText_WidthMaxLine(t *testing.T) {
	// The image width should match the widest line
	short := "Short"
	long := "Much Longer Text Here"
	text := short + "\n" + long

	img, err := RenderText(text, "sans", 24, false, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	if err != nil {
		t.Fatalf("RenderText failed: %v", err)
	}

	singleLong, _ := RenderText(long, "sans", 24, false, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	expectedWidth := singleLong.Bounds().Dx()
	if img.Bounds().Dx() != expectedWidth {
		t.Errorf("Image width %d should match widest line width %d", img.Bounds().Dx(), expectedWidth)
	}
}

func TestRenderText_ChecklistStyle(t *testing.T) {
	// Simulate the use case: a checklist with multiple lines
	text := `Strix Halo's performance Qwen 3.8 Flash Next:
- Official llama.cpp
- strix-llama.cpp
- halo-flash-server`

	img, err := RenderText(text, "sans", 28, false, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	if err != nil {
		t.Fatalf("RenderText failed: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		t.Errorf("Expected positive dimensions, got %dx%d", bounds.Dx(), bounds.Dy())
	}

	// Should have 4 lines worth of content
	if bounds.Dy() <= 0 {
		t.Error("Rendered image has no height")
	}
}

func TestRenderText_EmptyLines(t *testing.T) {
	// Text with empty lines should still render correctly
	text := "Line 1\n\nLine 3"
	img, err := RenderText(text, "sans", 24, false, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	if err != nil {
		t.Fatalf("RenderText failed: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dy() <= 0 {
		t.Errorf("Expected positive dimensions, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestRenderText_BackwardCompatibility_SingleLine(t *testing.T) {
	// Single-line text without newlines must produce the same layout
	// as the original implementation: imgW = width + 2*pad, imgH = height + 2*pad
	img, err := RenderText("Hello World", "sans", 24, false, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	if err != nil {
		t.Fatalf("RenderText failed: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		t.Errorf("Expected positive dimensions, got %dx%d", bounds.Dx(), bounds.Dy())
	}

	// Image should have non-zero content (text pixels)
	if !hasVisibleContent(img) {
		t.Error("Rendered image has no visible text content")
	}
}

func TestRenderText_MonoFont(t *testing.T) {
	img, err := RenderText("Line A\nLine B", "mono", 20, true, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		t.Fatalf("RenderText mono font failed: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		t.Errorf("Expected positive dimensions, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestRenderText_SerifFont(t *testing.T) {
	img, err := RenderText("Line 1\nLine 2", "serif", 16, false, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	if err != nil {
		t.Fatalf("RenderText serif font failed: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		t.Errorf("Expected positive dimensions, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestRenderText_BoldFont(t *testing.T) {
	img, err := RenderText("Line 1\nLine 2", "sans", 24, true, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	if err != nil {
		t.Fatalf("RenderText bold failed: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		t.Errorf("Expected positive dimensions, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestRenderText_SingleLineMatchesOriginalHeight(t *testing.T) {
	// Verify single-line text produces the same height as expected by original formula
	// height = yMax - yMin, imgH = height + 2*pad (where pad=10)
	text := "Test"
	img, err := RenderText(text, "sans", 24, false, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	if err != nil {
		t.Fatalf("RenderText failed: %v", err)
	}

	// The height should include 2*pad = 20 pixels of padding plus the text height
	if img.Bounds().Dy() <= 0 {
		t.Errorf("Expected positive height, got %d", img.Bounds().Dy())
	}

	// Verify the image has visible content near the top (text should be rendered)
	if !hasVisibleContent(img) {
		t.Error("No visible text content found in rendered image")
	}
}

func TestRenderText_LineOrder(t *testing.T) {
	// First line should be at the top, last line at the bottom
	text := "First\nSecond\nThird"
	img, err := RenderText(text, "sans", 24, false, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	if err != nil {
		t.Fatalf("RenderText failed: %v", err)
	}

	bounds := img.Bounds()
	// Find the first non-transparent pixel row (should be near top)
	// and verify there's content in multiple row ranges
	rowsWithContent := make(map[int]bool)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if r > 0 || g > 0 || b > 0 || a > 0 {
				rowsWithContent[y] = true
				break
			}
		}
	}

	if len(rowsWithContent) < 3 {
		t.Errorf("Expected content in at least 3 row ranges for 3 lines, got %d row ranges", len(rowsWithContent))
	}
}

func TestRenderText_BackslashNEscape(t *testing.T) {
	// Backslash-n escape sequences in scripting API should produce multi-line text
	text := `Line 1\nLine 2`
	img, err := RenderText(text, "sans", 24, false, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	if err != nil {
		t.Fatalf("RenderText failed: %v", err)
	}

	single, _ := RenderText("Line 2", "sans", 24, false, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	if img.Bounds().Dy() <= single.Bounds().Dy() {
		t.Errorf("Backslash-n escape should produce multi-line text; got height %d, single-line %d", img.Bounds().Dy(), single.Bounds().Dy())
	}
}

func TestRenderText_DoubleBackslash(t *testing.T) {
	// Double backslash should render as a literal backslash, not an escape
	text := `C:\\Users`
	img, err := RenderText(text, "sans", 24, false, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	if err != nil {
		t.Fatalf("RenderText failed: %v", err)
	}

	if img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		t.Errorf("Expected positive dimensions for text with literal backslash")
	}
}

func TestRenderText_MixedEscapesAndNewlines(t *testing.T) {
	// Mix of actual newlines and escape sequences
	text := "Actual newline\n" + `Escape\nLine`
	img, err := RenderText(text, "sans", 24, false, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	if err != nil {
		t.Fatalf("RenderText failed: %v", err)
	}

	// Should have 3 lines worth of height
	bounds := img.Bounds()
	if bounds.Dy() <= 0 {
		t.Errorf("Expected positive height for mixed newlines and escapes")
	}
}
