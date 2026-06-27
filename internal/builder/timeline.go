package builder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"zen-board/internal/assets"
	"zen-board/internal/model"
	"zen-board/internal/render"
)

type TextRenderJob struct {
	AssetID    string
	Content    string
	FontFamily string
	FontSize   float64
	IsBold     bool
	Style      string
}

type GenRenderJob struct {
	AssetID string
	Prompt  string
}

type StyleKeyframe struct {
	Frame int
	Style string
}

type ChapterMarker struct {
	StartTime float64
	Title     string
}

type TimelineCompilation struct {
	Timeline        *model.Timeline
	TextJobs        []TextRenderJob
	GenJobs         []GenRenderJob
	StyleKeyframes  []StyleKeyframe
	Chapters        []ChapterMarker
	SubtitleEvents  []model.SubtitleEvent
}

func CompileTimeline(conf *model.Project, allWordTimings []model.WordTiming, pLines []model.ProcessedLine, exactDuration float64, audioTmp string) (*TimelineCompilation, error) {
	timeline := &model.Timeline{
		Words:     allWordTimings,
		AudioPath: audioTmp,
		Duration:  exactDuration,
	}

	var textJobs []TextRenderJob
	textAssetCount := 0

	var genJobs []GenRenderJob
	genAssetCount := 0

	var styleKeyframes []StyleKeyframe
	var chapters []ChapterMarker
	var subtitleEvents []model.SubtitleEvent

	currentStyle := "whiteboard"
	gridIndex := 0
	currentZoomFocus := "reset"
	marginX := int(float64(conf.Width) * 0.05)
	marginY := int(float64(conf.Height) * 0.05)
	colWidth := (conf.Width - 2*marginX) / 3
	rowHeight := (conf.Height - 2*marginY) / 2

	// Pre-pass: collect zoom transition windows so draw reveals can be delayed until
	// the camera has settled. Must match renderer.go's transitionDuration (1 second).
	zoomTransFrames := int(1.0 * float64(conf.FPS))
	type zoomInterval struct{ start, end int }
	var zoomIntervals []zoomInterval
	for _, pl := range pLines {
		for _, action := range pl.Actions {
			if !strings.HasPrefix(action.Tag, "zoom:") {
				continue
			}
			zt := pl.StartTime
			if action.WordIndex > 0 {
				idx := pl.WordOffset + action.WordIndex - 1
				if idx >= 0 && idx < len(allWordTimings) {
					zt = allWordTimings[idx].Start // match renderer.go: always .Start for zooms
				}
			}
			zf := int(zt * float64(conf.FPS))
			zoomIntervals = append(zoomIntervals, zoomInterval{zf, zf + zoomTransFrames})
		}
	}

	// adjustForZoom pushes startFrame past any active camera-transition window.
	// Handles cascading zooms by re-checking until stable.
	adjustForZoom := func(sf int) int {
		changed := true
		for changed {
			changed = false
			for _, zi := range zoomIntervals {
				if sf >= zi.start && sf < zi.end {
					sf = zi.end
					changed = true
					break
				}
			}
		}
		return sf
	}

	for _, pl := range pLines {
		for _, action := range pl.Actions {
			// Find trigger time
			triggerTime := pl.StartTime
			if action.WordIndex > 0 {
				triggerWordIdx := pl.WordOffset + action.WordIndex - 1
				if triggerWordIdx >= 0 && triggerWordIdx < len(allWordTimings) {
					if action.TriggerAfterWord {
						triggerTime = allWordTimings[triggerWordIdx].End
					} else {
						triggerTime = allWordTimings[triggerWordIdx].Start
					}
				} else {
					log.Printf("Warning: WordIndex %d OOB for line starting at %.2fs", action.WordIndex, pl.StartTime)
				}
			}

			rawStartFrame := int(triggerTime * float64(conf.FPS))

			// Handle custom duration parameters or defaults
			revealDuration := 2.0
			actionTag := action.Tag
			preset := ""

			isSpecialPrefix := false
			specialPrefixes := []string{"WAIT:", "zoom:", "style:", "chapter:", "sfx:", "subtitle:", "text:", "erase:", "move:", "gen:", "slide:", "lower3rd:", "arrow:", "highlight:", "compare:", "transition:", "overlay:", "counter:"}
			for _, prefix := range specialPrefixes {
				if strings.HasPrefix(actionTag, prefix) {
					isSpecialPrefix = true
					break
				}
			}

			if !isSpecialPrefix {
				parts := strings.Split(actionTag, ":")
				if len(parts) > 1 {
					if dur, err := strconv.ParseFloat(parts[len(parts)-1], 64); err == nil {
						revealDuration = dur
						parts = parts[:len(parts)-1]
					}
				}
				if len(parts) > 0 {
					actionTag = parts[0]
				}
				if len(parts) > 1 {
					preset = parts[1]
				}
			}

			// For draw/gen/text reveals: delay past any concurrent zoom transition so
			// the hand-draw animation never plays while the camera is still panning.
			// zoom/style/erase/move events keep their raw startFrame.
			startFrame := rawStartFrame
			if !isSpecialPrefix || strings.HasPrefix(actionTag, "gen:") || strings.HasPrefix(actionTag, "text:") {
				startFrame = adjustForZoom(rawStartFrame)
			}

			endFrame := startFrame + int(revealDuration*float64(conf.FPS))

			if strings.HasPrefix(action.Tag, "WAIT:") {
				continue
			}

			if strings.HasPrefix(action.Tag, "zoom:") {
				currentZoomFocus = strings.TrimPrefix(action.Tag, "zoom:")
				continue
			}

			if strings.HasPrefix(action.Tag, "style:") {
				styleName := strings.TrimPrefix(action.Tag, "style:")
				currentStyle = styleName
				styleKeyframes = append(styleKeyframes, StyleKeyframe{
					Frame: startFrame,
					Style: styleName,
				})
				continue
			}

			if strings.HasPrefix(action.Tag, "chapter:") {
				title := strings.TrimPrefix(action.Tag, "chapter:")
				title = strings.Trim(title, "\"")
				chapters = append(chapters, ChapterMarker{
					StartTime: triggerTime,
					Title:     title,
				})
				continue
			}

			if strings.HasPrefix(action.Tag, "subtitle:") {
				state := strings.TrimPrefix(action.Tag, "subtitle:")
				subtitleEvents = append(subtitleEvents, model.SubtitleEvent{
					Time:  triggerTime,
					State: state,
				})
				continue
			}

			if strings.HasPrefix(action.Tag, "sfx:") {
				continue
			}

			if strings.HasPrefix(action.Tag, "text:") {
				rest := strings.TrimPrefix(action.Tag, "text:")
				firstQuote := strings.Index(rest, "\"")
				lastQuote := strings.LastIndex(rest, "\"")
				if firstQuote != -1 && lastQuote != -1 && lastQuote > firstQuote {
					content := rest[firstQuote+1 : lastQuote]
					remainder := rest[lastQuote+1:]
					
					preset := ""
					fontFamily := "sans"
					fontSize := 48.0
					fontWeight := "regular"
					
					parts := strings.Split(remainder, ":")
					if len(parts) > 1 {
						preset = parts[1]
					}
					if len(parts) > 2 {
						fontFamily = parts[2]
					}
					if len(parts) > 3 {
						if sz, err := strconv.ParseFloat(parts[3], 64); err == nil {
							fontSize = sz
						}
					}
					if len(parts) > 4 {
						fontWeight = parts[4]
					}

					textAssetID := fmt.Sprintf("__text_%d", textAssetCount)
					textAssetCount++

					textJobs = append(textJobs, TextRenderJob{
						AssetID:    textAssetID,
						Content:    content,
						FontFamily: fontFamily,
						FontSize:   fontSize,
						IsBold:     fontWeight == "bold",
						Style:      currentStyle,
					})

					tx, ty, tw, th := action.X, action.Y, action.W, action.H
					if preset != "" && tx == 0 && ty == 0 && tw == 0 && th == 0 {
						px, py, pw, ph := getPresetLayout(preset, conf.Width, conf.Height)
						padW := int(float64(pw) * 0.1)
						padH := int(float64(ph) * 0.1)
						tx = px + padW
						ty = py + padH
						tw = pw - 2*padW
						th = ph - 2*padH
					}

					evFocus := preset
					if evFocus == "" {
						evFocus = currentZoomFocus
					}

					event := model.FrameEvent{
						TargetImage: textAssetID,
						StartFrame:  startFrame,
						EndFrame:    endFrame,
						X:           tx,
						Y:           ty,
						Width:       tw,
						Height:      th,
						EventType:   "text",
						MaskStyle:   "ltr",
						HandStyle:   "marker",
						ZoomFocus:   evFocus,
					}
					timeline.Events = append(timeline.Events, event)
					// Persist text on screen after reveal animation
					timeline.Events = append(timeline.Events, model.FrameEvent{
						TargetImage: textAssetID,
						StartFrame:  endFrame,
						EndFrame:    999999,
						X: tx, Y: ty, Width: tw, Height: th,
						EventType:   "static",
						ZoomFocus:   evFocus,
					})
				}
				continue
			}

			if action.Tag == "erase:*" {
				clearFrame := startFrame
				var activeEvents []model.FrameEvent
				for _, ev := range timeline.Events {
					if ev.StartFrame >= clearFrame {
						continue
					}
					if ev.EndFrame > clearFrame {
						ev.EndFrame = clearFrame
					}
					activeEvents = append(activeEvents, ev)
				}
				timeline.Events = activeEvents
				gridIndex = 0
				continue
			}

		if strings.HasPrefix(action.Tag, "erase:") {
			targetAsset := strings.TrimPrefix(action.Tag, "erase:")
			eraseEvent := model.FrameEvent{
				TargetImage: targetAsset,
				StartFrame:  startFrame,
				EndFrame:    endFrame,
				EventType:   "erase",
				HandStyle:   "eraser",
				MaskStyle:   "ttb",
				ZoomFocus:   currentZoomFocus,
			}
				
				found := false
				for i := len(timeline.Events) - 1; i >= 0; i-- {
					if timeline.Events[i].TargetImage == targetAsset && (timeline.Events[i].EventType == "draw" || timeline.Events[i].EventType == "text" || timeline.Events[i].EventType == "gen" || timeline.Events[i].EventType == "static") {
						eraseEvent.X = timeline.Events[i].X
						eraseEvent.Y = timeline.Events[i].Y
						eraseEvent.Width = timeline.Events[i].Width
						eraseEvent.Height = timeline.Events[i].Height
						eraseEvent.ZoomFocus = timeline.Events[i].ZoomFocus
						if timeline.Events[i].EndFrame > startFrame {
							timeline.Events[i].EndFrame = startFrame
						}
						found = true
						break
					}
				}
				if !found {
					log.Printf("Warning: [erase:%s] cannot find active asset to erase; skipping", targetAsset)
					continue
				}
				timeline.Events = append(timeline.Events, eraseEvent)
				continue
			}

			if strings.HasPrefix(action.Tag, "move:") {
				parts := strings.Split(strings.TrimPrefix(action.Tag, "move:"), ":")
				targetAsset := parts[0]
				destPreset := ""
				if len(parts) > 1 {
					destPreset = parts[1]
				}

				var startX, startY int
				var startW, startH int
				found := false
				var evFocus string = currentZoomFocus
				for i := len(timeline.Events) - 1; i >= 0; i-- {
					if timeline.Events[i].TargetImage == targetAsset {
						if timeline.Events[i].EventType == "move" {
							startX = timeline.Events[i].DestX
							startY = timeline.Events[i].DestY
						} else {
							startX = timeline.Events[i].X
							startY = timeline.Events[i].Y
						}
						startW = timeline.Events[i].Width
						startH = timeline.Events[i].Height
						evFocus = timeline.Events[i].ZoomFocus
						found = true
						if timeline.Events[i].EndFrame > startFrame {
							timeline.Events[i].EndFrame = startFrame
						}
						break
					}
				}

				if found {
					destX, destY := startX, startY
					moveFocus := evFocus
					if destPreset != "" {
						px, py, pw, ph := getPresetLayout(destPreset, conf.Width, conf.Height)
						padW := int(float64(pw) * 0.1)
						padH := int(float64(ph) * 0.1)
						destX = px + padW
						destY = py + padH
						startW = pw - 2*padW
						startH = ph - 2*padH
						moveFocus = destPreset
					} else if action.X != 0 || action.Y != 0 {
						destX = action.X
						destY = action.Y
						if action.W != 0 && action.H != 0 {
							startW = action.W
							startH = action.H
						}
					}

					moveEvent := model.FrameEvent{
						TargetImage: targetAsset,
						StartFrame:  startFrame,
						EndFrame:    endFrame,
						EventType:   "move",
						X:           startX,
						Y:           startY,
						Width:       startW,
						Height:      startH,
						DestX:       destX,
						DestY:       destY,
						HandStyle:   "pencil",
						ZoomFocus:   moveFocus,
					}
					timeline.Events = append(timeline.Events, moveEvent)

					staticDrawEvent := model.FrameEvent{
						TargetImage: targetAsset,
						StartFrame:  endFrame,
						EndFrame:    999999,
						X:           destX,
						Y:           destY,
						Width:       startW,
						Height:      startH,
						EventType:   "static",
						ZoomFocus:   moveFocus,
					}
					timeline.Events = append(timeline.Events, staticDrawEvent)
				}
				continue
			}

			if strings.HasPrefix(action.Tag, "gen:") {
				rest := strings.TrimPrefix(action.Tag, "gen:")
				parts := strings.Split(rest, ":")
				prompt := parts[0]
				preset := ""
				if len(parts) > 1 {
					preset = parts[1]
				}

				genAssetID := fmt.Sprintf("__gen_%d", genAssetCount)
				genAssetCount++

				genJobs = append(genJobs, GenRenderJob{
					AssetID: genAssetID,
					Prompt:  prompt,
				})

				tx, ty, tw, th := action.X, action.Y, action.W, action.H
				if preset != "" && tx == 0 && ty == 0 && tw == 0 && th == 0 {
					px, py, pw, ph := getPresetLayout(preset, conf.Width, conf.Height)
					padW := int(float64(pw) * 0.1)
					padH := int(float64(ph) * 0.1)
					tx = px + padW
					ty = py + padH
					tw = pw - 2*padW
					th = ph - 2*padH
				}

				genFocus := preset
				if genFocus == "" {
					genFocus = currentZoomFocus
				}

				event := model.FrameEvent{
					TargetImage: genAssetID,
					StartFrame:  startFrame,
					EndFrame:    endFrame,
					X:           tx,
					Y:           ty,
					Width:       tw,
					Height:      th,
					EventType:   "draw",
					MaskStyle:   "diagonal",
					HandStyle:   "pencil",
					ZoomFocus:   genFocus,
				}
				timeline.Events = append(timeline.Events, event)
				// Persist generated image on screen after reveal
				timeline.Events = append(timeline.Events, model.FrameEvent{
					TargetImage: genAssetID,
					StartFrame:  endFrame,
					EndFrame:    999999,
					X: tx, Y: ty, Width: tw, Height: th,
					EventType:   "static",
					ZoomFocus:   genFocus,
				})
				continue
			}

			if action.Tag == "clear" {
				clearFrame := startFrame
				var activeEvents []model.FrameEvent
				for _, ev := range timeline.Events {
					if ev.StartFrame >= clearFrame {
						continue
					}
					if ev.EndFrame > clearFrame {
						ev.EndFrame = clearFrame
					}
					activeEvents = append(activeEvents, ev)
				}
				timeline.Events = activeEvents
				gridIndex = 0
				continue
			}

		if strings.HasPrefix(action.Tag, "slide:") {
			rest := strings.TrimPrefix(action.Tag, "slide:")
			parts := strings.Split(rest, ":")
			asset := parts[0]
			preset := ""
			transition := "none"
			fitMode := "fit"
			if len(parts) > 1 && parts[1] != "" {
				preset = parts[1]
			}
			if len(parts) > 2 && parts[2] != "" {
				transition = parts[2]
			}
			if len(parts) > 3 && parts[3] != "" {
				fitMode = parts[3]
			}

			sx, sy, sw, sh := action.X, action.Y, action.W, action.H
			if sw == 0 && sh == 0 {
				sw = conf.Width
				sh = conf.Height
			}
			if preset != "" && sx == 0 && sy == 0 {
				px, py, pw, ph := getPresetLayout(preset, conf.Width, conf.Height)
				sx, sy = px, py
				sw, sh = pw, ph
			}
			if sx == 0 && sy == 0 && sw == 0 && sh == 0 {
				sx, sy, sw, sh = 0, 0, conf.Width, conf.Height
			}

			slideFocus := preset
			if slideFocus == "" {
				slideFocus = currentZoomFocus
			}
			timeline.Events = append(timeline.Events, model.FrameEvent{
				TargetImage: asset,
				StartFrame:  startFrame,
				EndFrame:    endFrame,
				X:           sx, Y: sy, Width: sw, Height: sh,
				EventType:   "slide",
				ZoomFocus:   slideFocus,
				Transition:  transition,
				FitMode:     fitMode,
			})
			timeline.Events = append(timeline.Events, model.FrameEvent{
				TargetImage: asset,
				StartFrame:  endFrame,
				EndFrame:    999999,
				X:           sx, Y: sy, Width: sw, Height: sh,
				EventType:   "slide",
				ZoomFocus:   slideFocus,
				Transition:  "none",
				FitMode:     fitMode,
			})
			continue
		}

		if strings.HasPrefix(action.Tag, "lower3rd:") {
			rest := strings.TrimPrefix(action.Tag, "lower3rd:")
			parts := strings.Split(rest, ":")
			title := ""
			subtitle := ""
			duration := 4.0
			colorHex := ""

			if len(parts) > 0 {
				title = unquote(parts[0])
			}
			if len(parts) > 1 {
				subtitle = unquote(parts[1])
			}
			for i := 2; i < len(parts); i++ {
				part := unquote(parts[i])
				if val, err := strconv.ParseFloat(part, 64); err == nil {
					duration = val
				} else {
					colorHex = part
				}
			}

			if strings.HasSuffix(action.Tag, "+") {
				continue
			}
			targetID := fmt.Sprintf("__lower3rd_%s|%s|%s", title, subtitle, colorHex)
			end := startFrame + int(duration*float64(conf.FPS))
			timeline.Events = append(timeline.Events, model.FrameEvent{
				TargetImage: targetID,
				StartFrame:  startFrame,
				EndFrame:    end,
				EventType:   "lower3rd",
				ZoomFocus:   currentZoomFocus,
			})
			continue
		}

		if strings.HasPrefix(action.Tag, "arrow:") {
			rest := strings.TrimPrefix(action.Tag, "arrow:")
			parts := strings.Split(rest, ":")
			from := parts[0]
			to := parts[1]
			style := "straight"
			duration := 1.0
			if len(parts) > 2 && parts[2] != "" {
				style = parts[2]
			}
			if len(parts) > 3 && parts[3] != "" {
				if d, err := strconv.ParseFloat(parts[3], 64); err == nil {
					duration = d
				}
			}

			end := startFrame + int(duration*float64(conf.FPS))
			timeline.Events = append(timeline.Events, model.FrameEvent{
				StartFrame: startFrame,
				EndFrame:   end,
				EventType:  "arrow",
				ArrowFrom:  from,
				ArrowTo:    to,
				ArrowStyle: style,
				ZoomFocus:  currentZoomFocus,
			})
			timeline.Events = append(timeline.Events, model.FrameEvent{
				StartFrame: end,
				EndFrame:   999999,
				EventType:  "arrow_static",
				ArrowFrom:  from,
				ArrowTo:    to,
				ArrowStyle: style,
				ZoomFocus:  currentZoomFocus,
			})
			continue
		}

		if strings.HasPrefix(action.Tag, "highlight:") {
			rest := strings.TrimPrefix(action.Tag, "highlight:")
			parts := strings.Split(rest, ":")
			region := parts[0]
			style := "rect"
			duration := 2.0
			if len(parts) > 1 && parts[1] != "" {
				style = parts[1]
			}
			if len(parts) > 2 && parts[2] != "" {
				if d, err := strconv.ParseFloat(parts[2], 64); err == nil {
					duration = d
				}
			}

			end := startFrame + int(duration*float64(conf.FPS))
			timeline.Events = append(timeline.Events, model.FrameEvent{
				StartFrame:     startFrame,
				EndFrame:       end,
				EventType:      "highlight",
				TargetImage:    region,
				HighlightStyle: style,
				ZoomFocus:      currentZoomFocus,
			})
			continue
		}

		if strings.HasPrefix(action.Tag, "compare:") {
			rest := strings.TrimPrefix(action.Tag, "compare:")
			parts := strings.Split(rest, ":")
			left := parts[0]
			right := parts[1]
			lblLeft := ""
			lblRight := ""
			if len(parts) > 2 {
				lblLeft = unquote(parts[2])
			}
			if len(parts) > 3 {
				lblRight = unquote(parts[3])
			}

			timeline.Events = append(timeline.Events, model.FrameEvent{
				StartFrame:   startFrame,
				EndFrame:     endFrame,
				EventType:    "compare",
				CompareLeft:  left,
				CompareRight: right,
				LabelLeft:    lblLeft,
				LabelRight:   lblRight,
				ZoomFocus:    currentZoomFocus,
			})
			timeline.Events = append(timeline.Events, model.FrameEvent{
				StartFrame:   endFrame,
				EndFrame:     999999,
				EventType:    "compare",
				CompareLeft:  left,
				CompareRight: right,
				LabelLeft:    lblLeft,
				LabelRight:   lblRight,
				ZoomFocus:    currentZoomFocus,
			})
			continue
		}

		if strings.HasPrefix(action.Tag, "overlay:") {
			rest := strings.TrimPrefix(action.Tag, "overlay:")
			parts := strings.Split(rest, ":")
			asset := parts[0]
			opacity := 0.5
			preset := "fullscreen"
			if len(parts) > 1 && parts[1] != "" {
				if op, err := strconv.ParseFloat(parts[1], 64); err == nil {
					opacity = op
				}
			}
			if len(parts) > 2 && parts[2] != "" {
				preset = parts[2]
			}

			timeline.Events = append(timeline.Events, model.FrameEvent{
				TargetImage: asset,
				StartFrame:  startFrame,
				EndFrame:    999999,
				EventType:   "overlay",
				Opacity:     opacity,
				ZoomFocus:   preset,
			})
			continue
		}

		if strings.HasPrefix(action.Tag, "transition:") {
			rest := strings.TrimPrefix(action.Tag, "transition:")
			parts := strings.Split(rest, ":")
			tType := parts[0]
			duration := 0.5
			if len(parts) > 1 && parts[1] != "" {
				if d, err := strconv.ParseFloat(parts[1], 64); err == nil {
					duration = d
				}
			}

			end := startFrame + int(duration*float64(conf.FPS))
			midpoint := startFrame + int(duration*float64(conf.FPS))/2

			// Truncate all active events at the midpoint of the transition
			var activeEvents []model.FrameEvent
			for _, ev := range timeline.Events {
				if ev.StartFrame >= midpoint {
					continue
				}
				if ev.EndFrame > midpoint {
					ev.EndFrame = midpoint
				}
				activeEvents = append(activeEvents, ev)
			}
			timeline.Events = activeEvents
			gridIndex = 0

			timeline.Events = append(timeline.Events, model.FrameEvent{
				StartFrame:     startFrame,
				EndFrame:       end,
				EventType:      "transition",
				TransitionType: tType,
				ZoomFocus:      currentZoomFocus,
			})
			continue
		}

		if strings.HasPrefix(action.Tag, "counter:") {
			rest := strings.TrimPrefix(action.Tag, "counter:")
			parts := strings.Split(rest, ":")
			cStart := 0.0
			cEnd := 0.0
			duration := 2.0
			format := "%d"
			preset := "center"

			if len(parts) > 0 {
				cStart, _ = strconv.ParseFloat(parts[0], 64)
			}
			if len(parts) > 1 {
				cEnd, _ = strconv.ParseFloat(parts[1], 64)
			}
			if len(parts) > 2 {
				duration, _ = strconv.ParseFloat(parts[2], 64)
			}
			if len(parts) > 3 {
				format = parts[3]
			}
			if len(parts) > 4 {
				preset = parts[4]
			}

			end := startFrame + int(duration*float64(conf.FPS))
			timeline.Events = append(timeline.Events, model.FrameEvent{
				StartFrame:    startFrame,
				EndFrame:      end,
				EventType:     "counter",
				CounterStart:  cStart,
				CounterEnd:    cEnd,
				CounterFormat: format,
				ZoomFocus:     preset,
			})
			timeline.Events = append(timeline.Events, model.FrameEvent{
				StartFrame:    end,
				EndFrame:      999999,
				EventType:     "counter",
				CounterStart:  cEnd,
				CounterEnd:    cEnd,
				CounterFormat: format,
				ZoomFocus:     preset,
			})
			continue
		}

		x, y := action.X, action.Y
		w, h := action.W, action.H

			if preset != "" && x == 0 && y == 0 && w == 0 && h == 0 {
				px, py, pw, ph := getPresetLayout(preset, conf.Width, conf.Height)
				padW := int(float64(pw) * 0.1)
				padH := int(float64(ph) * 0.1)
				w = pw - 2*padW
				h = ph - 2*padH
				x = px + padW
				y = py + padH
			} else if x == 0 && y == 0 {
				col := gridIndex % 3
				row := (gridIndex / 3) % 2
				cellX := marginX + col*colWidth
				cellY := marginY + row*rowHeight

				if w == 0 && h == 0 {
					w = int(float64(colWidth) * 0.8)
					h = int(float64(rowHeight) * 0.8)
				}

				x = cellX + (colWidth-w)/2
				y = cellY + (rowHeight-h)/2
				gridIndex++
			}

			drawFocus := preset
			if drawFocus == "" {
				drawFocus = currentZoomFocus
			}

			// Reveal animation event
			timeline.Events = append(timeline.Events, model.FrameEvent{
				TargetImage: actionTag,
				StartFrame:  startFrame,
				EndFrame:    endFrame,
				X:           x,
				Y:           y,
				Width:       w,
				Height:      h,
				EventType:   "draw",
				MaskStyle:   "diagonal",
				HandStyle:   "pencil",
				ZoomFocus:   drawFocus,
			})
			// Persistence event: image stays on screen after reveal
			timeline.Events = append(timeline.Events, model.FrameEvent{
				TargetImage: actionTag,
				StartFrame:  endFrame,
				EndFrame:    999999,
				X:           x,
				Y:           y,
				Width:       w,
				Height:      h,
				EventType:   "static",
				ZoomFocus:   drawFocus,
			})
		}
	}

	// Validate Assets exist upfront
	assetIndex, _ := assets.LoadIndex(conf.AssetsDir)
	assetMap := make(map[string]assets.AssetEntry)
	if assetIndex != nil {
		for _, a := range assetIndex.Assets {
			assetMap[a.ID] = a
		}
	}

	var missingAssets []string
	seenAssets := make(map[string]bool)
	for _, ev := range timeline.Events {
		if ev.EventType == "compare" {
			for _, imgID := range []string{ev.CompareLeft, ev.CompareRight} {
				if imgID == "" || seenAssets[imgID] {
					continue
				}
				seenAssets[imgID] = true
				var assetPath string
				if entry, ok := assetMap[imgID]; ok {
					assetPath = filepath.Join(conf.AssetsDir, entry.File)
				} else {
					assetPath = filepath.Join(conf.AssetsDir, imgID+".png")
				}
				if _, err := os.Stat(assetPath); os.IsNotExist(err) {
					missingAssets = append(missingAssets, imgID)
				}
			}
			continue
		}
		if ev.TargetImage == "" || ev.TargetImage == "clear" || strings.HasPrefix(ev.TargetImage, "__text_") || strings.HasPrefix(ev.TargetImage, "__gen_") || strings.HasPrefix(ev.TargetImage, "__lower3rd_") {
			continue
		}
		if ev.EventType == "arrow" || ev.EventType == "arrow_static" || ev.EventType == "highlight" || ev.EventType == "counter" || ev.EventType == "transition" {
			continue
		}
		if seenAssets[ev.TargetImage] {
			continue
		}
		seenAssets[ev.TargetImage] = true
		
		var assetPath string
		if entry, ok := assetMap[ev.TargetImage]; ok {
			assetPath = filepath.Join(conf.AssetsDir, entry.File)
			if entry.HasBg {
				log.Printf("Warning: Asset %q is marked as having a background (has_bg: true). It is recommended to run background removal processing first.", ev.TargetImage)
			}
		} else {
			assetPath = filepath.Join(conf.AssetsDir, ev.TargetImage+".png")
		}

		if _, err := os.Stat(assetPath); os.IsNotExist(err) {
			missingAssets = append(missingAssets, ev.TargetImage)
		}
	}

	if len(missingAssets) > 0 {
		return nil, fmt.Errorf("missing asset files in %s: %s (please make sure they exist as .png files)", conf.AssetsDir, strings.Join(missingAssets, ", "))
	}

	for _, ev := range timeline.Events {
		if ev.EventType == "erase" {
			if _, hasAsset := assetMap[ev.TargetImage]; !hasAsset {
				assetPath := filepath.Join(conf.AssetsDir, ev.TargetImage+".png")
				if _, err := os.Stat(assetPath); os.IsNotExist(err) {
					log.Printf("Warning: [erase:%s] erasing an asset that was not placed on screen; check asset name", ev.TargetImage)
				}
			}
		}
	}

	return &TimelineCompilation{
		Timeline:       timeline,
		TextJobs:       textJobs,
		GenJobs:        genJobs,
		StyleKeyframes: styleKeyframes,
		Chapters:       chapters,
		SubtitleEvents: subtitleEvents,
	}, nil
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func getPresetLayout(preset string, canvasW, canvasH int) (x, y, w, h int) {
	halfW := canvasW / 2
	halfH := canvasH / 2
	switch preset {
	case "TL":
		return 0, 0, halfW, halfH
	case "TR":
		return halfW, 0, halfW, halfH
	case "BL":
		return 0, halfH, halfW, halfH
	case "BR":
		return halfW, halfH, halfW, halfH
	case "HT":
		return 0, 0, canvasW, halfH
	case "HB":
		return 0, halfH, canvasW, halfH
	case "LH":
		return 0, 0, halfW, canvasH
	case "RH":
		return halfW, 0, halfW, canvasH
	default:
		return 0, 0, canvasW, canvasH
	}
}

type PaintGenRequest struct {
	Prompt string `json:"prompt"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Steps  int    `json:"steps,omitempty"`
}

type PaintGenResponse struct {
	Path string `json:"path"`
}

func GeneratePaintAsset(prompt string) (image.Image, error) {
	reqBody, err := json.Marshal(PaintGenRequest{
		Prompt: prompt,
		Width:  512,
		Height: 512,
		Steps:  4,
	})
	if err != nil {
		return nil, err
	}

	resp, err := http.Post("http://localhost:8765/paint/generate", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var genResp PaintGenResponse
	if err := json.Unmarshal(body, &genResp); err != nil {
		return nil, err
	}

	f, err := os.Open(genResp.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	return img, err
}

func PrepareAssets(conf *model.Project, engine *render.Engine, timeline *model.Timeline, textJobs []TextRenderJob, genJobs []GenRenderJob) error {
	assetIndex, _ := assets.LoadIndex(conf.AssetsDir)
	assetMap := make(map[string]assets.AssetEntry)
	if assetIndex != nil {
		for _, a := range assetIndex.Assets {
			assetMap[a.ID] = a
		}
	}

	// Load standard assets
	fmt.Println("Loading assets...")
	seenAssets := make(map[string]bool)
	for _, ev := range timeline.Events {
		if ev.EventType == "compare" {
			for _, imgID := range []string{ev.CompareLeft, ev.CompareRight} {
				if imgID == "" || seenAssets[imgID] {
					continue
				}
				seenAssets[imgID] = true
				var assetPath string
				if entry, ok := assetMap[imgID]; ok {
					assetPath = filepath.Join(conf.AssetsDir, entry.File)
				} else {
					assetPath = filepath.Join(conf.AssetsDir, imgID+".png")
				}
				err := engine.LoadAsset(imgID, assetPath)
				if err != nil {
					log.Printf("Warning: Could not load compare asset %s: %v", imgID, err)
				}
			}
			continue
		}
		if ev.TargetImage == "" || ev.TargetImage == "clear" || strings.HasPrefix(ev.TargetImage, "__text_") || strings.HasPrefix(ev.TargetImage, "__gen_") || strings.HasPrefix(ev.TargetImage, "__lower3rd_") {
			continue
		}
		if ev.EventType == "arrow" || ev.EventType == "arrow_static" || ev.EventType == "highlight" || ev.EventType == "counter" || ev.EventType == "transition" {
			continue
		}
		if seenAssets[ev.TargetImage] {
			continue
		}
		seenAssets[ev.TargetImage] = true
		
		var assetPath string
		if entry, ok := assetMap[ev.TargetImage]; ok {
			assetPath = filepath.Join(conf.AssetsDir, entry.File)
		} else {
			assetPath = filepath.Join(conf.AssetsDir, ev.TargetImage+".png")
		}

		err := engine.LoadAsset(ev.TargetImage, assetPath)
		if err != nil {
			log.Printf("Warning: Could not load asset %s: %v", ev.TargetImage, err)
		}
	}

	// Render and load all text assets
	for _, job := range textJobs {
		textColor := color.RGBA{R: 0, G: 0, B: 0, A: 255}
		if job.Style == "blackboard" {
			textColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
		}
		img, err := render.RenderText(job.Content, job.FontFamily, job.FontSize, job.IsBold, textColor)
		if err != nil {
			log.Printf("Warning: failed to render text %q: %v", job.Content, err)
			continue
		}
		engine.RegisterAsset(job.AssetID, img)
	}

	// Generate and load all neural paint assets
	for _, job := range genJobs {
		fmt.Printf("Generating paint asset for prompt %q...\n", job.Prompt)
		img, err := GeneratePaintAsset(job.Prompt)
		if err != nil {
			log.Printf("Warning: Paint generation failed for %q: %v. Using transparent 1x1 placeholder.", job.Prompt, err)
			placeholder := image.NewRGBA(image.Rect(0, 0, 1, 1))
			engine.RegisterAsset(job.AssetID, placeholder)
		} else {
			engine.RegisterAsset(job.AssetID, img)
		}
	}

	return nil
}
