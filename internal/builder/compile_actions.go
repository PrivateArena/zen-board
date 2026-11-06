package builder

import (
	"fmt"
	"strconv"
	"strings"

	"zen-board/internal/constant"
	"zen-board/internal/diag"
	"zen-board/internal/model"
	"zen-board/internal/script"
)

// errArity is returned by a handler whose tag carries fewer parts than it can
// safely index. Historically [arrow:TL] and [compare:onlyone] panicked on the
// out-of-range read; the compiler converts this into an E0009 finding and drops
// the action.
type errArity struct {
	tag     string
	got     int
	min     int
	fixHint string
}

func (e errArity) Error() string {
	return fmt.Sprintf("%q expects at least %d argument(s), got %d", e.tag, e.min, e.got)
}

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
	route          string // the handler dispatch chose; recorded for the DSL coverage test
	actionTag      string
	preset         string
	revealDuration float64
	// spec is the grammar entry the tag matched, and tag its resolved arguments. Both
	// are computed once per action so no handler re-splits a tag by itself.
	spec *script.ActionSpec
	tag  script.Tag
	// generatedID is the fallback identity, allocated once per action so a handler that
	// needs it early and the stamping pass cannot disagree, and so the counter stays
	// sequential.
	generatedID string
}

// positional returns the tag's arguments with any `key=value` tail already removed,
// which is what lets `[draw:robot:TR id=bot dur=6]` and `[draw:robot:TR:6]` reach the
// same handler code. The result is unquoted, as every handler has always expected.
func (ctx *actionContext) positional() []string {
	if len(ctx.tag.Parts) > 0 {
		return ctx.tag.Parts
	}
	return tagParts(strings.TrimPrefix(ctx.action.Tag, ctx.actionTag+":"))
}

// keys exposes the named parameters the tag carried.
func (ctx *actionContext) key(name string) string {
	v, _ := ctx.tag.Key(name)
	return v
}

// parseTag resolves the action once against the grammar table. Called from
// processAction before dispatch so that no handler has to split a tag by itself.
func (ctx *actionContext) parseTag() {
	spec, tag, ok := script.ParseTagSpec("[" + ctx.action.Tag + "]")
	if !ok {
		// A bare `[city]` is a draw - the dispatch chain falls through to
		// handleDraw for exactly that reason, so the grammar lookup has to agree
		// with the routing instead of giving up on an unprefixed asset id.
		drawSpec, found := script.ByTag("draw")
		if !found {
			return
		}
		spec, tag = drawSpec, script.ParseTag(drawSpec, "["+ctx.action.Tag+"]", ctx.action.Tag, nil)
	}
	ctx.spec, ctx.tag = &spec, tag
	ctx.tag.Parts = unquoteParts(tag.Parts)
}

func unquoteParts(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, script.Unquote(p))
	}
	return out
}

// kind names the action family used to build a deterministic fallback identity.
// It reads the route dispatch chose, so an identity can never disagree with the
// handler that ran; TargetImage cannot serve - it is an asset id for draws and a
// synthesised key for everything else.
func (ctx *actionContext) kind() string {
	if ctx.route != "" {
		return ctx.route
	}
	return "event"
}

// isSpecialAction reports whether the raw action tag belongs to the set of
// reserved command prefixes. Special actions bypass the generic
// "<asset>:<preset>:<duration>" tag parsing performed for draw actions.
func isSpecialAction(tag string) bool {
	for _, prefix := range specialActionPrefixes() {
		if strings.HasPrefix(tag, prefix) {
			return true
		}
	}
	return false
}

// specialActionPrefixes is the reserved command-prefix vocabulary, in the order
// the dispatch chain tests it.
func specialActionPrefixes() []string {
	return []string{constant.TAG_WAIT, constant.TAG_ZOOM, constant.TAG_STYLE, constant.TAG_CHAPTER, constant.TAG_SFX, constant.TAG_SUBTITLE, constant.TAG_TEXT, constant.TAG_ERASE, constant.TAG_MOVE, constant.TAG_GEN, constant.TAG_SLIDE, constant.TAG_BANNER, constant.TAG_WATERMARK, constant.TAG_ARROW, constant.TAG_HIGHLIGHT, constant.TAG_COMPARE, constant.TAG_TRANSITION, constant.TAG_OVERLAY, constant.TAG_COUNTER}
}

