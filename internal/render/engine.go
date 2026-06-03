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

	_ "image/jpeg"
	_ "image/png"

	"zen-board/internal/model"

	xdraw "golang.org/x/image/draw"
)

type Engine struct {
	Width, Height int
	FPS           int
	Pool          *RenderPool
	Hand          *HandRenderer
	Assets        map[string]image.Image
	ScaledAssets  map[string]image.Image
	AssetMu       sync.RWMutex
}

func NewEngine(w, h, fps int, handPath string, tipX, tipY int) (*Engine, error) {
	hr, err := NewHandRenderer(handPath)
	if err != nil {
		return nil, err
	}
	hr.TipX = tipX
	hr.TipY = tipY

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
	buf := e.Pool.BufferPool.Get().(*image.RGBA)

	// 1. Clear with appropriate background color
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

	var activeHandX, activeHandY int
	var handVisible bool
	var activeHandStyle string = "default"
	var activeHandAngle int = 0

	maskCfg := DefaultMaskConfig()

	for _, ev := range events {
		if frameNum < ev.StartFrame || frameNum > ev.EndFrame {
			continue
		}

		e.AssetMu.RLock()
		img, ok := e.Assets[ev.TargetImage]
		e.AssetMu.RUnlock()
		if !ok {
			continue
		}

		// Invert colors if blackboard style and not text
		if style == "blackboard" && !strings.HasPrefix(ev.TargetImage, "__text_") {
			key := ev.TargetImage + "_inverted"
			e.AssetMu.RLock()
			invImg, ok := e.Assets[key]
			e.AssetMu.RUnlock()
			if ok {
				img = invImg
			} else {
				invImg = invertImageColors(img)
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

			// Center the scaled image inside the bounding box of ev.X, ev.Y, ev.Width, ev.Height
			renderX = ev.X + (ev.Width-renderW)/2
			renderY = ev.Y + (ev.Height-renderH)/2

			key := fmt.Sprintf("%s_%d_%d", ev.TargetImage, renderW, renderH)
			e.AssetMu.RLock()
			scaledImg, ok := e.ScaledAssets[key]
			e.AssetMu.RUnlock()

			if ok {
				img = scaledImg
			} else {
				// Scale and cache
				scaledImg = scaleImage(img, renderW, renderH)
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
		// Smooth-step easing: slow start, fast middle, slow stop — matches human drawing rhythm.
		easedProgress := progress * progress * (3 - 2*progress)

		destRect := image.Rect(renderX, renderY, renderX+img.Bounds().Dx(), renderY+img.Bounds().Dy())

		if ev.EventType == "move" {
			rawT := float64(frameNum-ev.StartFrame) / float64(ev.EndFrame-ev.StartFrame)
			if rawT > 1.0 {
				rawT = 1.0
			}
			easedT := rawT * rawT * (3 - 2*rawT)
			curX := ev.X + int(float64(ev.DestX-ev.X)*easedT)
			curY := ev.Y + int(float64(ev.DestY-ev.Y)*easedT)
			destRect = image.Rect(curX, curY, curX+renderW, curY+renderH)
			draw.Draw(buf, destRect, img, image.Point{}, draw.Over)

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
			// Angle from movement direction (capped ±25°)
			if dx != 0 || dy != 0 {
				angRad := math.Atan2(float64(dy), float64(dx))
				ang := int(angRad * 180 / math.Pi)
				if ang > 25 { ang = 25 }
				if ang < -25 { ang = -25 }
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
			mask := GenerateMask(img.Bounds().Dx(), img.Bounds().Dy(), easedProgress, ev.MaskStyle, maskCfg)
			for i := range mask.Pix {
				mask.Pix[i] = 255 - mask.Pix[i]
			}
			if easedProgress >= 0.9 {
				factor := (1.0 - easedProgress) / 0.1
				for i := range mask.Pix {
					mask.Pix[i] = uint8(float64(mask.Pix[i]) * factor)
				}
			}
			draw.DrawMask(buf, destRect, img, image.Point{}, mask, image.Point{}, draw.Over)

			fx, fy := GetFrontierPoint(img.Bounds().Dx(), img.Bounds().Dy(), easedProgress, ev.MaskStyle, maskCfg)
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
			draw.Draw(buf, destRect, img, image.Point{}, draw.Over)
		} else {
			mask := GenerateMask(img.Bounds().Dx(), img.Bounds().Dy(), easedProgress, ev.MaskStyle, maskCfg)
			if easedProgress >= 0.9 {
				factor := (easedProgress - 0.9) / 0.1
				for i := range mask.Pix {
					val := float64(mask.Pix[i])
					mask.Pix[i] = uint8(val + (255.0-val)*factor)
				}
			}
			draw.DrawMask(buf, destRect, img, image.Point{}, mask, image.Point{}, draw.Over)

			fx, fy := GetFrontierPoint(img.Bounds().Dx(), img.Bounds().Dy(), easedProgress, ev.MaskStyle, maskCfg)
			activeHandX = renderX + fx
			activeHandY = renderY + fy
			// Angle by mask style: diagonal tilts 15°, ltr tilts -10°, ttb is flat
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

	// 3. Draw Hand
	if handVisible {
		e.Hand.Draw(buf, activeHandX, activeHandY, frameNum, activeHandStyle, activeHandAngle)
	}

	// 4. Crop and Scale relative to CameraState
	finalFrame := CropAndScale(buf, cam, e.Width, e.Height)
	if finalFrame != buf {
		e.Pool.BufferPool.Put(buf)
	}

	return finalFrame
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
			nrgba := color.NRGBAModel.Convert(c).(color.NRGBA)
			if nrgba.A > 0 {
				dst.Set(x, y, color.NRGBA{
					R: 255 - nrgba.R,
					G: 255 - nrgba.G,
					B: 255 - nrgba.B,
					A: nrgba.A,
				})
			} else {
				dst.Set(x, y, color.Alpha{0})
			}
		}
	}
	return dst
}
