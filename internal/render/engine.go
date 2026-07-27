package render

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"sort"
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

type EngineEventLog struct {
	mu       sync.Mutex
	Entries  []EventLogEntry
	Enabled  bool
}

type EventLogEntry struct {
	Frame      int     `json:"frame"`
	EventID    string  `json:"event_id"`
	TsMs       float64 `json:"ts_ms,omitempty"`
	InvertNs   int64   `json:"invert_ns,omitempty"`
	ScaleNs    int64   `json:"scale_ns,omitempty"`
	MaskNs     int64   `json:"mask_ns,omitempty"`
	FrontierNs int64   `json:"frontier_ns,omitempty"`
	DrawNs     int64   `json:"draw_ns,omitempty"`
	HandNs     int64   `json:"hand_ns,omitempty"`
	TotalNs    int64   `json:"total_ns"`
	Progress   float64 `json:"progress,omitempty"`
	Visibility float64 `json:"visibility,omitempty"`
}

type EventSummary struct {
	EventID    string  `json:"event_id"`
	EventType  string  `json:"type"`
	Target     string  `json:"target"`
	StartFrame int     `json:"start_frame"`
	EndFrame   int     `json:"end_frame"`
	StartMs    float64 `json:"start_ms"`
	EndMs      float64 `json:"end_ms"`
	FrameCount int     `json:"frame_count"`
	TotalNs    int64   `json:"total_ns"`
	AvgNs      int64   `json:"avg_ns"`
	MaxNs      int64   `json:"max_ns"`
	MaxNsFrame int     `json:"max_ns_frame"`
}

func eventID(ev model.FrameEvent) string {
	target := ev.TargetImage
	if target == "" {
		target = "-"
	}
	return fmt.Sprintf("%s:%s:%d-%d", ev.EventType, target, ev.StartFrame, ev.EndFrame)
}

func parseEventID(id string) (etype, target string, start, end int) {
	parts := strings.SplitN(id, ":", 3)
	if len(parts) == 3 {
		etype, target = parts[0], parts[1]
		fmt.Sscanf(parts[2], "%d-%d", &start, &end)
	}
	return
}

func NewEngineEventLog(enabled bool) *EngineEventLog {
	return &EngineEventLog{Enabled: enabled}
}

func (l *EngineEventLog) Append(entry EventLogEntry) {
	if !l.Enabled {
		return
	}
	l.mu.Lock()
	l.Entries = append(l.Entries, entry)
	l.mu.Unlock()
}

func buildEventSummaries(entries []EventLogEntry, fps int) []EventSummary {
	byID := map[string]*EventSummary{}
	order := []string{}
	for _, en := range entries {
		s, ok := byID[en.EventID]
		if !ok {
			et, target, start, end := parseEventID(en.EventID)
			s = &EventSummary{
				EventID:    en.EventID,
				EventType:  et,
				Target:     target,
				StartFrame: start,
				EndFrame:   end,
				StartMs:    float64(start) / float64(fps) * 1000,
				EndMs:      float64(end) / float64(fps) * 1000,
			}
			byID[en.EventID] = s
			order = append(order, en.EventID)
		}
		s.FrameCount++
		s.TotalNs += en.TotalNs
		if en.TotalNs > s.MaxNs {
			s.MaxNs = en.TotalNs
			s.MaxNsFrame = en.Frame
		}
	}
	sort.Strings(order)
	out := make([]EventSummary, 0, len(order))
	for _, id := range order {
		s := byID[id]
		if s.FrameCount > 0 {
			s.AvgNs = s.TotalNs / int64(s.FrameCount)
		}
		out = append(out, *s)
	}
	return out
}

func writeJSONL[T any](path string, rows []T) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
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
	EventLog      *EngineEventLog
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
		EventLog:     NewEngineEventLog(false),
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

func (e *Engine) EventLogSize() int {
	if e.EventLog == nil {
		return 0
	}
	e.EventLog.mu.Lock()
	defer e.EventLog.mu.Unlock()
	return len(e.EventLog.Entries)
}