// dispatch routes an action tag to its dedicated handler and records the route it
// chose on the context, so the DSL coverage test can compare the vocabulary the
// table declares against the vocabulary the compiler actually routes.
//
// The case order mirrors the original if/continue chain and is significant: the
// exact-match directives (erase:*, clear) must be evaluated before their prefix
// siblings. A handler returns errArity when the tag is malformed; every other
// outcome is a successful (possibly empty) compile.
func (c *timelineCompiler) dispatch(ctx *actionContext) error {
	tag := ctx.action.Tag
	switch {
	case strings.HasPrefix(tag, constant.TAG_WAIT):
		ctx.route = "wait"
		// compile-time-only directive; nothing is emitted to the timeline
	case strings.HasPrefix(tag, constant.TAG_ZOOM):
		ctx.route = "zoom"
		c.handleZoom(ctx)
	case strings.HasPrefix(tag, constant.TAG_STYLE):
		ctx.route = "style"
		c.handleStyle(ctx)
	case strings.HasPrefix(tag, constant.TAG_CHAPTER):
		ctx.route = "chapter"
		c.handleChapter(ctx)
	case strings.HasPrefix(tag, constant.TAG_SUBTITLE):
		ctx.route = "subtitle"
		c.handleSubtitle(ctx)
	case strings.HasPrefix(tag, constant.TAG_SFX):
		ctx.route = "sfx"
		// compile-time-only directive; nothing is emitted to the timeline
	case strings.HasPrefix(tag, constant.TAG_TEXT):
		ctx.route = "text"
		c.handleText(ctx)
	case tag == constant.TAG_ERASE_ALL:
		ctx.route = "erase"
		c.handleEraseAll(ctx)
	case strings.HasPrefix(tag, constant.TAG_ERASE):
		ctx.route = "erase"
		c.handleErase(ctx)
	case strings.HasPrefix(tag, constant.TAG_MOVE):
		ctx.route = "move"
		c.handleMove(ctx)
	case strings.HasPrefix(tag, constant.TAG_GEN):
		ctx.route = "gen"
		c.handleGen(ctx)
	case tag == constant.TAG_CLEAR || strings.HasPrefix(tag, constant.TAG_CLEAR+":"):
		ctx.route = "clear"
		c.handleClear(ctx)
	case strings.HasPrefix(tag, constant.TAG_SLIDE):
		ctx.route = "slide"
		c.handleSlide(ctx)
	case strings.HasPrefix(tag, constant.TAG_BANNER):
		ctx.route = "banner"
		c.handleBanner(ctx)
	case strings.HasPrefix(tag, constant.TAG_WATERMARK):
		ctx.route = "watermark"
		c.handleWatermark(ctx)
	case strings.HasPrefix(tag, constant.TAG_ARROW):
		ctx.route = "arrow"
		return c.handleArrow(ctx)
	case strings.HasPrefix(tag, constant.TAG_HIGHLIGHT):
		ctx.route = "highlight"
		c.handleHighlight(ctx)
	case strings.HasPrefix(tag, constant.TAG_COMPARE):
		ctx.route = "compare"
		return c.handleCompare(ctx)
	case strings.HasPrefix(tag, constant.TAG_CHARACTER):
		ctx.route = "character"
		c.handleCharacter(ctx)
	case strings.HasPrefix(tag, constant.TAG_STICKER):
		ctx.route = "sticker"
		c.handleSticker(ctx)
	case strings.HasPrefix(tag, constant.TAG_CAPTION):
		ctx.route = "caption"
		c.handleCaption(ctx)
	case strings.HasPrefix(tag, constant.TAG_OVERLAY):
		ctx.route = "overlay"
		c.handleOverlay(ctx)
	case strings.HasPrefix(tag, constant.TAG_TRANSITION):
		ctx.route = "transition"
		c.handleTransition(ctx)
	case strings.HasPrefix(tag, constant.TAG_COUNTER):
		ctx.route = "counter"
		c.handleCounter(ctx)
	default:
		ctx.route = "draw"
		c.handleDraw(ctx)
	}
	return nil
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

		parts := tagParts(remainder)
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

		// The unified tail: anchor=, size=, color=, id= mean the same thing here as in
		// every other placement tag, and win over the positional spellings above.
		var tail script.Tag
		if spec, parsed, ok := script.ParseTagSpec("[" + ctx.action.Tag + "]"); ok {
			_ = spec
			tail = parsed
		}
		if v := tagKey(tail, "anchor"); v != "" {
			preset = v
		}
		if v, ok := tagNum(tail, "size"); ok {
			fontSize = v
		}
		if v := tagKey(tail, "font"); v != "" {
			fontFamily = v
		}
		if v := tagKey(tail, "weight"); v != "" {
			fontWeight = v
		}

		textAssetID := c.newTextJob(TextRenderJob{
			Content:    content,
			FontFamily: fontFamily,
			FontSize:   fontSize,
			IsBold:     fontWeight == "bold",
			Style:      c.currentStyle,
			ColourHex:  tagKey(tail, "color"),
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
	// [erase:*] is an explicit request to wipe the whole board, sticky layers included;
	// [clear] is the scene change that keeps the presenter.
	c.timeline.Events = cutEvents(c.timeline.Events, ctx.startFrame, false)
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
		c.record(diag.Finding{
			Code:      diag.W0006,
			Severity:  diag.SeverityWarning,
			Stage:     "compile",
			Line:      ctx.action.SrcLine,
			Col:       ctx.action.SrcCol,
			Snippet:   ctx.action.SrcRaw,
			Message:   fmt.Sprintf("[erase:%s] found no active layer to erase; the directive was skipped", targetAsset),
			FixHint:   "erase the asset id that is on screen, or use [clear] / [erase:*]",
			LegacyMsg: fmt.Sprintf("Warning: [erase:%s] cannot find active asset to erase; skipping", targetAsset),
		})
		return
	}
	c.timeline.Events = append(c.timeline.Events, eraseEvent)
}

func (c *timelineCompiler) handleMove(ctx *actionContext) {
	parts := tagParts(strings.TrimPrefix(ctx.action.Tag, constant.TAG_MOVE))
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
	// The resolved arguments, keys already separated - so [gen:"p" anchor=TR] cannot
	// become a prompt called `p anchor=TR`.
	parts := ctx.positional()
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

// cutEvents ends every event that is still on screen at atFrame.
//
// keepSticky decides whether a sticky layer (a character, a caption pinned for the
// beat) survives the wipe. Today all three cut directives rewrite EndFrame for every
// earlier event, which is why an overlay cannot outlive a scene change and why a
// [transition:] eats the mascot at its midpoint.
func cutEvents(events []model.FrameEvent, atFrame int, keepSticky bool) []model.FrameEvent {
	out := make([]model.FrameEvent, 0, len(events))
	for _, ev := range events {
		if ev.StartFrame >= atFrame {
			continue
		}
		if keepSticky && ev.Sticky {
			out = append(out, ev)
			continue
		}
		if ev.EndFrame > atFrame {
			ev.EndFrame = atFrame
		}
		out = append(out, ev)
	}
	return out
}

func (c *timelineCompiler) handleClear(ctx *actionContext) {
	// [clear] wipes the board and keeps the characters; [clear:all] is the escape hatch
	// for a genuine scene change.
	scope := strings.TrimSpace(strings.TrimPrefix(ctx.action.Tag, constant.TAG_CLEAR))
	if strings.HasPrefix(scope, ":") {
		scope = strings.TrimPrefix(scope, ":")
	}
	c.timeline.Events = cutEvents(c.timeline.Events, ctx.startFrame, scope != "all")
	c.gridIndex = 0
	// Inert marker so the eventlog records the board wipe
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame: ctx.startFrame,
		EndFrame:   constant.FRAME_FOREVER,
		EventType:  constant.EVENT_CLEAR,
	})
}

