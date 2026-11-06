package render

import (
	"image"
	"image/color"
	"image/draw"
	"testing"

	"zen-board/internal/constant"
	"zen-board/internal/model"
)

func TestOverlayLayerIgnoresTheCamera(t *testing.T) {
	inView := func(focus, preset string) bool { return focus == preset || preset == constant.FOCUS_RESET }
	board := model.FrameEvent{ZoomFocus: "TL", Layer: constant.LAYER_BOARD}
	mascot := model.FrameEvent{ZoomFocus: "br", Layer: constant.LAYER_OVERLAY}

	// The camera is looking at the opposite corner.
	if got := eventVisibility(board, 0, "BR", "BR", inView, 1); got != 0 {
		t.Errorf("board visibility = %v, want 0: a board layer must follow the camera", got)
	}
	if got := eventVisibility(mascot, 0, "BR", "BR", inView, 1); got != 1 {
		t.Errorf("overlay visibility = %v, want 1: the mascot disappeared because the camera moved", got)
	}
}

func TestExitRampFadesToNothing(t *testing.T) {
	ev := model.FrameEvent{
		Layer: constant.LAYER_OVERLAY, ExitFrames: 10, ExitKind: constant.EXIT_FADE,
		StartFrame: 100, EndFrame: 110,
	}
	if got := exitRamp(ev, 100, 1); got < 0.99 {
		t.Errorf("visibility at the first exit frame = %v, want ~1", got)
	}
	if got := exitRamp(ev, 110, 1); got > 0.01 {
		t.Errorf("visibility at the end of the exit window = %v, want ~0", got)
	}
	if middle := exitRamp(ev, 105, 1); middle <= 0 || middle >= 1 {
		t.Errorf("visibility mid-exit = %v, want a partial fade", middle)
	}
	plain := model.FrameEvent{Layer: constant.LAYER_OVERLAY, StartFrame: 0, EndFrame: 10}
	if got := exitRamp(plain, 9, 1); got != 1 {
		t.Errorf("a layer without exit frames ramped to %v", got)
	}
}

func TestBlitNativeKeepsPixelSize(t *testing.T) {
	eng := &Engine{Width: 200, Height: 100}
	raster := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for i := range raster.Pix {
		raster.Pix[i] = 255
	}
	buf := image.NewRGBA(image.Rect(0, 0, 200, 100))
	ev := model.FrameEvent{
		X: 10, Y: 10, Width: 180, Height: 80, FitMode: constant.FIT_NATIVE,
		StartFrame: 0, EndFrame: 1,
	}
	eng.blitNative(buf, ev, raster, 1)

	// Centred in the box: 10 + (180-40)/2 = 80, 10 + (80-20)/2 = 40.
	if got := buf.RGBAAt(80, 40).A; got == 0 {
		t.Error("the raster was not drawn at the expected centre offset")
	}
	if got := buf.RGBAAt(80+39, 40+19).A; got == 0 {
		t.Error("the raster did not cover its own pixel size")
	}
	if got := buf.RGBAAt(80+40, 40).A; got != 0 {
		t.Error("the raster was scaled beyond its pixel size")
	}
}

// A presenter layer is described in screen coordinates on purpose: it stays where it was
// placed and keeps its pixel size no matter where the camera looks. Mapping its box
// through the crop would make it a board layer with extra steps.
func TestScreenLayerKeepsItsPlaceUnderAZoom(t *testing.T) {
	eng := newScreenLayerEngine()
	place := func(cam CameraState) (int, int, bool) {
		buf := image.NewRGBA(image.Rect(0, 0, 40, 40))
		eng.drawScreenLayer(buf, []model.FrameEvent{{
			TargetImage: "sticker", EventType: constant.EVENT_OVERLAY, Layer: constant.LAYER_OVERLAY,
			X: 28, Y: 28, Width: 10, Height: 10, Opacity: 1, StartFrame: 0, EndFrame: 1,
		}}, 0)
		for y := 20; y < 40; y++ {
			for x := 20; x < 40; x++ {
				if buf.RGBAAt(x, y).R > 0 {
					return x, y, true
				}
			}
		}
		return -1, -1, false
	}

	zoomedX, zoomedY, drawnWhileZoomed := place(CameraState{X: 0, Y: 0, W: 20, H: 20})
	stillX, stillY, _ := place(CameraState{X: 0, Y: 0, W: 40, H: 40})
	if !drawnWhileZoomed {
		t.Fatal("the layer was not drawn at all while the camera was zoomed elsewhere")
	}
	if zoomedX != stillX || zoomedY != stillY {
		t.Errorf("the layer moved from %d,%d to %d,%d when the camera zoomed", stillX, stillY, zoomedX, zoomedY)
	}
}

