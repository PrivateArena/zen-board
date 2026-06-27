package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "image/jpeg"
	_ "image/png"

	"zen-board/internal/model"

	xdraw "golang.org/x/image/draw"
)

type RenderStats struct {
	TotalFrames     int64
	ClearBgTime     int64 // in nanoseconds
	InvertColorTime int64
	ScaleAssetTime  int64
	MaskGenTime     int64
	FrontierTime    int64
	DrawMaskTime    int64
	DrawHandTime    int64
	CropScaleTime   int64
	TotalRenderTime int64
}

type Engine struct {
	Width, Height int
	FPS           int
	Pool          *RenderPool
	Hand          *HandRenderer
	Assets        map[string]image.Image
	ScaledAssets  map[string]image.Image
	AssetMu       sync.RWMutex
	Stats         RenderStats
	FastMode      bool
}

func NewEngine(w, h, fps int, handPath string, tipX, tipY int) (*Engine, error) {
	hr, err := NewHandRenderer(handPath, tipX, tipY)
	if err != nil {
		return nil, err
	}

	return &Engine{
		Width:        w,
		Height:       h,
		FPS:          fps,
		Pool:         NewRenderPool(w, h),
		Hand:         hr,
		Assets:       make(map[string]image.Image),
		ScaledAssets: make(map[string]image.Image),
	}, nil
}

func (e *Engine) StartWorkers() {
	for i := 0; i < e.Pool.Workers; i++ {
		go func() {
			for job := range e.Pool.Jobs {
				frame := e.RenderFrame(job.Index, job.Events, job.Cam, job.Style)
				e.Pool.Results <- RenderResult{
					Index: job.Index,
					Frame: frame,
				}
			}
		}()
	}
}

func (e *Engine) PrintStats() {
	frames := atomic.LoadInt64(&e.Stats.TotalFrames)
	if frames == 0 {
		return
	}
	fmt.Printf("\n=== Render Timing Stats (Total Frames: %d) ===\n", frames)
	printStat := func(label string, ns int64) {
		dur := time.Duration(ns)
		avg := time.Duration(ns / frames)
		pct := 0.0
		totalNs := atomic.LoadInt64(&e.Stats.TotalRenderTime)
		if totalNs > 0 {
			pct = float64(ns) * 100.0 / float64(totalNs)
		}
		fmt.Printf("- %-18s: %10s (avg %8s/frame, %5.1f%%)\n", label, dur, avg, pct)
	}
	printStat("Clear Bg", atomic.LoadInt64(&e.Stats.ClearBgTime))
	printStat("Invert Color", atomic.LoadInt64(&e.Stats.InvertColorTime))
	printStat("Scale Asset", atomic.LoadInt64(&e.Stats.ScaleAssetTime))
	printStat("Mask Generation", atomic.LoadInt64(&e.Stats.MaskGenTime))
	printStat("Frontier Point", atomic.LoadInt64(&e.Stats.FrontierTime))
	printStat("Draw Mask/Img", atomic.LoadInt64(&e.Stats.DrawMaskTime))
	printStat("Draw Hand", atomic.LoadInt64(&e.Stats.DrawHandTime))
	printStat("Crop & Scale", atomic.LoadInt64(&e.Stats.CropScaleTime))
	printStat("Total Render", atomic.LoadInt64(&e.Stats.TotalRenderTime))
}

func (e *Engine) LoadAsset(name, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}

	e.AssetMu.Lock()
	e.Assets[name] = img
	e.AssetMu.Unlock()
	return nil
}

func (e *Engine) RegisterAsset(name string, img image.Image) {
	e.AssetMu.Lock()
	e.Assets[name] = img
	e.AssetMu.Unlock()
}