func (e *Engine) PrintEventReport() {
	if e.EventLog == nil || !e.EventLog.Enabled || len(e.EventLog.Entries) == 0 {
		return
	}
	e.EventLog.mu.Lock()
	entries := make([]EventLogEntry, len(e.EventLog.Entries))
	copy(entries, e.EventLog.Entries)
	e.EventLog.mu.Unlock()

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Frame != entries[j].Frame {
			return entries[i].Frame < entries[j].Frame
		}
		return entries[i].EventID < entries[j].EventID
	})

	summaries := buildEventSummaries(entries, e.FPS)

	fmt.Printf("\n=== Script Event Render Map (%d events, %d samples) ===\n", len(summaries), len(entries))
	for _, s := range summaries {
		fmt.Printf("  %-40s frames %6d-%6d (%7.1f-%7.1fms) | n=%-4d avg=%-10s max=%-10s @f%d\n",
			s.EventID, s.StartFrame, s.EndFrame, s.StartMs, s.EndMs,
			s.FrameCount, time.Duration(s.AvgNs), time.Duration(s.MaxNs), s.MaxNsFrame)
	}

	top := append([]EventSummary(nil), summaries...)
	sort.Slice(top, func(i, j int) bool { return top[i].MaxNs > top[j].MaxNs })
	if len(top) > 5 {
		top = top[:5]
	}
	fmt.Println("  --- Slowest single-frame samples ---")
	for _, s := range top {
		fmt.Printf("  %-40s max=%-10s @frame %d\n", s.EventID, time.Duration(s.MaxNs), s.MaxNsFrame)
	}
}

