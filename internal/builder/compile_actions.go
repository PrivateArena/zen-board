package builder

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"zen-board/internal/constant"
	"zen-board/internal/model"
)

// actionContext carries the values shared by every action-tag handler that
// CompileTimeline computes once per action (trigger time, frame bounds, parsed
// tag). Handlers must not mutate the shared compiler state via this context.
type actionContext struct {
	pl             *model.ProcessedLine
	action         *model.DrawAction
	triggerTime    float64
	rawStartFrame  int
	startFrame     int
	endFrame       int
	actionTag      string
	preset         string
	revealDuration float64
}

// isSpecialAction reports whether the raw action tag belongs to the set of
// reserved command prefixes. Special actions bypass the generic
// "<asset>:<preset>:<duration>" tag parsing performed for draw actions.
func isSpecialAction(tag string) bool {
	specialPrefixes := []string{constant.TAG_WAIT, constant.TAG_ZOOM, constant.TAG_STYLE, constant.TAG_CHAPTER, constant.TAG_SFX, constant.TAG_SUBTITLE, constant.TAG_TEXT, constant.TAG_ERASE, constant.TAG_MOVE, constant.TAG_GEN, constant.TAG_SLIDE, constant.TAG_BANNER, constant.TAG_ARROW, constant.TAG_HIGHLIGHT, constant.TAG_COMPARE, constant.TAG_TRANSITION, constant.TAG_OVERLAY, constant.TAG_COUNTER}
	for _, prefix := range specialPrefixes {
		if strings.HasPrefix(tag, prefix) {
			return true
		}
	}
	return false
}

// dispatch routes an action tag to its dedicated handler. The case order
// mirrors the original if/continue chain and is significant: the exact-match
// directives (erase:*, clear) must be evaluated before their prefix siblings.
func (c *timelineCompiler) dispatch(ctx *actionContext) {
	tag := ctx.action.Tag
	switch {
	case strings.HasPrefix(tag, constant.TAG_WAIT):
		// compile-time-only directive; nothing is emitted to the timeline
	case strings.HasPrefix(tag, constant.TAG_ZOOM):
		c.handleZoom(ctx)
	case strings.HasPrefix(tag, constant.TAG_STYLE):
		c.handleStyle(ctx)
	case strings.HasPrefix(tag, constant.TAG_CHAPTER):
		c.handleChapter(ctx)
	case strings.HasPrefix(tag, constant.TAG_SUBTITLE):
		c.handleSubtitle(ctx)
	case strings.HasPrefix(tag, constant.TAG_SFX):
		// compile-time-only directive; nothing is emitted to the timeline
	case strings.HasPrefix(tag, constant.TAG_TEXT):
		c.handleText(ctx)
	case tag == constant.TAG_ERASE_ALL:
		c.handleEraseAll(ctx)
	case strings.HasPrefix(tag, constant.TAG_ERASE):
		c.handleErase(ctx)
	case strings.HasPrefix(tag, constant.TAG_MOVE):
		c.handleMove(ctx)
	case strings.HasPrefix(tag, constant.TAG_GEN):
		c.handleGen(ctx)
	case tag == constant.TAG_CLEAR:
		c.handleClear(ctx)
	case strings.HasPrefix(tag, constant.TAG_SLIDE):
		c.handleSlide(ctx)
	case strings.HasPrefix(tag, constant.TAG_BANNER):
		c.handleBanner(ctx)
	case strings.HasPrefix(tag, constant.TAG_ARROW):
		c.handleArrow(ctx)
	case strings.HasPrefix(tag, constant.TAG_HIGHLIGHT):
		c.handleHighlight(ctx)
	case strings.HasPrefix(tag, constant.TAG_COMPARE):
		c.handleCompare(ctx)
	case strings.HasPrefix(tag, constant.TAG_OVERLAY):
		c.handleOverlay(ctx)
	case strings.HasPrefix(tag, constant.TAG_TRANSITION):
		c.handleTransition(ctx)
	case strings.HasPrefix(tag, constant.TAG_COUNTER):
		c.handleCounter(ctx)
	default:
		c.handleDraw(ctx)
	}
}