// RenderFrame generates a single frame based on the active events.
func (e *Engine) RenderFrame(frameNum int, events []model.FrameEvent, cam CameraState, style string) *image.RGBA {
	t0 := time.Now()
	buf := e.Pool.BufferPool.Get().(*image.RGBA)

	tBgStart := time.Now()
	var bg image.Image
	switch style {
	case "blackboard":
		bg = image.NewUniform(color.RGBA{15, 15, 15, 255})
	case "glassboard":
		bg = image.NewUniform(color.RGBA{24, 28, 37, 255})
	default:
		bg = image.NewUniform(image.White)
	}
	draw.Draw(buf, buf.Bounds(), bg, image.Point{}, draw.Src)
	tClearBg := time.Since(tBgStart)

	var activeHandX, activeHandY int
	var handVisible bool
	var activeHandStyle string = "default"
	var activeHandAngle int = 0

	maskCfg := DefaultMaskConfig()

	var localInvertColorTime time.Duration
	var localScaleAssetTime time.Duration
	var localMaskGenTime time.Duration
	var localFrontierTime time.Duration
	var localDrawMaskTime time.Duration
	var localDrawHandTime time.Duration
	var localCropScaleTime time.Duration

	slideAnimFrames := int(0.18 * float64(e.FPS))

	for _, ev := range events {
		if frameNum < ev.StartFrame || frameNum > ev.EndFrame {
			continue
		}

		evFocus := ev.ZoomFocus
		if evFocus == "" {
			evFocus = "reset"
		}

		isVisibleIn := func(focus, preset string) bool {
			return focus == preset || preset == "reset"
		}

		srcPreset := cam.SourcePreset
		if srcPreset == "" {
			srcPreset = "reset"
		}
		tgtPreset := cam.TargetPreset
		if tgtPreset == "" {
			tgtPreset = "reset"
		}

		var visibility float64 = 1.0
		srcVis := isVisibleIn(evFocus, srcPreset)
		tgtVis := isVisibleIn(evFocus, tgtPreset)

		if srcVis && tgtVis {
			visibility = 1.0
		} else if srcVis && !tgtVis {
			visibility = 1.0 - cam.TransitionT
		} else if !srcVis && tgtVis {
			visibility = cam.TransitionT
		} else {
			visibility = 0.0
		}

		if visibility <= 0.0 {
			continue
		}

		if ev.EventType == "slide" {
			handleSlideEvent(e, frameNum, ev, buf, visibility, slideAnimFrames, cam)
			continue
		}

		if ev.EventType == "lower3rd" {
			handleLower3rdEvent(e, frameNum, ev, buf, cam)
			continue
		}

		e.AssetMu.RLock()
		img, ok := e.Assets[ev.TargetImage]
		e.AssetMu.RUnlock()
		if !ok {
			continue
		}

		if style == "blackboard" && !strings.HasPrefix(ev.TargetImage, "__text_") {
			key := ev.TargetImage + "_inverted"
			e.AssetMu.RLock()
			invImg, ok := e.Assets[key]
			e.AssetMu.RUnlock()
			if ok {
				img = invImg
			} else {
				tInvertStart := time.Now()
				invImg = invertImageColors(img)
				localInvertColorTime += time.Since(tInvertStart)
				e.AssetMu.Lock()
				e.Assets[key] = invImg
				e.AssetMu.Unlock()
				img = invImg
			}
		}

		var renderW, renderH int
		var renderX, renderY int

		if ev.Width > 0 && ev.Height > 0 {
			srcW := img.Bounds().Dx()
			srcH := img.Bounds().Dy()
			ratioSrc := float64(srcW) / float64(srcH)
			ratioTarget := float64(ev.Width) / float64(ev.Height)

			if ratioSrc > ratioTarget {
				renderW = ev.Width
				renderH = int(float64(ev.Width) / ratioSrc)
				if renderH <= 0 {
					renderH = 1
				}
			} else {
				renderH = ev.Height
				renderW = int(float64(ev.Height) * ratioSrc)
				if renderW <= 0 {
					renderW = 1
				}
			}

			renderX = ev.X + (ev.Width-renderW)/2
			renderY = ev.Y + (ev.Height-renderH)/2

			key := fmt.Sprintf("%s_%d_%d", ev.TargetImage, renderW, renderH)
			e.AssetMu.RLock()
			scaledImg, ok := e.ScaledAssets[key]
			e.AssetMu.RUnlock()

			if ok {
				img = scaledImg
			} else {
				tScaleStart := time.Now()
				scaledImg = scaleImage(img, renderW, renderH)
				localScaleAssetTime += time.Since(tScaleStart)
				e.AssetMu.Lock()
				e.ScaledAssets[key] = scaledImg
				e.AssetMu.Unlock()
				img = scaledImg
			}
		} else {
			renderW = img.Bounds().Dx()
			renderH = img.Bounds().Dy()
			renderX = ev.X
			renderY = ev.Y
		}

		var progress float64
		if ev.EndFrame <= ev.StartFrame {
			progress = 1.0
		} else {
			progress = float64(frameNum-ev.StartFrame) / float64(ev.EndFrame-ev.StartFrame)
			if progress > 1.0 {
				progress = 1.0
			}
		}
		easedProgress := progress * progress * (3 - 2*progress)

		destRect := image.Rect(renderX, renderY, renderX+img.Bounds().Dx(), renderY+img.Bounds().Dy())

		if ev.EventType == "static" {
			tDrawStart := time.Now()
			if visibility >= 1.0 {
				draw.Draw(buf, destRect, img, image.Point{}, draw.Over)
			} else {
				maskUniform := image.NewUniform(color.Alpha{A: uint8(visibility * 255)})
				draw.DrawMask(buf, destRect, img, image.Point{}, maskUniform, image.Point{}, draw.Over)
			}
			localDrawMaskTime += time.Since(tDrawStart)
			continue
		}

		if ev.EventType == "move" {
			rawT := float64(frameNum-ev.StartFrame) / float64(ev.EndFrame-ev.StartFrame)
			if rawT > 1.0 {
				rawT = 1.0
			}
			easedT := rawT * rawT * (3 - 2*rawT)
			curX := ev.X + int(float64(ev.DestX-ev.X)*easedT)
			curY := ev.Y + int(float64(ev.DestY-ev.Y)*easedT)
			destRect = image.Rect(curX, curY, curX+renderW, curY+renderH)
			tDrawStart := time.Now()
			if visibility >= 1.0 {
				draw.Draw(buf, destRect, img, image.Point{}, draw.Over)
			} else {
				maskUniform := image.NewUniform(color.Alpha{A: uint8(visibility * 255)})
				draw.DrawMask(buf, destRect, img, image.Point{}, maskUniform, image.Point{}, draw.Over)
			}
			localDrawMaskTime += time.Since(tDrawStart)

			dx := ev.DestX - ev.X
			dy := ev.DestY - ev.Y
			handOffX, handOffY := 0, 0
			if dx > 0 {
				handOffX = renderW / 3
			} else if dx < 0 {
				handOffX = -renderW / 3
			}
			if dy > 0 {
				handOffY = renderH / 3
			} else if dy < 0 {
				handOffY = -renderH / 3
			}
			activeHandX = curX + renderW/2 + handOffX
			activeHandY = curY + renderH/2 + handOffY
			if dx != 0 || dy != 0 {
				angRad := math.Atan2(float64(dy), float64(dx))
				ang := int(angRad * 180 / math.Pi)
				if ang > 25 {
					ang = 25
				}
				if ang < -25 {
					ang = -25
				}
				activeHandAngle = ang
			}
			handVisible = true
			if ev.HandStyle != "" {
				activeHandStyle = ev.HandStyle
			} else {
				activeHandStyle = "default"
			}
			continue
		}

		if ev.EventType == "erase" {
			if easedProgress >= 1.0 {
				continue
			}
			tMaskStart := time.Now()
			mask := GenerateMask(img.Bounds().Dx(), img.Bounds().Dy(), easedProgress, ev.MaskStyle, maskCfg)
			localMaskGenTime += time.Since(tMaskStart)

			tDrawStart := time.Now()
			for i := range mask.Pix {
				mask.Pix[i] = 255 - mask.Pix[i]
			}
			if easedProgress >= 0.9 {
				factor := (1.0 - easedProgress) / 0.1
				for i := range mask.Pix {
					mask.Pix[i] = uint8(float64(mask.Pix[i]) * factor)
				}
			}
			if visibility < 1.0 {
				for i := range mask.Pix {
					mask.Pix[i] = uint8(float64(mask.Pix[i]) * visibility)
				}
			}
			draw.DrawMask(buf, destRect, img, image.Point{}, mask, image.Point{}, draw.Over)
			localDrawMaskTime += time.Since(tDrawStart)

			tFrontierStart := time.Now()
			fx, fy := GetFrontierPoint(img.Bounds().Dx(), img.Bounds().Dy(), easedProgress, ev.MaskStyle, maskCfg)
			localFrontierTime += time.Since(tFrontierStart)

			activeHandX = renderX + fx
			activeHandY = renderY + fy
			activeHandAngle = 0
			handVisible = true
			if ev.HandStyle != "" {
				activeHandStyle = ev.HandStyle
			} else {
				activeHandStyle = "eraser"
			}
			continue
		}

		if easedProgress >= 1.0 {
			tDrawStart := time.Now()
			if visibility >= 1.0 {
				draw.Draw(buf, destRect, img, image.Point{}, draw.Over)
			} else {
				maskUniform := image.NewUniform(color.Alpha{A: uint8(visibility * 255)})
				draw.DrawMask(buf, destRect, img, image.Point{}, maskUniform, image.Point{}, draw.Over)
			}
			localDrawMaskTime += time.Since(tDrawStart)
		} else {
			tMaskStart := time.Now()
			mask := GenerateMask(img.Bounds().Dx(), img.Bounds().Dy(), easedProgress, ev.MaskStyle, maskCfg)
			localMaskGenTime += time.Since(tMaskStart)

			tDrawStart := time.Now()
			if easedProgress >= 0.9 {
				factor := (easedProgress - 0.9) / 0.1
				for i := range mask.Pix {
					val := float64(mask.Pix[i])
					mask.Pix[i] = uint8(val + (255.0-val)*factor)
				}
			}
			if visibility < 1.0 {
				for i := range mask.Pix {
					mask.Pix[i] = uint8(float64(mask.Pix[i]) * visibility)
				}
			}
			draw.DrawMask(buf, destRect, img, image.Point{}, mask, image.Point{}, draw.Over)
			localDrawMaskTime += time.Since(tDrawStart)

			tFrontierStart := time.Now()
			fx, fy := GetFrontierPoint(img.Bounds().Dx(), img.Bounds().Dy(), easedProgress, ev.MaskStyle, maskCfg)
			localFrontierTime += time.Since(tFrontierStart)

			activeHandX = renderX + fx
			activeHandY = renderY + fy
			switch ev.MaskStyle {
			case "diagonal":
				activeHandAngle = 15
			case "ltr":
				activeHandAngle = -10
			default:
				activeHandAngle = 0
			}
			handVisible = true
			if ev.HandStyle != "" {
				activeHandStyle = ev.HandStyle
			} else {
				activeHandStyle = "default"
			}
		}
	}

	if handVisible {
		tHandStart := time.Now()
		e.Hand.Draw(buf, activeHandX, activeHandY, frameNum, activeHandStyle, activeHandAngle)
		localDrawHandTime = time.Since(tHandStart)
	}

	tCropScaleStart := time.Now()
	finalFrame := CropAndScale(buf, cam, e.Width, e.Height, e.FastMode)
	if finalFrame != buf {
		e.Pool.BufferPool.Put(buf)
	}
	localCropScaleTime = time.Since(tCropScaleStart)

	atomic.AddInt64(&e.Stats.ClearBgTime, int64(tClearBg))
	atomic.AddInt64(&e.Stats.InvertColorTime, int64(localInvertColorTime))
	atomic.AddInt64(&e.Stats.ScaleAssetTime, int64(localScaleAssetTime))
	atomic.AddInt64(&e.Stats.MaskGenTime, int64(localMaskGenTime))
	atomic.AddInt64(&e.Stats.FrontierTime, int64(localFrontierTime))
	atomic.AddInt64(&e.Stats.DrawMaskTime, int64(localDrawMaskTime))
	atomic.AddInt64(&e.Stats.DrawHandTime, int64(localDrawHandTime))
	atomic.AddInt64(&e.Stats.CropScaleTime, int64(localCropScaleTime))
	atomic.AddInt64(&e.Stats.TotalFrames, 1)

	tTotal := time.Since(t0)
	atomic.AddInt64(&e.Stats.TotalRenderTime, int64(tTotal))

	return finalFrame
}

