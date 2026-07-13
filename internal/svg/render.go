package svg

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

type RasterConfig struct {
	MaxDimension int
}

func RasterizeSVG(svgXML []byte, w, h int, cfg RasterConfig) (*image.RGBA, error) {
	if w > cfg.MaxDimension || h > cfg.MaxDimension {
		return nil, fmt.Errorf("requested layout size (%dx%d) exceeds maximum dimensions (%d)", w, h, cfg.MaxDimension)
	}
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("invalid layout coordinates (%dx%d)", w, h)
	}

	icon, err := oksvg.ReadIconStream(bytes.NewReader(svgXML), oksvg.WarnErrorMode)
	if err != nil {
		return nil, fmt.Errorf("parsing svg: %w", err)
	}

	icon.SetTarget(0, 0, float64(w), float64(h))
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, rgba, rgba.Bounds())
	dasher := rasterx.NewDasher(w, h, scanner)
	icon.Draw(dasher, 1.0)

	return rgba, nil
}

func CacheKey(rawXML []byte, variants Variant, w, h int) string {
	hasher := sha256.New()
	hasher.Write(rawXML)
	for k, v := range variants {
		hasher.Write([]byte(fmt.Sprintf("%s:%s", k, v)))
	}
	return fmt.Sprintf("%x_%d_%d", hasher.Sum(nil), w, h)
}