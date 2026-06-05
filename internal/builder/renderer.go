package builder

import (
	"fmt"
	"image"
	"sort"
	"strings"
	"time"
	"zen-board/internal/ffmpeg"
	"zen-board/internal/model"
	"zen-board/internal/render"
)

type zoomKeyframe struct {
	frame  int
	target string
}

func RenderTimeline(conf *model.Project, timeline *model.Timeline, engine *render.Engine, pipe *ffmpeg.Pipe, styleKeyframes []StyleKeyframe, pLines []model.ProcessedLine, cameraEnabled bool) error {
	totalFrames := int(timeline.Duration*float64(conf.FPS)) + conf.FreezeFrames

	// Clamp all event EndFrame boundaries to totalFrames - 1 to handle sentinels safely
	for i := range timeline.Events {
		if timeline.Events[i].EndFrame >= totalFrames {
			timeline.Events[i].EndFrame = totalFrames - 1
		}
	}

	// Generate Camera States
	var zoomKeyframes []zoomKeyframe
	for _, pl := range pLines {
		for _, action := range pl.Actions {
			if strings.HasPrefix(action.Tag, "zoom:") {
				triggerTime := pl.StartTime
				if action.WordIndex > 0 {
					triggerWordIdx := pl.WordOffset + action.WordIndex - 1
					if triggerWordIdx >= 0 && triggerWordIdx < len(timeline.Words) {
						triggerTime = timeline.Words[triggerWordIdx].Start
					}
				}
				frame := int(triggerTime * float64(conf.FPS))
				target := strings.TrimPrefix(action.Tag, "zoom:")
				zoomKeyframes = append(zoomKeyframes, zoomKeyframe{
					frame:  frame,
					target: target,
				})
			}
		}
	}

	sort.Slice(zoomKeyframes, func(i, j int) bool {
		return zoomKeyframes[i].frame < zoomKeyframes[j].frame
	})

	cameraStates := make([]render.CameraState, totalFrames)
	defaultCam := render.GetPresetViewport("reset", conf.Width, conf.Height)

	if !cameraEnabled {
		for f := 0; f < totalFrames; f++ {
			cameraStates[f] = defaultCam
		}
	} else {
		currentCam := defaultCam
		targetCam := defaultCam
		startTransitionFrame := -1
		transitionDuration := int(1.0 * float64(conf.FPS)) // 1 second transition
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
	}

	styleStates := make([]string, totalFrames)
	currentStyleState := "whiteboard"
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

	tTimelineStart := time.Now()
	var pipeWriteTime time.Duration

	engine.StartWorkers()

	// Limit to at most 120 frames in-flight (uncompressed RGBA in memory)
	sem := make(chan struct{}, 120)

	// Feed jobs in a goroutine
	go func() {
		for f := 0; f < totalFrames; f++ {
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

	fmt.Printf("Rendering %d frames (parallel)...\n", totalFrames)

	// Collect results in order
	resultsMap := make(map[int]*image.RGBA)
	nextFrame := 0

	for nextFrame < totalFrames {
		res := <-engine.Pool.Results
		resultsMap[res.Index] = res.Frame

		// Drain as many sequential frames as possible
		for {
			frame, available := resultsMap[nextFrame]
			if !available {
				break
			}

			tPipeStart := time.Now()
			err := pipe.WriteFrame(frame.Pix)
			pipeWriteTime += time.Since(tPipeStart)
			if err != nil {
				return fmt.Errorf("pipe write: %w", err)
			}

			// Clean up and return to pool
			delete(resultsMap, nextFrame)
			engine.Pool.BufferPool.Put(frame)
			<-sem // Release in-flight frame slot

			if nextFrame%30 == 0 {
				fmt.Printf("\rProgress: %d/%d (%.1f%%)", nextFrame, totalFrames, float64(nextFrame)*100/float64(totalFrames))
			}
			nextFrame++
		}
	}
	fmt.Println("\nFinishing encoding...")
	pipe.Close()

	fmt.Printf("Done! Video saved to %s\n", conf.OutputPath)
	fmt.Printf("Total rendering execution: %v\n", time.Since(tTimelineStart))
	fmt.Printf("Total time writing to FFmpeg pipe: %v\n", pipeWriteTime)
	engine.PrintStats()
	return nil
}
