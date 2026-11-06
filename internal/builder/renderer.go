package builder

import (
	"fmt"
	"image"
	"sort"
	"strings"
	"time"
	"zen-board/internal/constant"
	"zen-board/internal/model"
	"zen-board/internal/render"
)

type zoomKeyframe struct {
	frame  int
	target string
}

// RenderTimeline drives the frame loop into a sink.
//
// The loop is a pure producer: it computes camera and style state, asks the sink
// which frames it wants, renders exactly those through the engine's worker pool,
// and hands each result to the sink in timeline order. Buffer ownership never moves
// (see FrameSink): the frame comes from the pool and returns to it here.
func RenderTimeline(conf *model.Project, timeline *model.Timeline, engine *render.Engine, sink FrameSink, styleKeyframes []StyleKeyframe, pLines []model.ProcessedLine, cameraEnabled bool, eventLog *render.EventLogger) error {
	totalFrames := pipelineFrameCount(conf, timeline)

	// Clamp all event EndFrame boundaries to totalFrames - 1 to handle sentinels safely
	for i := range timeline.Events {
		if timeline.Events[i].EndFrame >= totalFrames {
			timeline.Events[i].EndFrame = totalFrames - 1
		}
	}

	cameraStates, err := computeCameraStates(conf, timeline, pLines, totalFrames, cameraEnabled)
	if err != nil {
		return err
	}
	styleStates := computeStyleStates(styleKeyframes, totalFrames)

	logEventTransitions(eventLog, timeline.Events, totalFrames, conf.FPS)

	frames := planFrames(totalFrames, sink)
	if len(frames) == 0 {
		return sink.Close()
	}

	tTimelineStart := time.Now()
	var sinkWriteTime time.Duration

	engine.StartWorkers()

	// Limit to at most workers * 2 frames in-flight (uncompressed RGBA in memory)
	inFlightLimit := engine.Pool.Workers * 2
	if inFlightLimit < 8 {
		inFlightLimit = 8
	}
	sem := make(chan struct{}, inFlightLimit)

	go func() {
		for _, f := range frames {
			sem <- struct{}{}
			engine.Pool.Jobs <- render.FrameJob{
				Index:  f,
				Events: timeline.Events,
				Cam:    cameraStates[f],
				Style:  styleStates[f],
			}
		}
		close(engine.Pool.Jobs)
	}()

	fmt.Printf("Rendering %d of %d frames (parallel)...\n", len(frames), totalFrames)

	// Collect in the planned order. Driving this by the plan slice rather than by a
	// running counter is what makes a partial render terminate: a frame that was
	// never submitted would never arrive.
	results := make(map[int]*image.RGBA)
	for i, want := range frames {
		frame, ok := results[want]
		for !ok {
			res, open := <-engine.Pool.Results
			if !open {
				return fmt.Errorf("frame %d was never produced", want)
			}
			results[res.Index] = res.Frame
			frame, ok = results[want]
		}

		tSinkStart := time.Now()
		if err := sink.WriteFrame(want, frame); err != nil {
			return fmt.Errorf("sink write: %w", err)
		}
		sinkWriteTime += time.Since(tSinkStart)

		delete(results, want)
		engine.Pool.BufferPool.Put(frame)
		<-sem // Release in-flight frame slot

		if i%30 == 0 {
			fmt.Printf("\rProgress: %d/%d (%.1f%%)", i, len(frames), float64(i)*100/float64(len(frames)))
		}
	}
	fmt.Println("\nFinishing output...")
	if err := sink.Close(); err != nil {
		return fmt.Errorf("sink close: %w", err)
	}

	if done, ok := sink.(Reporter); ok {
		fmt.Println(done.Done())
		fmt.Printf("Total rendering execution: %v\n", time.Since(tTimelineStart))
		fmt.Printf("Total time writing to %s: %v\n", done.Name(), sinkWriteTime)
	}
	engine.PrintStats()
	return nil
}

// pipelineFrameCount is the frame count of the finished video, freeze tail included.
func pipelineFrameCount(conf *model.Project, timeline *model.Timeline) int {
	return int(timeline.Duration*float64(conf.FPS)) + conf.FreezeFrames
}