func (c *timelineCompiler) handleSlide(ctx *actionContext) {
	// The resolved arguments, keys already separated - so [slide:city anchor=br] cannot
	// become an asset called "city anchor=br".
	parts := ctx.positional()
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
	// The resolved arguments, keys already separated - so [slide:city anchor=br] cannot
	// become an asset called "city anchor=br".
	parts := ctx.positional()
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

// handleWatermark creates a persistent overlay watermark. The tag format is:
// [watermark:<source>:<position>]
// where <source> is either text (e.g. "CavemanExplains") or an image path,
// and <position> is one of TL, TR, BL, BR (top-left, top-right, bottom-left,
// bottom-right). The watermark persists for the entire video duration.
func (c *timelineCompiler) handleWatermark(ctx *actionContext) {
	// The resolved arguments, keys already separated - so [slide:city anchor=br] cannot
	// become an asset called "city anchor=br".
	parts := ctx.positional()
	source := parts[0]
	position := "TL"
	if len(parts) > 1 {
		position = strings.ToUpper(parts[1])
	}
	// Validate position
	validPositions := map[string]bool{"TL": true, "TR": true, "BL": true, "BR": true}
	if !validPositions[position] {
		position = "TL"
	}

	targetID := fmt.Sprintf("__watermark_%s|%s", source, position)

	// Watermark persists for the entire video: FRAME_FOREVER
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		TargetImage: targetID,
		StartFrame:  ctx.startFrame,
		EndFrame:    constant.FRAME_FOREVER,
		EventType:   constant.EVENT_WATERMARK,
		ZoomFocus:   c.currentZoomFocus,
	})
}

