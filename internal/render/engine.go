package render

import (
	"fmt"
	"image"
	"image/draw"
	"os"
	"sync"
	"zen-board/internal/model"
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
				frame := e.RenderFrame(job.Index, job.Events)
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

// RenderFrame generates a single frame based on the active events.
func (e *Engine) RenderFrame(frameNum int, events []model.FrameEvent) *image.RGBA {
	buf := e.Pool.BufferPool.Get().(*image.RGBA)
	
	// 1. Clear with white background
	white := image.NewUniform(image.White)
	draw.Draw(buf, buf.Bounds(), white, image.Point{}, draw.Src)

	var activeHandX, activeHandY int
	var handVisible bool

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

		if ev.Width > 0 && ev.Height > 0 {
			key := fmt.Sprintf("%s_%d_%d", ev.TargetImage, ev.Width, ev.Height)
			e.AssetMu.RLock()
			scaledImg, ok := e.ScaledAssets[key]
			e.AssetMu.RUnlock()

			if ok {
				img = scaledImg
			} else {
				// Scale and cache
				scaledImg = scaleImage(img, ev.Width, ev.Height)
				e.AssetMu.Lock()
				e.ScaledAssets[key] = scaledImg
				e.AssetMu.Unlock()
				img = scaledImg
			}
		}

		progress := float64(frameNum-ev.StartFrame) / float64(ev.EndFrame-ev.StartFrame)
		if progress > 1.0 {
			progress = 1.0
		}

		// Generate mask for this specific image reveal
		mask := GenerateMask(img.Bounds().Dx(), img.Bounds().Dy(), progress, maskCfg)
		
		// Draw masked image onto canvas
		destRect := image.Rect(ev.X, ev.Y, ev.X+img.Bounds().Dx(), ev.Y+img.Bounds().Dy())
		draw.DrawMask(buf, destRect, img, image.Point{}, mask, image.Point{}, draw.Over)

		// Hand follows the LAST active reveal event
		if progress < 1.0 {
			fx, fy := GetFrontierPoint(img.Bounds().Dx(), img.Bounds().Dy(), progress, maskCfg)
			activeHandX = ev.X + fx
			activeHandY = ev.Y + fy
			handVisible = true
		}
	}

	// 3. Draw Hand
	if handVisible {
		e.Hand.Draw(buf, activeHandX, activeHandY, frameNum)
	}

	return buf
}

func scaleImage(src image.Image, w, h int) image.Image {
	if w <= 0 || h <= 0 {
		return src
	}
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW == w && srcH == h {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		srcY := bounds.Min.Y + (y * srcH / h)
		for x := 0; x < w; x++ {
			srcX := bounds.Min.X + (x * srcW / w)
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}