// logEventTransitions writes the ENTER/EXIT event log over the whole frame range,
// independently of which frames are rendered: -eventlog describes the timeline, not
// the subset that was sampled.
func logEventTransitions(eventLog *render.EventLogger, events []model.FrameEvent, totalFrames, fps int) {
	if eventLog == nil {
		return
	}
	prevActive := make([]bool, len(events))
	for f := 0; f < totalFrames; f++ {
		for i := range events {
			ev := events[i]
			nowActive := f >= ev.StartFrame && f <= ev.EndFrame
			if nowActive && !prevActive[i] {
				eventLog.LogTransition("ENTER", f, ev, fps)
			}
			if !nowActive && prevActive[i] {
				eventLog.LogTransition("EXIT", f, ev, fps)
			}
			prevActive[i] = nowActive
		}
	}
	for i := range events {
		if prevActive[i] {
			eventLog.LogTransition("EXIT", totalFrames-1, events[i], fps)
		}
	}
}

// computeCameraStates resolves every [zoom:] into a per-frame camera state, with
// the one-second eased transition between consecutive targets.
func computeCameraStates(conf *model.Project, timeline *model.Timeline, pLines []model.ProcessedLine, totalFrames int, cameraEnabled bool) ([]render.CameraState, error) {
	var zoomKeyframes []zoomKeyframe
	for _, pl := range pLines {
		for _, action := range pl.Actions {
			if !strings.HasPrefix(action.Tag, constant.TAG_ZOOM) {
				continue
			}
			triggerTime := pl.StartTime
			if action.WordIndex > 0 {
				triggerWordIdx := pl.WordOffset + action.WordIndex - 1
				if triggerWordIdx >= 0 && triggerWordIdx < len(timeline.Words) {
					triggerTime = timeline.Words[triggerWordIdx].Start
				}
			}
			frame := int(triggerTime * float64(conf.FPS))
			target := strings.TrimPrefix(action.Tag, constant.TAG_ZOOM)
			zoomKeyframes = append(zoomKeyframes, zoomKeyframe{frame: frame, target: target})
		}
	}

	sort.Slice(zoomKeyframes, func(i, j int) bool {
		return zoomKeyframes[i].frame < zoomKeyframes[j].frame
	})

	cameraStates := make([]render.CameraState, totalFrames)
	defaultCam := render.GetPresetViewport(constant.FOCUS_RESET, conf.Width, conf.Height)

	if !cameraEnabled {
		for f := 0; f < totalFrames; f++ {
			cameraStates[f] = defaultCam
		}
		return cameraStates, nil
	}

	currentCam := defaultCam
	targetCam := defaultCam
	startTransitionFrame := -1
	transitionDuration := int(1.0 * float64(conf.FPS))
	transitionStartCam := defaultCam
	nextZoomIdx := 0

	for f := 0; f < totalFrames; f++ {
		if nextZoomIdx < len(zoomKeyframes) && f >= zoomKeyframes[nextZoomIdx].frame {
			preset := zoomKeyframes[nextZoomIdx].target
			targetCam = render.GetPresetViewport(preset, conf.Width, conf.Height)
			transitionStartCam = currentCam
			startTransitionFrame = f
			nextZoomIdx++
		}
		if startTransitionFrame != -1 && f < startTransitionFrame+transitionDuration {
			t := float64(f-startTransitionFrame) / float64(transitionDuration)
			currentCam = render.LerpCamera(transitionStartCam, targetCam, t)
		} else {
			currentCam = targetCam
		}
		cameraStates[f] = currentCam
	}
	return cameraStates, nil
}

// computeStyleStates resolves the style keyframes into a per-frame board style.
func computeStyleStates(styleKeyframes []StyleKeyframe, totalFrames int) []string {
	styleStates := make([]string, totalFrames)
	currentStyleState := constant.STYLE_WHITEBOARD
	sort.Slice(styleKeyframes, func(i, j int) bool {
		return styleKeyframes[i].Frame < styleKeyframes[j].Frame
	})
	nextStyleIdx := 0
	for f := 0; f < totalFrames; f++ {
		if nextStyleIdx < len(styleKeyframes) && f >= styleKeyframes[nextStyleIdx].Frame {
			currentStyleState = styleKeyframes[nextStyleIdx].Style
			nextStyleIdx++
		}
		styleStates[f] = currentStyleState
	}
	return styleStates
}
