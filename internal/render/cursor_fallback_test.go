package render

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"zen-board/internal/constant"
	"zen-board/internal/model"
)

func testAssetsDir(t *testing.T) string {
	t.Helper()
	for _, d := range []string{"../../assets", "assets"} {
		if _, err := os.Stat(filepath.Join(d, "cursors", "hand", "config.json")); err == nil {
			abs, _ := filepath.Abs(d)
			return abs
		}
	}
	t.Skip("no shipped assets directory found")
	return ""
}

// TestCursorPresetFallback: an unknown cursor name used to mean the hand was
// never drawn at all, silently, for every frame of the video.
func TestCursorPresetFallback(t *testing.T) {
	dir := testAssetsDir(t)
	registry := NewCursorRegistry(filepath.Join(dir, "cursors"))
	if registry.Get(constant.CURSOR_HAND) == nil {
		t.Fatal("expected the shipped hand cursor preset to load")
	}
	e, err := NewEngine(64, 64, 30, registry, "hand")
	if err != nil {
		t.Fatal(err)
	}

	if p := e.cursorPresetFor(constant.CURSOR_HAND); p == nil {
		t.Error("hand preset must resolve")
	}
	if p := e.cursorPresetFor("magic_wand"); p == nil {
		t.Error("an unknown preset must fall back to the hand preset, not to nothing")
	}
	// Repeated lookups (i.e. every frame) must be served from the memo.
	for i := 0; i < 200; i++ {
		if e.cursorPresetFor("magic_wand") == nil {
			t.Fatal("fallback unstable")
		}
	}
	if _, seen := e.cursorCache["magic_wand"]; !seen {
		t.Error("lookup not memoised")
	}

	// A registry with no presets at all resolves to nil (the caller then uses the
	// hand sprite), and must not panic on the empty name.
	empty := NewCursorRegistry(t.TempDir())
	e2, _ := NewEngine(64, 64, 30, empty, "hand")
	if p := e2.cursorPresetFor("magic_wand"); p != nil {
		t.Error("expected nil when nothing is installed")
	}
	if p := e2.cursorPresetFor(""); p != nil {
		t.Error("expected nil for the empty cursor name")
	}
}

// TestPersistentLayerCachesAreByteExact is the W0 perf gate: the per-frame
// overlay/compare/counter caches must produce the same pixels, frame for frame,
// as computing them from scratch. The comparison is against a second engine
// whose derived cache is kept empty.
func TestPersistentLayerCachesAreByteExact(t *testing.T) {
	assets := map[string]image.Image{}
	for _, id := range []string{"a", "b"} {
		img := image.NewRGBA(image.Rect(0, 0, 64, 48))
		for y := 0; y < 48; y += 3 {
			img.SetRGBA(3, y, color.RGBA{R: 200, G: 40, B: 90, A: 255})
		}
		assets[id] = img
	}

	newTestEngine := func(useCache bool) *Engine {
		e, err := NewEngine(128, 96, 30, NewCursorRegistry(testAssetsDir(t)), "hand")
		if err != nil {
			t.Fatal(err)
		}
		for id, img := range assets {
			e.RegisterAsset(id, img)
		}
		if !useCache {
			// storeDerived refuses to insert, so every frame recomputes.
			e.disableDerivedCache = true
		}
		return e
	}

	events := []model.FrameEvent{
		{TargetImage: "a", EventType: constant.EVENT_OVERLAY, Opacity: 0.5, StartFrame: 0, EndFrame: constant.FRAME_FOREVER, ZoomFocus: constant.PRESET_FULLSCREEN},
		{TargetImage: "b", EventType: constant.EVENT_OVERLAY, Opacity: 0.25, StartFrame: 0, EndFrame: constant.FRAME_FOREVER, ZoomFocus: "TL"},
		{EventType: constant.EVENT_COMPARE, CompareLeft: "a", CompareRight: "b", LabelLeft: "Left", LabelRight: "Right", StartFrame: 0, EndFrame: 20},
		{EventType: constant.EVENT_COMPARE, CompareLeft: "a", CompareRight: "b", LabelLeft: "Left", LabelRight: "Right", StartFrame: 20, EndFrame: constant.FRAME_FOREVER},
		{EventType: constant.EVENT_COUNTER, CounterStart: 0, CounterEnd: 100, CounterFormat: "%d", ZoomFocus: "center", StartFrame: 0, EndFrame: 30},
		{EventType: constant.EVENT_COUNTER, CounterStart: 100, CounterEnd: 100, CounterFormat: "%d", ZoomFocus: "center", StartFrame: 30, EndFrame: constant.FRAME_FOREVER},
	}
	cam := GetPresetViewport("reset", 128, 96)

	cached, uncached := newTestEngine(true), newTestEngine(false)
	for f := 0; f < 40; f++ {
		a := cached.RenderFrame(f, events, cam, "whiteboard")
		b := uncached.RenderFrame(f, events, cam, "whiteboard")
		if len(a.Pix) != len(b.Pix) {
			t.Fatalf("frame %d: buffer size differs", f)
		}
		for i := range a.Pix {
			if a.Pix[i] != b.Pix[i] {
				t.Fatalf("frame %d byte %d differs: cached vs uncached", f, i)
			}
		}
		cached.Pool.BufferPool.Put(a)
		uncached.Pool.BufferPool.Put(b)
	}
	if n := len(uncached.ScaledAssets); n != 0 {
		t.Errorf("uncached engine stored %d derived assets", n)
	}
	if n := len(cached.ScaledAssets); n == 0 {
		t.Error("cached engine stored no derived assets - the caches are not engaged")
	}
	t.Logf("cached engine memoised %d derived rasters over 40 frames", len(cached.ScaledAssets))
}