func (e *Engine) ExportEventLogJSON(path string) error {
	if e.EventLog == nil {
		return fmt.Errorf("event log is not enabled")
	}
	e.EventLog.mu.Lock()
	entries := make([]EventLogEntry, len(e.EventLog.Entries))
	copy(entries, e.EventLog.Entries)
	e.EventLog.mu.Unlock()

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Frame != entries[j].Frame {
			return entries[i].Frame < entries[j].Frame
		}
		return entries[i].EventID < entries[j].EventID
	})

	if err := writeJSONL(path, entries); err != nil {
		return fmt.Errorf("write event log jsonl: %w", err)
	}

	summaries := buildEventSummaries(entries, e.FPS)
	return writeJSONL(path+".summary.jsonl", summaries)
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
	draw.Draw(buf, buf.Bounds(), ResolveStyleBg(style), image.Point{}, draw.Src)
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

		evProgress := 0.0
		evInvertS := localInvertColorTime
		evScaleS := localScaleAssetTime
		evMaskS := localMaskGenTime
		evFrontierS := localFrontierTime
		evDrawS := localDrawMaskTime

		var logID string
		var tsMs float64
		if e.EventLog != nil && e.EventLog.Enabled {
			logID = eventID(ev)
			tsMs = float64(frameNum) / float64(e.FPS) * 1000
		}

		if e.EventLog != nil && e.EventLog.Enabled {
			appendEvLog := func(p float64) {
				invert := int64(localInvertColorTime - evInvertS)
				scale := int64(localScaleAssetTime - evScaleS)
				mask := int64(localMaskGenTime - evMaskS)
				frontier := int64(localFrontierTime - evFrontierS)
				drawN := int64(localDrawMaskTime - evDrawS)
				e.EventLog.Append(EventLogEntry{
					Frame:      frameNum,
					EventID:    logID,
					TsMs:       tsMs,
					InvertNs:   invert,
					ScaleNs:    scale,
					MaskNs:     mask,
					FrontierNs: frontier,
					DrawNs:     drawN,
					TotalNs:    invert + scale + mask + frontier + drawN,
					Progress:   p,
					Visibility: visibility,
				})
			}
			if ev.EventType == "slide" {
				handleSlideEvent(e, frameNum, ev, buf, visibility, slideAnimFrames, cam)
				appendEvLog(0)
				continue
			}
			if ev.EventType == "lower3rd" {
				handleLower3rdEvent(e, frameNum, ev, buf, cam)
				appendEvLog(0)
				continue
			}
			if ev.EventType == "arrow" || ev.EventType == "arrow_static" {
				handleArrowEvent(e, frameNum, ev, buf, visibility)
				appendEvLog(0)
				continue
			}
			if ev.EventType == "highlight" {
				handleHighlightEvent(e, frameNum, ev, buf, visibility)
				appendEvLog(0)
				continue
			}
			if ev.EventType == "compare" {
				handleCompareEvent(e, frameNum, ev, buf, visibility, style)
				appendEvLog(0)
				continue
			}
			if ev.EventType == "overlay" {
				handleOverlayEvent(e, frameNum, ev, buf, visibility)
				appendEvLog(0)
				continue
			}
			if ev.EventType == "transition" {
				handleTransitionEvent(e, frameNum, ev, buf, visibility)
				appendEvLog(0)
				continue
			}
			if ev.EventType == "counter" {
				handleCounterEvent(e, frameNum, ev, buf, visibility, style)
				appendEvLog(0)
				continue
			}
		} else {
			if ev.EventType == "slide" {
				handleSlideEvent(e, frameNum, ev, buf, visibility, slideAnimFrames, cam)
				continue
			}
			if ev.EventType == "lower3rd" {
				handleLower3rdEvent(e, frameNum, ev, buf, cam)
				continue
			}
			if ev.EventType == "arrow" || ev.EventType == "arrow_static" {
				handleArrowEvent(e, frameNum, ev, buf, visibility)
				continue
			}
			if ev.EventType == "highlight" {
				handleHighlightEvent(e, frameNum, ev, buf, visibility)
				continue
			}
			if ev.EventType == "compare" {
				handleCompareEvent(e, frameNum, ev, buf, visibility, style)
				continue
			}
			if ev.EventType == "overlay" {
				handleOverlayEvent(e, frameNum, ev, buf, visibility)
				continue
			}
			if ev.EventType == "transition" {
				handleTransitionEvent(e, frameNum, ev, buf, visibility)
				continue
			}
			if ev.EventType == "counter" {
				handleCounterEvent(e, frameNum, ev, buf, visibility, style)
				continue
			}
		}

		e.AssetMu.RLock()
		img, ok := e.Assets[ev.TargetImage]
		e.AssetMu.RUnlock()
		if !ok {
			if e.EventLog != nil && e.EventLog.Enabled {
				e.EventLog.Append(EventLogEntry{
					Frame:      frameNum,
					EventID:    logID,
					TsMs:       tsMs,
					Progress:   evProgress,
					Visibility: visibility,
				})
			}
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

		easedProgress := EaseInOut(CalcProgress(frameNum, ev.StartFrame, ev.EndFrame))
		evProgress = easedProgress

		destRect := image.Rect(renderX, renderY, renderX+img.Bounds().Dx(), renderY+img.Bounds().Dy())

		logEnd := func() {
			if e.EventLog != nil && e.EventLog.Enabled {
				invert := int64(localInvertColorTime - evInvertS)
				scale := int64(localScaleAssetTime - evScaleS)
				mask := int64(localMaskGenTime - evMaskS)
				frontier := int64(localFrontierTime - evFrontierS)
				drawN := int64(localDrawMaskTime - evDrawS)
				e.EventLog.Append(EventLogEntry{
					Frame:      frameNum,
					EventID:    logID,
					TsMs:       tsMs,
					InvertNs:   invert,
					ScaleNs:    scale,
					MaskNs:     mask,
					FrontierNs: frontier,
					DrawNs:     drawN,
					TotalNs:    invert + scale + mask + frontier + drawN,
					Progress:   evProgress,
					Visibility: visibility,
				})
			}
		}

		if ev.EventType == "static" {
			tDrawStart := time.Now()
			DrawWithMask(buf, destRect, img, visibility)
			localDrawMaskTime += time.Since(tDrawStart)
			logEnd()
			continue
		}

		if ev.EventType == "move" {
			easedT := EaseInOut(CalcProgress(frameNum, ev.StartFrame, ev.EndFrame))
			curX := ev.X + int(float64(ev.DestX-ev.X)*easedT)
			curY := ev.Y + int(float64(ev.DestY-ev.Y)*easedT)
			destRect = image.Rect(curX, curY, curX+renderW, curY+renderH)
			tDrawStart := time.Now()
			DrawWithMask(buf, destRect, img, visibility)
			localDrawMaskTime += time.Since(tDrawStart)

			dx := ev.DestX - ev.X
			dy := ev.DestY - ev.Y
			handOffX, handOffY := HandOffset(dx, dy, renderW, renderH)
			activeHandX = curX + renderW/2 + handOffX
			activeHandY = curY + renderH/2 + handOffY
			if dx != 0 || dy != 0 {
				activeHandAngle = ComputeHandAngle(dx, dy)
			}
			handVisible = true
			activeHandStyle = ResolveStr(ev.HandStyle, "default")
			logEnd()
			continue
		}

		if ev.EventType == "erase" {
			if easedProgress >= 1.0 {
				logEnd()
				continue
			}
			tMaskStart := time.Now()
			mask := GenerateMask(img.Bounds().Dx(), img.Bounds().Dy(), easedProgress, ev.MaskStyle, maskCfg)
			localMaskGenTime += time.Since(tMaskStart)

			tDrawStart := time.Now()
			ApplyEasedProgressMask(mask, easedProgress, visibility)
			draw.DrawMask(buf, destRect, img, image.Point{}, mask, image.Point{}, draw.Over)
			localDrawMaskTime += time.Since(tDrawStart)

			tFrontierStart := time.Now()
			fx, fy := GetFrontierPoint(img.Bounds().Dx(), img.Bounds().Dy(), easedProgress, ev.MaskStyle, maskCfg)
			localFrontierTime += time.Since(tFrontierStart)

			activeHandX = renderX + fx
			activeHandY = renderY + fy
			activeHandAngle = 0
			handVisible = true
			activeHandStyle = ResolveStr(ev.HandStyle, "eraser")
			logEnd()
			continue
		}

		if easedProgress >= 1.0 {
			tDrawStart := time.Now()
			DrawWithMask(buf, destRect, img, visibility)
			localDrawMaskTime += time.Since(tDrawStart)
		} else {
			tMaskStart := time.Now()
			mask := GenerateMask(img.Bounds().Dx(), img.Bounds().Dy(), easedProgress, ev.MaskStyle, maskCfg)
			localMaskGenTime += time.Since(tMaskStart)

			tDrawStart := time.Now()
			if easedProgress >= 0.9 {
				factor := (easedProgress - 0.9) / 0.1
				for j := range mask.Pix {
					val := float64(mask.Pix[j])
					mask.Pix[j] = uint8(val + (255.0-val)*factor)
				}
			}
			if visibility < 1.0 {
				for j := range mask.Pix {
					mask.Pix[j] = uint8(float64(mask.Pix[j]) * visibility)
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
			if ev.MaskStyle == "diagonal" {
				activeHandAngle = 15
			} else if ev.MaskStyle == "ltr" {
				activeHandAngle = -10
			}
			handVisible = true
			if ev.HandStyle != "" {
				activeHandStyle = ev.HandStyle
			} else {
				activeHandStyle = "default"
			}
		}

		logEnd()
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

	// Scale the asset to the computed render dimensions if needed
	if renderW > 0 && renderH > 0 && (renderW != rawW || renderH != rawH) {
		key := fmt.Sprintf("%s_%d_%d", ev.TargetImage, renderW, renderH)
		e.AssetMu.RLock()
		scaledImg, ok := e.ScaledAssets[key]
		e.AssetMu.RUnlock()
		if ok {
			img = scaledImg
		} else {
			scaledImg = scaleImage(img, renderW, renderH)
			e.AssetMu.Lock()
			e.ScaledAssets[key] = scaledImg
			e.AssetMu.Unlock()
			img = scaledImg
		}
	}

	progress := CalcProgress(frameNum, ev.StartFrame, ev.EndFrame)

	locX := renderX
	locY := renderY
	drawW := renderW
	drawH := renderH
	alpha := visibility

	transition := ResolveStr(ev.Transition, "none")

	animWindow := animFrames
	if animWindow <= 0 {
		animWindow = 1
	}

	if progress < float64(animWindow)/float64(ev.EndFrame-ev.StartFrame+1) && transition != "none" {
		frameProgress := progress * float64(ev.EndFrame-ev.StartFrame+1) / float64(animWindow)
		if frameProgress > 1.0 {
			frameProgress = 1.0
		}
		easedFrameProgress := EaseInOut(frameProgress)

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
			locX = renderX + int(float64(ev.X+ev.Width-renderX)*(1.0-easedFrameProgress))
			locY = renderY
		case "slide-right":
			locX = renderX - int(float64(renderX+drawW-ev.X)*(1.0-easedFrameProgress))
			locY = renderY
		case "slide-up":
			locX = renderX
			locY = renderY + int(float64(ev.Y+ev.Height-renderY)*(1.0-easedFrameProgress))
		case "slide-down":
			locX = renderX
			locY = renderY - int(float64(renderY+drawH-ev.Y)*(1.0-easedFrameProgress))
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

	rest := strings.TrimPrefix(ev.TargetImage, "__lower3rd_")
	parts := strings.SplitN(rest, "|", 3)
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
			ep := EaseOutCubic(float64(frameInEvent) / float64(slideEntryAnimFrames))
			sourceY := e.Height - lower3rdH - int(float64(e.Height)*0.04)
			localY = int(float64(targetY-sourceY)*(1.0-ep)) + sourceY
			alpha = ep
		} else if frameInEvent > durationFrames-slideExitAnimFrames {
			ep := EaseInOutCubic(float64(frameInEvent-(durationFrames-slideExitAnimFrames)) / float64(slideExitAnimFrames))
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

	DrawWithMask(buf, destRect, cropped, alpha)
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
				dst.Set(x, y, color.RGBA{
					R: 255 - rgba.R,
					G: 255 - rgba.G,
					B: 255 - rgba.B,
					A: rgba.A,
				})
			} else {
				dst.Set(x, y, color.RGBA{})
			}
		}
	}
	return dst
}