func (c *timelineCompiler) handleZoom(ctx *actionContext) {
	c.currentZoomFocus = strings.TrimPrefix(ctx.action.Tag, constant.TAG_ZOOM)
}

func (c *timelineCompiler) handleStyle(ctx *actionContext) {
	styleName := strings.TrimPrefix(ctx.action.Tag, constant.TAG_STYLE)
	c.currentStyle = styleName
	c.styleKeyframes = append(c.styleKeyframes, StyleKeyframe{
		Frame: ctx.startFrame,
		Style: styleName,
	})
	// Inert marker so the eventlog records the style switch
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame: ctx.startFrame,
		EndFrame:   constant.FRAME_FOREVER,
		EventType:  constant.EVENT_STYLE,
	})
}

func (c *timelineCompiler) handleChapter(ctx *actionContext) {
	title := strings.TrimPrefix(ctx.action.Tag, constant.TAG_CHAPTER)
	title = strings.Trim(title, "\"")
	c.chapters = append(c.chapters, ChapterMarker{
		StartTime: ctx.triggerTime,
		Title:     title,
	})
	// Inert marker so the eventlog records the chapter boundary
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame: ctx.startFrame,
		EndFrame:   constant.FRAME_FOREVER,
		EventType:  constant.EVENT_CHAPTER,
	})
}

func (c *timelineCompiler) handleSubtitle(ctx *actionContext) {
	state := strings.TrimPrefix(ctx.action.Tag, constant.TAG_SUBTITLE)
	c.subtitleEvents = append(c.subtitleEvents, model.SubtitleEvent{
		Time:  ctx.triggerTime,
		State: state,
	})
}

func (c *timelineCompiler) handleText(ctx *actionContext) {
	rest := strings.TrimPrefix(ctx.action.Tag, constant.TAG_TEXT)
	firstQuote := strings.Index(rest, "\"")
	lastQuote := strings.LastIndex(rest, "\"")
	if firstQuote != -1 && lastQuote != -1 && lastQuote > firstQuote {
		content := rest[firstQuote+1 : lastQuote]
		remainder := rest[lastQuote+1:]

		preset := ""
		fontFamily := constant.FONT_SANS
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

		textAssetID := fmt.Sprintf("__text_%d", c.textAssetCount)
		c.textAssetCount++

		c.textJobs = append(c.textJobs, TextRenderJob{
			AssetID:    textAssetID,
			Content:    content,
			FontFamily: fontFamily,
			FontSize:   fontSize,
			IsBold:     fontWeight == "bold",
			Style:      c.currentStyle,
		})

		tx, ty, tw, th := ctx.action.X, ctx.action.Y, ctx.action.W, ctx.action.H
		if preset != "" && tx == 0 && ty == 0 && tw == 0 && th == 0 {
			px, py, pw, ph := model.GetPresetLayout(preset, c.conf.Width, c.conf.Height)
			padW := int(float64(pw) * 0.1)
			padH := int(float64(ph) * 0.1)
			tx = px + padW
			ty = py + padH
			tw = pw - 2*padW
			th = ph - 2*padH
		}

		evFocus := preset
		if evFocus == "" {
			evFocus = c.currentZoomFocus
		}

		event := model.FrameEvent{
			TargetImage: textAssetID,
			StartFrame:  ctx.startFrame,
			EndFrame:    ctx.endFrame,
			X:           tx,
			Y:           ty,
			Width:       tw,
			Height:      th,
			EventType:   constant.EVENT_TEXT,
			MaskStyle:   constant.MASK_LTR,
			HandStyle:   constant.HAND_MARKER,
			ZoomFocus:   evFocus,
		}
		c.timeline.Events = append(c.timeline.Events, event)
		// Persist text on screen after reveal animation
		c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
			TargetImage: textAssetID,
			StartFrame:  ctx.endFrame,
			EndFrame:    constant.FRAME_FOREVER,
			X:           tx, Y: ty, Width: tw, Height: th,
			EventType: constant.EVENT_STATIC,
			ZoomFocus: evFocus,
		})
	}
}