func handleSlideEvent(e *Engine, frameNum int, ev model.FrameEvent, buf *image.RGBA, visibility float64, animFrames int, cam CameraState) {
	e.AssetMu.RLock()
	img, ok := e.Assets[ev.TargetImage]
	e.AssetMu.RUnlock()
	if !ok {
		return
	}

	renderW, renderH, renderX, renderY := ev.Width, ev.Height, ev.X, ev.Y
	if ev.FitMode == "" {
		ev.FitMode = "fit"
	}

	rawW, rawH := img.Bounds().Dx(), img.Bounds().Dy()
	ratioSrc := float64(rawW) / float64(rawH)
	ratioTarget := float64(ev.Width) / float64(ev.Height)

	if ev.FitMode == "fill" {
		if ratioSrc > ratioTarget {
			renderH = ev.Height
			renderW = int(float64(ev.Height) * ratioSrc)
		} else {
			renderW = ev.Width
			renderH = int(float64(ev.Width) / ratioSrc)
		}
	} else if ev.FitMode == "stretch" {
		renderW = ev.Width
		renderH = ev.Height
	} else {
		if ratioSrc > ratioTarget {
			renderW = ev.Width
			renderH = int(float64(ev.Width) / ratioSrc)
			if renderH <= 0 {
				renderH = 1
			}
		} else {
			renderH = ev.Height
			renderW = int(float64(ev.Height) * ratioSrc)
			if renderW <= 0 {
				renderW = 1
			}
		}
	}

	renderX = ev.X + (ev.Width-renderW)/2
	renderY = ev.Y + (ev.Height-renderH)/2

	progress := float64(frameNum-ev.StartFrame) / float64(ev.EndFrame-ev.StartFrame)
	if progress > 1.0 {
		progress = 1.0
	}

	locX := renderX
	locY := renderY
	drawW := renderW
	drawH := renderH
	alpha := visibility

	transition := ev.Transition
	if transition == "" {
		transition = "none"
	}

	animWindow := animFrames
	if animWindow <= 0 {
		animWindow = 1
	}

	if progress < float64(animWindow)/float64(ev.EndFrame-ev.StartFrame+1) && transition != "none" {
		frameProgress := progress * float64(ev.EndFrame-ev.StartFrame+1) / float64(animWindow)
		if frameProgress > 1.0 {
			frameProgress = 1.0
		}
		easedFrameProgress := frameProgress * frameProgress * (3 - 2*frameProgress)

		switch transition {
		case "fade":
			alpha = easedFrameProgress * visibility
		case "pop":
			pf := 1.0 + 0.33*(1.0-easedFrameProgress)
			drawW = int(float64(renderW) * pf)
			drawH = int(float64(renderH) * pf)
			locX = renderX - (drawW-renderW)/2
			locY = renderY - (drawH-renderH)/2
			img = scaleImageProgress(img, drawW, drawH, easedFrameProgress)
		case "slide-left":
			locX = ev.X + ev.Width + int(float64(ev.Width)*(1.0-easedFrameProgress))
			locY = renderY
		case "slide-right":
			locX = ev.X - int(float64(ev.Width)*(1.0-easedFrameProgress))
			locY = renderY
		case "slide-up":
			locX = renderX
			locY = ev.Y + ev.Height + int(float64(ev.Height)*(1.0-easedFrameProgress))
		case "slide-down":
			locX = renderX
			locY = ev.Y - int(float64(ev.Height)*(1.0-easedFrameProgress))
		}
	}

	destRect := image.Rect(locX, locY, locX+drawW, locY+drawH)
	if destRect.Dx() > 0 && destRect.Dy() > 0 {
		if alpha >= 1.0 {
			draw.Draw(buf, destRect, img, image.Point{}, draw.Over)
		} else {
			maskUniform := image.NewUniform(color.Alpha{A: uint8(alpha * 255)})
			draw.DrawMask(buf, destRect, img, image.Point{}, maskUniform, image.Point{}, draw.Over)
		}
	}
}

