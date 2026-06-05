package render

import (
	"image"
	"image/color"
	"testing"
	"zen-board/internal/model"
)

func createTestEngine(b *testing.B) *Engine {
	// Try different relative paths depending on test execution context
	paths := []string{"../../assets/hand.png", "assets/hand.png", "../assets/hand.png"}
	var engine *Engine
	var err error
	for _, p := range paths {
		engine, err = NewEngine(1920, 1080, 60, p, 128, 128)
		if err == nil {
			break
		}
	}
	if err != nil {
		b.Fatalf("failed to create engine: %v", err)
	}

	// Register a dummy 800x600 test image asset
	dummyImg := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			dummyImg.SetRGBA(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	engine.RegisterAsset("test_img", dummyImg)
	return engine
}

func BenchmarkRenderFrameStatic(b *testing.B) {
	engine := createTestEngine(b)
	events := []model.FrameEvent{
		{
			EventType:   "static",
			TargetImage: "test_img",
			StartFrame:  0,
			EndFrame:    100,
			X:           100,
			Y:           100,
			Width:       800,
			Height:      600,
		},
	}
	cam := GetPresetViewport("reset", 1920, 1080)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rgba := engine.RenderFrame(50, events, cam, "whiteboard")
		engine.Pool.BufferPool.Put(rgba)
	}
}

func BenchmarkRenderFrameDraw(b *testing.B) {
	engine := createTestEngine(b)
	events := []model.FrameEvent{
		{
			EventType:   "draw",
			TargetImage: "test_img",
			StartFrame:  0,
			EndFrame:    100,
			X:           100,
			Y:           100,
			Width:       800,
			Height:      600,
			MaskStyle:   "diagonal",
		},
	}
	cam := GetPresetViewport("reset", 1920, 1080)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rgba := engine.RenderFrame(50, events, cam, "whiteboard")
		engine.Pool.BufferPool.Put(rgba)
	}
}

func BenchmarkRenderFrameErase(b *testing.B) {
	engine := createTestEngine(b)
	events := []model.FrameEvent{
		{
			EventType:   "erase",
			TargetImage: "test_img",
			StartFrame:  0,
			EndFrame:    100,
			X:           100,
			Y:           100,
			Width:       800,
			Height:      600,
			MaskStyle:   "diagonal",
		},
	}
	cam := GetPresetViewport("reset", 1920, 1080)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rgba := engine.RenderFrame(50, events, cam, "whiteboard")
		engine.Pool.BufferPool.Put(rgba)
	}
}

func BenchmarkRenderFrameMove(b *testing.B) {
	engine := createTestEngine(b)
	events := []model.FrameEvent{
		{
			EventType:   "move",
			TargetImage: "test_img",
			StartFrame:  0,
			EndFrame:    100,
			X:           100,
			Y:           100,
			DestX:       500,
			DestY:       400,
			Width:       800,
			Height:      600,
		},
	}
	cam := GetPresetViewport("reset", 1920, 1080)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rgba := engine.RenderFrame(50, events, cam, "whiteboard")
		engine.Pool.BufferPool.Put(rgba)
	}
}

func BenchmarkCropAndScaleBilinear(b *testing.B) {
	src := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	cam := CameraState{X: 100, Y: 100, W: 960, H: 540} // Zoom state

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst := CropAndScale(src, cam, 1920, 1080, false)
		_ = dst
	}
}

func BenchmarkCropAndScaleNearestNeighbor(b *testing.B) {
	src := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	cam := CameraState{X: 100, Y: 100, W: 960, H: 540} // Zoom state

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst := CropAndScale(src, cam, 1920, 1080, true)
		_ = dst
	}
}

func BenchmarkGenerateMask(b *testing.B) {
	cfg := DefaultMaskConfig()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mask := GenerateMask(800, 600, 0.5, "diagonal", cfg)
		_ = mask
	}
}