func (c *timelineCompiler) handleEraseAll(ctx *actionContext) {
	clearFrame := ctx.startFrame
	var activeEvents []model.FrameEvent
	for _, ev := range c.timeline.Events {
		if ev.StartFrame >= clearFrame {
			continue
		}
		if ev.EndFrame > clearFrame {
			ev.EndFrame = clearFrame
		}
		activeEvents = append(activeEvents, ev)
	}
	c.timeline.Events = activeEvents
	c.gridIndex = 0
	// Inert marker so the eventlog records the board wipe
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame: ctx.startFrame,
		EndFrame:   constant.FRAME_FOREVER,
		EventType:  constant.EVENT_ERASE_ALL,
	})
}

func (c *timelineCompiler) handleErase(ctx *actionContext) {
	targetAsset := strings.TrimPrefix(ctx.action.Tag, constant.TAG_ERASE)
	eraseEvent := model.FrameEvent{
		TargetImage: targetAsset,
		StartFrame:  ctx.startFrame,
		EndFrame:    ctx.endFrame,
		EventType:   constant.EVENT_ERASE,
		HandStyle:   constant.HAND_ERASER,
		MaskStyle:   constant.MASK_TTB,
		ZoomFocus:   c.currentZoomFocus,
	}

	found := false
	for i := len(c.timeline.Events) - 1; i >= 0; i-- {
		if c.timeline.Events[i].TargetImage == targetAsset && (c.timeline.Events[i].EventType == constant.EVENT_DRAW || c.timeline.Events[i].EventType == constant.EVENT_TEXT || c.timeline.Events[i].EventType == constant.EVENT_GEN || c.timeline.Events[i].EventType == constant.EVENT_STATIC) {
			eraseEvent.X = c.timeline.Events[i].X
			eraseEvent.Y = c.timeline.Events[i].Y
			eraseEvent.Width = c.timeline.Events[i].Width
			eraseEvent.Height = c.timeline.Events[i].Height
			eraseEvent.ZoomFocus = c.timeline.Events[i].ZoomFocus
			if c.timeline.Events[i].EndFrame > ctx.startFrame {
				c.timeline.Events[i].EndFrame = ctx.startFrame
			}
			found = true
			break
		}
	}
	if !found {
		log.Printf("Warning: [erase:%s] cannot find active asset to erase; skipping", targetAsset)
		return
	}
	c.timeline.Events = append(c.timeline.Events, eraseEvent)
}