// namedDuration returns dur= when the tag carried one, so the named spelling wins over the
// positional one the caller already parsed - the same precedence the grammar promises.
func (ctx *actionContext) namedDuration(def float64) float64 {
	if v, ok := tagNum(ctx.tag, "dur"); ok && v > 0 {
		return v
	}
	return def
}

func (c *timelineCompiler) handleArrow(ctx *actionContext) error {
	// The resolved arguments, keys already separated - so [slide:city anchor=br] cannot
	// become an asset called "city anchor=br".
	parts := ctx.positional()
	if len(parts) < 2 || parts[1] == "" {
		return errArity{
			tag:     "[" + constant.TAG_ARROW + "from:to[:style][:duration]]",
			got:     len(parts),
			min:     2,
			fixHint: "give both endpoints, e.g. [arrow:TL:BR] or [arrow:@bot:@curve:curved:2]",
		}
	}
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
	duration = ctx.namedDuration(duration)

	// Element references resolve now, while the compiler knows what is on screen. An
	// unresolvable @ref is reported rather than falling through to "the whole canvas".
	fromRef, badFrom := c.resolveEndpoint(ctx, from)
	if badFrom != "" {
		c.recordUnresolvedRef(ctx, badFrom)
		return nil
	}
	toRef, badTo := c.resolveEndpoint(ctx, to)
	if badTo != "" {
		c.recordUnresolvedRef(ctx, badTo)
		return nil
	}
	from, to = fromRef, toRef
	// An arrow between placed elements belongs with them: leaving it on the board layer
	// would pan it away from the thing it names.
	arrowLayer := c.endpointLayer(ctx, from)
	if l := c.endpointLayer(ctx, to); l != "" {
		arrowLayer = l
	}

	end := ctx.startFrame + int(duration*float64(c.conf.FPS))
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame: ctx.startFrame,
		EndFrame:   end,
		EventType:  constant.EVENT_ARROW,
		ArrowFrom:  from,
		ArrowTo:    to,
		ArrowStyle: style,
		Layer:      arrowLayer,
		ZoomFocus:  c.currentZoomFocus,
	})
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame: end,
		EndFrame:   constant.FRAME_FOREVER,
		EventType:  constant.EVENT_ARROW_STATIC,
		ArrowFrom:  from,
		ArrowTo:    to,
		ArrowStyle: style,
		Layer:      arrowLayer,
		ZoomFocus:  c.currentZoomFocus,
	})
	return nil
}

