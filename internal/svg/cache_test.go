package svg

import "testing"

// TestCacheKeyStable asserts that 1000 CacheKey calls over the same 4-entry
// Variant collapse to exactly one key. Before the sort fix, Go's randomised map
// iteration order produced up to 4!/(number of distinct orders) keys per run.
func TestCacheKeyStable(t *testing.T) {
	raw := []byte(`<svg viewBox="0 0 24 24"><path d="M0 0h24v24H0z"/></svg>`)
	v := Variant{
		"fill":   "#EB4034",
		"stroke": "#000000",
		"accent": "#22AA66",
		"bg":     "none",
	}

	keys := make(map[string]int)
	for i := 0; i < 1000; i++ {
		keys[CacheKey(raw, v, 400, 300)]++
	}
	if len(keys) != 1 {
		t.Fatalf("CacheKey produced %d distinct keys for the same input, want 1", len(keys))
	}

	// Key must still be a function of every input.
	if CacheKey(raw, v, 400, 301) == CacheKey(raw, v, 400, 300) {
		t.Error("CacheKey ignores height")
	}
	v2 := Variant{"fill": "#EB4034", "stroke": "#000000", "accent": "#22AA66", "bg": "white"}
	if CacheKey(raw, v2, 400, 300) == CacheKey(raw, v, 400, 300) {
		t.Error("CacheKey ignores variant values")
	}
	// Same set of pairs, different insertion order => same key.
	v3 := Variant{"bg": "none", "accent": "#22AA66", "stroke": "#000000", "fill": "#EB4034"}
	if CacheKey(raw, v3, 400, 300) != CacheKey(raw, v, 400, 300) {
		t.Error("CacheKey depends on map insertion order")
	}
}