func (c *timelineCompiler) handleMove(ctx *actionContext) {
	parts := strings.Split(strings.TrimPrefix(ctx.action.Tag, constant.TAG_MOVE), ":")
	targetAsset := parts[0]
	destPreset := ""
	if len(parts) > 1 {
		destPreset = parts[1]
	}

	var startX, startY int
	var startW, startH int
	found := false
	var evFocus string = c.currentZoomFocus
	for i := len(c.timeline.Events) - 1; i >= 0; i-- {
		if c.timeline.Events[i].TargetImage == targetAsset {
			if c.timeline.Events[i].EventType == constant.EVENT_MOVE {
				startX = c.timeline.Events[i].DestX
				startY = c.timeline.Events[i].DestY
			} else {
				startX = c.timeline.Events[i].X
				startY = c.timeline.Events[i].Y
			}
			startW = c.timeline.Events[i].Width
			startH = c.timeline.Events[i].Height
			evFocus = c.timeline.Events[i].ZoomFocus
			found = true
			if c.timeline.Events[i].EndFrame > ctx.startFrame {
				c.timeline.Events[i].EndFrame = ctx.startFrame
			}
			break
		}
	}

	if found {
		destX, destY := startX, startY
		moveFocus := evFocus
		if destPreset != "" {
			px, py, pw, ph := model.GetPresetLayout(destPreset, c.conf.Width, c.conf.Height)
			padW := int(float64(pw) * 0.1)
			padH := int(float64(ph) * 0.1)
			destX = px + padW
			destY = py + padH
			startW = pw - 2*padW
			startH = ph - 2*padH
			moveFocus = destPreset
		} else if ctx.action.X != 0 || ctx.action.Y != 0 {
			destX = ctx.action.X
			destY = ctx.action.Y
			if ctx.action.W != 0 && ctx.action.H != 0 {
				startW = ctx.action.W
				startH = ctx.action.H
			}
		}

		moveEvent := model.FrameEvent{
			TargetImage: targetAsset,
			StartFrame:  ctx.startFrame,
			EndFrame:    ctx.endFrame,
			EventType:   constant.EVENT_MOVE,
			X:           startX,
			Y:           startY,
			Width:       startW,
			Height:      startH,
			DestX:       destX,
			DestY:       destY,
			HandStyle:   constant.HAND_PENCIL,
			ZoomFocus:   moveFocus,
		}
		c.timeline.Events = append(c.timeline.Events, moveEvent)

		staticDrawEvent := model.FrameEvent{
			TargetImage: targetAsset,
			StartFrame:  ctx.endFrame,
			EndFrame:    constant.FRAME_FOREVER,
			X:           destX,
			Y:           destY,
			Width:       startW,
			Height:      startH,
			EventType:   constant.EVENT_STATIC,
			ZoomFocus:   moveFocus,
		}
		c.timeline.Events = append(c.timeline.Events, staticDrawEvent)
	}
}

func (c *timelineCompiler) handleGen(ctx *actionContext) {
	rest := strings.TrimPrefix(ctx.action.Tag, constant.TAG_GEN)
	parts := strings.Split(rest, ":")
	prompt := parts[0]
	preset := ""
	if len(parts) > 1 {
		preset = parts[1]
	}

	genAssetID := fmt.Sprintf("__gen_%d", c.genAssetCount)
	c.genAssetCount++

	c.genJobs = append(c.genJobs, GenRenderJob{
		AssetID: genAssetID,
		Prompt:  prompt,
	})

	tx, ty, tw, th := ctx.action.X, ctx.action.Y, ctx.action.W, ctx.action.H
	if preset != "" && tx == 0 && ty == 0 && tw == 0 && th == 0 {
		px, py, pw, ph := model.GetPresetLayout(preset, c.conf.Width, c.conf.Height)
		padW := int(float64(pw) * 0.1)
		padH := int(float64(ph) * 0.1)
		tx = px + padW
		ty = py + padH
		tw = pw - 2*padW
		th = ph - 2*padH
	}

	genFocus := preset
	if genFocus == "" {
		genFocus = c.currentZoomFocus
	}

	event := model.FrameEvent{
		TargetImage: genAssetID,
		StartFrame:  ctx.startFrame,
		EndFrame:    ctx.endFrame,
		X:           tx,
		Y:           ty,
		Width:       tw,
		Height:      th,
		EventType:   constant.EVENT_DRAW,
		MaskStyle:   constant.MASK_DIAGONAL,
		HandStyle:   constant.HAND_PENCIL,
		ZoomFocus:   genFocus,
	}
	c.timeline.Events = append(c.timeline.Events, event)
	// Persist generated image on screen after reveal
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		TargetImage: genAssetID,
		StartFrame:  ctx.endFrame,
		EndFrame:    constant.FRAME_FOREVER,
		X:           tx, Y: ty, Width: tw, Height: th,
		EventType: constant.EVENT_STATIC,
		ZoomFocus: genFocus,
	})
}

