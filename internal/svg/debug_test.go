package svg

import (
	"image/png"
	"os"
	"testing"
)

func TestRasterizeSVG(t *testing.T) {
	data, err := os.ReadFile("../../assets/shield_test.svg")
	if err != nil {
		t.Fatalf("Error reading SVG: %v", err)
	}

	cfg := RasterConfig{MaxDimension: 4096}
	img, err := RasterizeSVG(data, 400, 400, cfg)
	if err != nil {
		t.Fatalf("Error rasterizing SVG: %v", err)
	}
	t.Logf("Rasterized: %dx%d", img.Bounds().Dx(), img.Bounds().Dy())

	nonWhite := 0
	total := img.Bounds().Dx() * img.Bounds().Dy()
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if r > 0 || g > 0 || b > 0 || a > 0 {
				nonWhite++
			}
		}
	}

	t.Logf("Non-zero pixels: %d/%d (%.2f%%)", nonWhite, total, 100*float64(nonWhite)/float64(total))
	t.Logf("Total pixels: %d", total)

	if nonWhite > 0 {
		t.Logf("Found content!")
		f, err := os.Create("/tmp/shield_test_rasterized.png")
		if err == nil {
			png.Encode(f, img)
			f.Close()
			t.Log("Saved to /tmp/shield_test_rasterized.png")
		}
	} else {
		t.Errorf("Completely empty rasterized image!")
	}
}
