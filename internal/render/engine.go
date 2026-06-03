package render

import (
	"fmt"
	"image"
	"image/draw"
	"os"
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
				frame := e.RenderFrame(job.Index, job.Events, job.Cam)
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
func (e *Engine) RenderFrame(frameNum int, events []model.FrameEvent, cam CameraState) *image.RGBA {
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

		progress := float64(frameNum-ev.StartFrame) / float64(ev.EndFrame-ev.StartFrame)
		if progress > 1.0 {
			progress = 1.0
		}

		destRect := image.Rect(renderX, renderY, renderX+img.Bounds().Dx(), renderY+img.Bounds().Dy())

		if progress >= 1.0 {
			// Optimization: Draw directly and avoid mask generation/allocation overhead for fully-revealed images
			draw.Draw(buf, destRect, img, image.Point{}, draw.Over)
		} else {
			// Generate mask for this specific image reveal
			mask := GenerateMask(img.Bounds().Dx(), img.Bounds().Dy(), progress, maskCfg)

			// Draw masked image onto canvas
			draw.DrawMask(buf, destRect, img, image.Point{}, mask, image.Point{}, draw.Over)

			// Hand follows the LAST active reveal event
			fx, fy := GetFrontierPoint(img.Bounds().Dx(), img.Bounds().Dy(), progress, maskCfg)
			activeHandX = renderX + fx
			activeHandY = renderY + fy
			handVisible = true
		}
	}

	// 3. Draw Hand
	if handVisible {
		e.Hand.Draw(buf, activeHandX, activeHandY, frameNum)
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
