package render

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
)

// maxCachedDerived limits the derived-asset cache so a long-running render (a
// counter with one raster per frame, for example) cannot grow without bound.
// Past the limit the value is still computed, simply not remembered.
const maxCachedDerived = 4096

// cachedDerived returns the memoised result for key, computing and storing it on
// a miss. It is the watermark.go idiom factored out: an expensive per-frame
// scale or text raster is a pure function of its inputs, so it belongs in
// ScaledAssets under AssetMu rather than in the hot render loop.
//
// Callers must pass a key that captures every input of gen, colour and alpha
// included; two different pixels must never share one key.
func (e *Engine) cachedDerived(key string, gen func() image.Image) image.Image {
	if img, ok := e.lookupDerived(key); ok {
		return img
	}
	img := gen()
	e.storeDerived(key, img)
	return img
}

func (e *Engine) lookupDerived(key string) (image.Image, bool) {
	e.AssetMu.RLock()
	img, ok := e.ScaledAssets[key]
	e.AssetMu.RUnlock()
	return img, ok
}

func (e *Engine) storeDerived(key string, img image.Image) {
	if e.disableDerivedCache {
		return
	}
	e.AssetMu.Lock()
	if len(e.ScaledAssets) < maxCachedDerived {
		e.ScaledAssets[key] = img
	}
	e.AssetMu.Unlock()
}

// scaleKey names a scaled raster uniquely for (asset, width, height) - the same
// scheme the draw path already uses inline.
func scaleKey(prefix, assetID string, w, h int) string {
	return fmt.Sprintf("%s:%s:%dx%d", prefix, assetID, w, h)
}

// textKey names a rendered text raster uniquely for its visible content and
// every styling input, alpha included. The content is hashed so the key stays
// short regardless of caption length.
func textKey(prefix, text, fontPreset string, size float64, bold bool, rgba uint32) string {
	sum := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%s:%s:%s:%v:%08x:%s", prefix, fontPreset, formatKeyFloat(size), bold, rgba, hex.EncodeToString(sum[:8]))
}

func formatKeyFloat(f float64) string { return fmt.Sprintf("%.3f", f) }

// rgbaKey packs a colour into the 32 bits a cache key needs. Alpha is part of
// the identity: a faded frame and an opaque frame are different pixels.
func rgbaKey(c color.RGBA) uint32 {
	return uint32(c.R)<<24 | uint32(c.G)<<16 | uint32(c.B)<<8 | uint32(c.A)
}