func (c *timelineCompiler) handleClear(ctx *actionContext) {
	clearFrame := ctx.startFrame
	var activeEvents []model.FrameEvent
	for _, ev := range c.timeline.Events {
		if ev.StartFrame >= clearFrame {
			continue
		}
		if ev.EndFrame > clearFrame {
			ev.EndFrame = clearFrame
		}
		activeEvents = append(activeEvents, ev)
	}
	c.timeline.Events = activeEvents
	c.gridIndex = 0
	// Inert marker so the eventlog records the board wipe
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame: ctx.startFrame,
		EndFrame:   constant.FRAME_FOREVER,
		EventType:  constant.EVENT_CLEAR,
	})
}

func (c *timelineCompiler) handleSlide(ctx *actionContext) {
	rest := strings.TrimPrefix(ctx.action.Tag, constant.TAG_SLIDE)
	parts := strings.Split(rest, ":")
	asset := parts[0]
	preset := ""
	transition := constant.TRANSITION_NONE
	fitMode := constant.FIT_FIT
	if len(parts) > 1 && parts[1] != "" {
		preset = parts[1]
	}
	if len(parts) > 2 && parts[2] != "" {
		transition = parts[2]
	}
	if len(parts) > 3 && parts[3] != "" {
		fitMode = parts[3]
	}

	sx, sy, sw, sh := ctx.action.X, ctx.action.Y, ctx.action.W, ctx.action.H
	if sw == 0 && sh == 0 {
		sw = c.conf.Width
		sh = c.conf.Height
	}
	if preset != "" && sx == 0 && sy == 0 {
		px, py, pw, ph := model.GetPresetLayout(preset, c.conf.Width, c.conf.Height)
		sx, sy = px, py
		sw, sh = pw, ph
	}
	if sx == 0 && sy == 0 && sw == 0 && sh == 0 {
		sx, sy, sw, sh = 0, 0, c.conf.Width, c.conf.Height
	}

	slideFocus := preset
	if slideFocus == "" {
		slideFocus = c.currentZoomFocus
	}
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		TargetImage: asset,
		StartFrame:  ctx.startFrame,
		EndFrame:    ctx.endFrame,
		X:           sx, Y: sy, Width: sw, Height: sh,
		EventType:  constant.EVENT_SLIDE,
		ZoomFocus:  slideFocus,
		Transition: transition,
		FitMode:    fitMode,
	})
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		TargetImage: asset,
		StartFrame:  ctx.endFrame,
		EndFrame:    constant.FRAME_FOREVER,
		X:           sx, Y: sy, Width: sw, Height: sh,
		EventType:  constant.EVENT_SLIDE,
		ZoomFocus:  slideFocus,
		Transition: constant.TRANSITION_NONE,
		FitMode:    fitMode,
	})
}

func (c *timelineCompiler) handleBanner(ctx *actionContext) {
	rest := strings.TrimPrefix(ctx.action.Tag, constant.TAG_BANNER)
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

	if strings.HasSuffix(ctx.action.Tag, "+") {
		return
	}
	targetID := fmt.Sprintf("__banner_%s|%s|%s", title, subtitle, colorHex)
	end := ctx.startFrame + int(duration*float64(c.conf.FPS))
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		TargetImage: targetID,
		StartFrame:  ctx.startFrame,
		EndFrame:    end,
		EventType:   constant.EVENT_BANNER,
		ZoomFocus:   c.currentZoomFocus,
	})
}

