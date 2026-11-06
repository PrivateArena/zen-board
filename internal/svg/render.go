package svg

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"sort"

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

// CacheKey hashes the rasterisation inputs. Variant pairs are sorted before
// hashing: iterating the map directly produced a different key per Go map
// iteration order, so identical rasters were re-rasterised (and re-registered
// under keys that could never be found again).
func CacheKey(rawXML []byte, variants Variant, w, h int) string {
	hasher := sha256.New()
	hasher.Write(rawXML)
	pairs := make([]string, 0, len(variants))
	for k, v := range variants {
		pairs = append(pairs, fmt.Sprintf("%s:%s", k, v))
	}
	sort.Strings(pairs)
	for _, p := range pairs {
		hasher.Write([]byte(p))
	}
	return fmt.Sprintf("%x_%d_%d", hasher.Sum(nil), w, h)
}