var slideEntryAnimFrames = int(0.4 * 30) // 12 frames
var slideExitAnimFrames = int(0.4 * 30)  // 12 frames

func handleLower3rdEvent(e *Engine, frameNum int, ev model.FrameEvent, buf *image.RGBA, cam CameraState) {
	var title string
	var subtitle string
	var colorHex string

	parts := strings.SplitN(ev.TargetImage, "|", 3)
	title = parts[0]
	if len(parts) > 1 {
		subtitle = parts[1]
	}
	if len(parts) > 2 {
		colorHex = parts[2]
	}

	lower3rdW := e.Width
	lower3rdH := int(float64(e.Height) * 0.14)
	if lower3rdH < 80 {
		lower3rdH = 80
	}
	if lower3rdH > 160 {
		lower3rdH = 160
	}
	targetY := e.Height - lower3rdH - int(float64(e.Height)*0.04)

	totalAnimFrames := slideEntryAnimFrames + slideExitAnimFrames
	frameInEvent := frameNum - ev.StartFrame
	durationFrames := ev.EndFrame - ev.StartFrame

	var localY int
	var alpha float64 = 1.0

	if frameInEvent <= totalAnimFrames && durationFrames > 0 {
		if frameInEvent < slideEntryAnimFrames {
			// Entry: slide up from below
			p := float64(frameInEvent) / float64(slideEntryAnimFrames)
			ep := easeOutCubic(p)
			sourceY := e.Height - lower3rdH - int(float64(e.Height)*0.04)
			localY = int(float64(targetY-sourceY)*(1.0-ep)) + sourceY
			alpha = ep
		} else if frameInEvent > durationFrames-slideExitAnimFrames {
			// Exit: slide down
			exitFrame := frameInEvent - (durationFrames - slideExitAnimFrames)
			p := float64(exitFrame) / float64(slideExitAnimFrames)
			ep := easeInOutCubic(p)
			sourceY := e.Height + 20
			localY = targetY + int(float64(sourceY-targetY)*ep)
			alpha = 1.0 - ep
		} else {
			localY = targetY
			alpha = 1.0
		}
	} else {
		localY = targetY
		alpha = 1.0
	}

	panelKey := fmt.Sprintf("__lower3rd_%s_%s_%s", title, subtitle, colorHex)
	e.AssetMu.RLock()
	panel, ok := e.Assets[panelKey]
	e.AssetMu.RUnlock()
	if !ok {
		panel = RenderLower3rdPanel(e.Width, e.Height, title, subtitle, colorHex)
		e.AssetMu.Lock()
		e.Assets[panelKey] = panel
		e.AssetMu.Unlock()
	}

	panelRGBA, ok := panel.(*image.RGBA)
	if !ok {
		return
	}

	destRect := image.Rect(0, localY, lower3rdW, localY+lower3rdH)
	srcRect := image.Rect(0, 0, lower3rdW, lower3rdH)
	cropped := image.NewRGBA(srcRect)
	copy(cropped.Pix, panelRGBA.Pix[0:len(cropped.Pix)])

	if alpha >= 1.0 {
		draw.Draw(buf, destRect, cropped, image.Point{}, draw.Over)
	} else {
		maskUniform := image.NewUniform(color.Alpha{A: uint8(alpha * 255)})
		draw.DrawMask(buf, destRect, cropped, image.Point{}, maskUniform, image.Point{}, draw.Over)
	}
}