func (c *timelineCompiler) handleArrow(ctx *actionContext) {
	rest := strings.TrimPrefix(ctx.action.Tag, constant.TAG_ARROW)
	parts := strings.Split(rest, ":")
	from := parts[0]
	to := parts[1]
	style := constant.ARROW_STRAIGHT
	duration := 1.0
	if len(parts) > 2 && parts[2] != "" {
		style = parts[2]
	}
	if len(parts) > 3 && parts[3] != "" {
		if d, err := strconv.ParseFloat(parts[3], 64); err == nil {
			duration = d
		}
	}

	end := ctx.startFrame + int(duration*float64(c.conf.FPS))
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame: ctx.startFrame,
		EndFrame:   end,
		EventType:  constant.EVENT_ARROW,
		ArrowFrom:  from,
		ArrowTo:    to,
		ArrowStyle: style,
		ZoomFocus:  c.currentZoomFocus,
	})
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame: end,
		EndFrame:   constant.FRAME_FOREVER,
		EventType:  constant.EVENT_ARROW_STATIC,
		ArrowFrom:  from,
		ArrowTo:    to,
		ArrowStyle: style,
		ZoomFocus:  c.currentZoomFocus,
	})
}

func (c *timelineCompiler) handleHighlight(ctx *actionContext) {
	rest := strings.TrimPrefix(ctx.action.Tag, constant.TAG_HIGHLIGHT)
	parts := strings.Split(rest, ":")
	region := parts[0]
	style := constant.HIGHLIGHT_RECT
	duration := 2.0
	if len(parts) > 1 && parts[1] != "" {
		style = parts[1]
	}
	if len(parts) > 2 && parts[2] != "" {
		if d, err := strconv.ParseFloat(parts[2], 64); err == nil {
			duration = d
		}
	}

	end := ctx.startFrame + int(duration*float64(c.conf.FPS))
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame:     ctx.startFrame,
		EndFrame:       end,
		EventType:      constant.EVENT_HIGHLIGHT,
		TargetImage:    region,
		HighlightStyle: style,
		ZoomFocus:      c.currentZoomFocus,
	})
}

func (c *timelineCompiler) handleCompare(ctx *actionContext) {
	rest := strings.TrimPrefix(ctx.action.Tag, constant.TAG_COMPARE)
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

	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame:   ctx.startFrame,
		EndFrame:     ctx.endFrame,
		EventType:    constant.EVENT_COMPARE,
		CompareLeft:  left,
		CompareRight: right,
		LabelLeft:    lblLeft,
		LabelRight:   lblRight,
		ZoomFocus:    c.currentZoomFocus,
	})
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame:   ctx.endFrame,
		EndFrame:     constant.FRAME_FOREVER,
		EventType:    constant.EVENT_COMPARE,
		CompareLeft:  left,
		CompareRight: right,
		LabelLeft:    lblLeft,
		LabelRight:   lblRight,
		ZoomFocus:    c.currentZoomFocus,
	})
}

func (c *timelineCompiler) handleOverlay(ctx *actionContext) {
	rest := strings.TrimPrefix(ctx.action.Tag, constant.TAG_OVERLAY)
	parts := strings.Split(rest, ":")
	asset := parts[0]
	opacity := 0.5
	preset := constant.PRESET_FULLSCREEN
	if len(parts) > 1 && parts[1] != "" {
		if op, err := strconv.ParseFloat(parts[1], 64); err == nil {
			opacity = op
		}
	}
	if len(parts) > 2 && parts[2] != "" {
		preset = parts[2]
	}

	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		TargetImage: asset,
		StartFrame:  ctx.startFrame,
		EndFrame:    constant.FRAME_FOREVER,
		EventType:   constant.EVENT_OVERLAY,
		Opacity:     opacity,
		ZoomFocus:   preset,
	})
}