func (c *timelineCompiler) handleHighlight(ctx *actionContext) {
	// The resolved arguments, keys already separated - so [slide:city anchor=br] cannot
	// become an asset called "city anchor=br".
	parts := ctx.positional()
	region := parts[0]
	layer := ""
	if ref, isRef := elementRef(region); isRef {
		resolved, ok := c.resolveRefArg(region, ctx.startFrame, true)
		if !ok {
			c.recordUnresolvedRef(ctx, ref)
			return
		}
		region = resolved
		// A ring around a presenter must sit with the presenter: leaving it on the board
		// would draw it under the camera crop the referenced layer escaped.
		if ev, found := c.anchorEvent(ref, ctx.startFrame); found {
			layer = ev.Layer
		}
	}
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
	duration = ctx.namedDuration(duration)

	end := ctx.startFrame + int(duration*float64(c.conf.FPS))
	c.timeline.Events = append(c.timeline.Events, model.FrameEvent{
		StartFrame:     ctx.startFrame,
		EndFrame:       end,
		EventType:      constant.EVENT_HIGHLIGHT,
		TargetImage:    region,
		HighlightStyle: style,
		Layer:          layer,
		ZoomFocus:      c.currentZoomFocus,
	})
}

func (c *timelineCompiler) handleCompare(ctx *actionContext) error {
	// The resolved arguments, keys already separated - so [slide:city anchor=br] cannot
	// become an asset called "city anchor=br".
	parts := ctx.positional()
	if len(parts) < 2 || parts[1] == "" {
		return errArity{
			tag:     "[" + constant.TAG_COMPARE + "left:right[:labelL][:labelR]]",
			got:     len(parts),
			min:     2,
			fixHint: "give both sides, e.g. [compare:city:pyramids]",
		}
	}
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
	return nil
}

func (c *timelineCompiler) handleOverlay(ctx *actionContext) {
	// The resolved arguments, keys already separated - so [slide:city anchor=br] cannot
	// become an asset called "city anchor=br".
	parts := ctx.positional()
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
	// The resolved arguments, keys already separated - so [slide:city anchor=br] cannot
	// become an asset called "city anchor=br".
	parts := ctx.positional()
	tType := parts[0]
	duration := 0.5
	if len(parts) > 1 && parts[1] != "" {
		if d, err := strconv.ParseFloat(parts[1], 64); err == nil {
			duration = d
		}
	}

	end := ctx.startFrame + int(duration*float64(c.conf.FPS))
	midpoint := ctx.startFrame + int(duration*float64(c.conf.FPS))/2

	// Truncate the board at the midpoint of the transition. A sticky layer is left
	// standing: a wipe between scenes is not a reason for the mascot to disappear.
	c.timeline.Events = cutEvents(c.timeline.Events, midpoint, true)
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
	// The resolved arguments, keys already separated - so [slide:city anchor=br] cannot
	// become an asset called "city anchor=br".
	parts := ctx.positional()
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

	// The cursor may arrive as a named parameter ([draw:robot cursor=magic_wand]) or, as
	// it did before named parameters existed, in the SVG variant map.
	cursorName := firstNonEmpty(ctx.key("cursor"), extractCursor(ctx.action.AssetVariant, constant.CURSOR_HAND))
	// A key the grammar declares is a parameter, not an SVG attribute. Leaving `id=` in
	// the variant map would set a literal id attribute on the rasterised SVG.
	for name := range ctx.action.AssetVariant {
		if script.IsAuthoringKey(name) {
			delete(ctx.action.AssetVariant, name)
		}
	}

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
