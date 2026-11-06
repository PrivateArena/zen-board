package inspect

import (
	"image"
	"testing"
)

func TestContactSheetStaysLegible(t *testing.T) {
	// A small-canvas run must not produce a grid of slivers.
	tiles := []image.Image{
		image.NewRGBA(image.Rect(0, 0, 160, 90)),
		image.NewRGBA(image.Rect(0, 0, 160, 90)),
	}
	sheet := contactSheet(tiles, MaxSheetWidth)
	if got := sheet.Bounds().Dx(); got < 2*MinSheetTileWidth {
		t.Errorf("sheet is %d px wide: tiles below the legibility floor", got)
	}

	// A full-size run must not exceed the vision model's tile budget.
	big := make([]image.Image, 24)
	for i := range big {
		big[i] = image.NewRGBA(image.Rect(0, 0, 1080, 1920))
	}
	sheet = contactSheet(big, MaxSheetWidth)
	if got := sheet.Bounds().Dx(); got > MaxSheetWidth {
		t.Errorf("sheet is %d px wide, budget is %d", got, MaxSheetWidth)
	}
}