func (c *timelineCompiler) handleTransition(ctx *actionContext) {
	rest := strings.TrimPrefix(ctx.action.Tag, constant.TAG_TRANSITION)
	parts := strings.Split(rest, ":")
	tType := parts[0]
	duration := 0.5
	if len(parts) > 1 && parts[1] != "" {
		if d, err := strconv.ParseFloat(parts[1], 64); err == nil {
			duration = d
		}
	}

	end := ctx.startFrame + int(duration*float64(c.conf.FPS))
	midpoint := ctx.startFrame + int(duration*float64(c.conf.FPS))/2

	// Truncate all active events at the midpoint of the transition
	var activeEvents []model.FrameEvent
	for _, ev := range c.timeline.Events {
		if ev.StartFrame >= midpoint {
			continue
		}
		if ev.EndFrame > midpoint {
			ev.EndFrame = midpoint
		}
		activeEvents = append(activeEvents, ev)
	}
	c.timeline.Events = activeEvents
	c.gridIndex = 0

	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame:     ctx.startFrame,
		EndFrame:       end,
		EventType:      constant.EVENT_TRANSITION,
		TransitionType: tType,
		ZoomFocus:      c.currentZoomFocus,
	})
}

func (c *timelineCompiler) handleCounter(ctx *actionContext) {
	rest := strings.TrimPrefix(ctx.action.Tag, constant.TAG_COUNTER)
	parts := strings.Split(rest, ":")
	cStart := 0.0
	cEnd := 0.0
	duration := 2.0
	format := "%d"
	preset := constant.PRESET_CENTER

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

	end := ctx.startFrame + int(duration*float64(c.conf.FPS))
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame:    ctx.startFrame,
		EndFrame:      end,
		EventType:     constant.EVENT_COUNTER,
		CounterStart:  cStart,
		CounterEnd:    cEnd,
		CounterFormat: format,
		ZoomFocus:     preset,
	})
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame:    end,
		EndFrame:      constant.FRAME_FOREVER,
		EventType:     constant.EVENT_COUNTER,
		CounterStart:  cEnd,
		CounterEnd:    cEnd,
		CounterFormat: format,
		ZoomFocus:     preset,
	})
}

func (c *timelineCompiler) handleDraw(ctx *actionContext) {
	x, y := ctx.action.X, ctx.action.Y
	w, h := ctx.action.W, ctx.action.H

	if ctx.preset != "" && x == 0 && y == 0 && w == 0 && h == 0 {
		px, py, pw, ph := model.GetPresetLayout(ctx.preset, c.conf.Width, c.conf.Height)
		padW := int(float64(pw) * 0.1)
		padH := int(float64(ph) * 0.1)
		w = pw - 2*padW
		h = ph - 2*padH
		x = px + padW
		y = py + padH
	} else if x == 0 && y == 0 {
		col := c.gridIndex % 3
		row := (c.gridIndex / 3) % 2
		cellX := c.marginX + col*c.colWidth
		cellY := c.marginY + row*c.rowHeight

		if w == 0 && h == 0 {
			w = int(float64(c.colWidth) * 0.8)
			h = int(float64(c.rowHeight) * 0.8)
		}

		x = cellX + (c.colWidth-w)/2
		y = cellY + (c.rowHeight-h)/2
		c.gridIndex++
	}

	drawFocus := ctx.preset
	if drawFocus == "" {
		drawFocus = c.currentZoomFocus
	}

	cursorName := extractCursor(ctx.action.AssetVariant, constant.CURSOR_HAND)

	// Reveal animation event
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		TargetImage:  ctx.actionTag,
		StartFrame:   ctx.startFrame,
		EndFrame:     ctx.endFrame,
		X:            x,
		Y:            y,
		Width:        w,
		Height:       h,
		EventType:    constant.EVENT_DRAW,
		MaskStyle:    constant.MASK_DIAGONAL,
		HandStyle:    constant.HAND_PENCIL,
		Cursor:       cursorName,
		ZoomFocus:    drawFocus,
		AssetVariant: ctx.action.AssetVariant,
	})
	// Persistence event: image stays on screen after reveal
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		TargetImage:  ctx.actionTag,
		StartFrame:   ctx.endFrame,
		EndFrame:     constant.FRAME_FOREVER,
		X:            x,
		Y:            y,
		Width:        w,
		Height:       h,
		EventType:    constant.EVENT_STATIC,
		ZoomFocus:    drawFocus,
		AssetVariant: ctx.action.AssetVariant,
	})
}