func scaleImageProgress(src image.Image, w, h int, progress float64) image.Image {
	if progress >= 1.0 {
		return scaleImage(src, w, h)
	}
	srcW := src.Bounds().Dx()
	srcH := src.Bounds().Dy()
	ratioSrc := float64(srcW) / float64(srcH)
	ratioTarget := float64(w) / float64(h)
	var baseW, baseH int
	if ratioSrc > ratioTarget {
		baseW = w
		baseH = int(float64(w) / ratioSrc)
		if baseH <= 0 {
			baseH = 1
		}
	} else {
		baseH = h
		baseW = int(float64(h) * ratioSrc)
		if baseW <= 0 {
			baseW = 1
		}
	}

	scale := 1.0 + 0.33*(1.0-progress)
	curW := int(float64(baseW) * scale)
	curH := int(float64(baseH) * scale)
	if curW <= 0 {
		curW = 1
	}
	if curH <= 0 {
		curH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

func scaleImage(src image.Image, w, h int) image.Image {
	if w <= 0 || h <= 0 {
		return src
	}
	if src.Bounds().Dx() == w && src.Bounds().Dy() == h {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

func invertImageColors(src image.Image) image.Image {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := src.At(x, y)
			rgba := color.RGBAModel.Convert(c).(color.RGBA)
			if rgba.A > 0 {
				f := float64(rgba.A) / 255.0
				r := uint8(f * float64(rgba.R))
				g := uint8(f * float64(rgba.G))
				b := uint8(f * float64(rgba.B))
				dst.Set(x, y, color.RGBA{
					R: 255 - r,
					G: 255 - g,
					B: 255 - b,
					A: rgba.A,
				})
			} else {
				dst.Set(x, y, color.RGBA{})
			}
		}
	}
	return dst
}