// Compositing after the crop is what stops a zoom magnifying a caption off screen. The
// comparison is against the board layer, which is magnified by the very same crop.
func TestScreenLayerIsNotMagnifiedByTheCrop(t *testing.T) {
	eng := newScreenLayerEngine()

	// The same 8x8 layer, once drawn on the board and cropped, once composited after.
	board := image.NewRGBA(image.Rect(0, 0, 40, 40))
	red := eng.Assets["sticker"]
	draw.Draw(board, image.Rect(28, 28, 36, 36), red, image.Point{}, draw.Src)
	cropped := CropAndScale(board, CameraState{X: 14, Y: 14, W: 28, H: 28}, 56, 56, false)
	if span := redSpan(cropped); span <= 8 {
		t.Errorf("a board layer magnified by a 2x crop measured %d px; the fixture is wrong", span)
	}

	screen := image.NewRGBA(image.Rect(0, 0, 40, 40))
	eng.drawScreenLayer(screen, []model.FrameEvent{{
		TargetImage: "sticker", EventType: constant.EVENT_OVERLAY, Layer: constant.LAYER_OVERLAY,
		X: 28, Y: 28, Width: 8, Height: 8, Opacity: 1, StartFrame: 0, EndFrame: 1,
	}}, 0)
	if span := redSpan(screen); span > 9 {
		t.Errorf("the presenter layer measured %d px; it was magnified although it sits above the camera", span)
	}
}

// redSpan measures the widest run of the sticker's red in a frame.
func redSpan(img *image.RGBA) int {
	best, run := 0, 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		run = 0
		for x := 0; x < img.Bounds().Dx(); x++ {
			if img.RGBAAt(x, y).R > 200 {
				run++
				if run > best {
					best = run
				}
			} else {
				run = 0
			}
		}
	}
	return best
}

func newScreenLayerEngine() *Engine {
	eng := &Engine{
		Width: 40, Height: 40, FPS: 10,
		Assets:        map[string]image.Image{},
		ScaledAssets:  map[string]image.Image{},
		DefaultCursor: constant.CURSOR_HAND,
	}
	red := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range red.Pix {
		red.Pix[i] = 255
	}
	eng.RegisterAsset("sticker", red)
	return eng
}

func TestTextOutlineIsDrawn(t *testing.T) {
	// The fill colour is deliberately the same as the light background it is read on:
	// what makes a caption legible over artwork is the stroke.
	plain, err := RenderTextStyled(TextOptions{Text: "HI", FontPreset: constant.FONT_SANS, Size: 40, Colour: color.RGBA{255, 255, 255, 255}})
	if err != nil {
		t.Fatal(err)
	}
	stroked, err := RenderTextStyled(TextOptions{
		Text: "HI", FontPreset: constant.FONT_SANS, Size: 40,
		Colour: color.RGBA{255, 255, 255, 255}, OutlinePx: 4,
		OutlineColour: color.RGBA{255, 0, 0, 255},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stroked.Bounds().Dx() <= plain.Bounds().Dx() {
		t.Errorf("outline did not grow the raster: %d vs %d", stroked.Bounds().Dx(), plain.Bounds().Dx())
	}
	var redPixels int
	b := stroked.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x += 2 {
			r, g, bb, a := stroked.At(x, y).RGBA()
			if a>>8 > 200 && r>>8 > 200 && g>>8 < 60 && bb>>8 < 60 {
				redPixels++
			}
		}
	}
	if redPixels == 0 {
		t.Error("no outline colour was drawn")
	}
}

// An annotation addressed to a presenter layer must actually be drawn: it carries its
// geometry in TargetImage, not in an asset, so the raster path silently dropped it.
func TestScreenLayerDrawsAnnotations(t *testing.T) {
	eng := newScreenLayerEngine()
	dst := image.NewRGBA(image.Rect(0, 0, 40, 40))
	eng.drawScreenLayer(dst, []model.FrameEvent{{
		EventType: constant.EVENT_HIGHLIGHT, Layer: constant.LAYER_OVERLAY,
		TargetImage: "8,8,16,16", HighlightStyle: constant.HIGHLIGHT_RECT,
		StartFrame: 0, EndFrame: 1,
	}}, 0)
	var painted int
	for i := 0; i < len(dst.Pix); i += 4 {
		if dst.Pix[i]+dst.Pix[i+1]+dst.Pix[i+2] > 0 {
			painted++
		}
	}
	if painted == 0 {
		t.Error("a highlight on the presenter layer drew nothing")
	}
}
