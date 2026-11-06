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

// The genre tags are the agent-facing spellings of the whiteboard, mascot and meme
// idioms. They are sugar over existing event types: a character is an overlay event on
// the overlay layer with camera-independence and [clear] survival, a sticker is the
// same with a pop entrance, a caption is a text event that keeps its pixel size.
//
// They all read their arguments through the DSL table, so `anchor=`, `size=` and `id=`
// mean the same thing here as they do in the linter and in `check --dsl`.

// tagSpec resolves the action through the grammar table. The compiler receives the tag
// body without brackets, so it is re-bracketed for the one parser both paths share -
// there is no second tag splitter here on purpose.

// placement resolves the box a genre tag should occupy, in priority order:
// explicit @x,y,w,h from the script, then anchor=/preset plus an explicit size, then
// the anchor box itself. Everything is derived from the canvas, so the same line is
// sane at 1920x1080 and 1280x720.
func (c *timelineCompiler) placement(ctx *actionContext, tag script.Tag, defaultAnchor string) (x, y, w, h int) {
	if ctx.action.W > 0 && ctx.action.H > 0 {
		return ctx.action.X, ctx.action.Y, ctx.action.W, ctx.action.H
	}
	anchor := firstNonEmpty(tagKey(tag, "anchor"), tagKey(tag, "preset"), ctx.preset, defaultAnchor)
	px, py, pw, ph := model.GetPresetLayout(anchor, c.conf.Width, c.conf.Height)

	// The box may be given three ways: a canvas percentage, an explicit pixel size, or
	// nothing at all (then the anchor box is the box).
	size, hasSize := tagNum(tag, "size")
	if hasSize && (size <= 0 || size > 100) {
		c.record(diag.Finding{
			Code: diag.E0004, Severity: diag.SeverityError, Stage: "compile",
			File: c.sourceName(ctx.action), Line: ctx.action.SrcLine, Col: ctx.action.SrcCol,
			Snippet: ctx.action.SrcRaw,
			Message: fmt.Sprintf("size=%v is not a percentage between 1 and 100", size),
			FixHint: "write size= as a number between 1 and 100",
		})
		hasSize = false
	}
	tw, hasW := tagNum(tag, "w")
	th, hasH := tagNum(tag, "h")
	switch {
	case hasSize && c.scalesSize(tag) && ph > 0:
		// For an image layer size= is a percentage of canvas height - "make the mascot
		// 45% of the screen". For a caption it is the glyph height, handled below.
		h = int(float64(c.conf.Height) * size / 100)
		w = h * pw / ph
	case hasW || hasH:
		if !(hasW && hasH) {
			// One number alone cannot describe a box, and guessing the other axis would
			// put the layer at a size nobody wrote.
			c.record(diag.Finding{
				Code: diag.W0012, Severity: diag.SeverityWarning, Stage: "compile",
				File: c.sourceName(ctx.action), Line: ctx.action.SrcLine, Col: ctx.action.SrcCol,
				Snippet: ctx.action.SrcRaw,
				Message: fmt.Sprintf("w= and h= must be given together; got %s", onlyOneOf(tw, hasW, th, hasH)),
				FixHint: "give w= and h= together, use size= for a percentage of the canvas, or drop both to fill the anchor box",
			})
		} else {
			w, h = int(tw), int(th)
		}
	}

	if w == 0 || h == 0 {
		return px, py, pw, ph
	}
	// Anchored to the box's own corner rather than centred in it: a bottom-right mascot
	// should sit in the corner, and the layer's aspect ratio is not the box's.
	a := normaliseAnchor(anchor)
	switch a {
	case "br", "tr", "r", "third-r":
		x = px + pw - w
	case "bl", "tl", "l", "third-l":
		x = px
	default:
		x = px + (pw-w)/2
	}
	switch a {
	case "br", "bl", "tr", "tl", "t", "tc", "b", "bc", "third-l", "third-r", "third-c":
		// A placed layer rests on the edge of its band: a mascot in the bottom band
		// stands on the bottom line, a thinking pose in the top band hangs from the top.
		if a[0] == 'b' || a == "third-l" || a == "third-r" || a == "third-c" {
			y = py + ph - h
		} else {
			y = py
		}
	default:
		y = py + (ph-h)/2
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	// A layer larger than the canvas is drawn but unreadable, and clipping it is not
	// what the author asked for - say so.
	if w > c.conf.Width || h > c.conf.Height {
		c.record(diag.Finding{
			Code: diag.W0011, Severity: diag.SeverityWarning, Stage: "compile",
			File: c.sourceName(ctx.action), Line: ctx.action.SrcLine, Col: ctx.action.SrcCol,
			Snippet: ctx.action.SrcRaw,
			Message: fmt.Sprintf("the placed box is %dx%d, larger than the %dx%d canvas", w, h, c.conf.Width, c.conf.Height),
			FixHint: "lower size=, or give a smaller w=/h= pair",
		})
	}
	return x, y, w, h
}

// scalesSize reports whether size= describes the layer's box rather than its type.
// A caption's size is its font, so letting it resize the box would put the text
// somewhere nobody asked for.
func (c *timelineCompiler) scalesSize(tag script.Tag) bool {
	return tag.Spec.Tag != "caption"
}

// normaliseAnchor folds the historical uppercase presets onto the anchor vocabulary so
// the corner logic has one spelling to switch on.
func normaliseAnchor(anchor string) string {
	switch anchor {
	case "BR":
		return "br"
	case "BL":
		return "bl"
	case "TR":
		return "tr"
	case "TL":
		return "tl"
	case "RH":
		return "r"
	case "LH":
		return "l"
	}
	return anchor
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func tagKey(tag script.Tag, name string) string {
	v, _ := tag.Key(name)
	return v
}

func tagNum(tag script.Tag, name string) (float64, bool) {
	raw, ok := tag.Key(name)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(raw, "%"), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func tagBool(tag script.Tag, name string, def bool) bool {
	raw, ok := tag.Key(name)
	if !ok {
		return def
	}
	return raw == "true" || raw == "yes" || raw == "1"
}

// handleCharacter compiles `[character:robot_happy anchor=br size=45%]`.
//
// The mascot needs four things the raw [overlay:] primitive does not provide: it must
// stay put when [zoom:] points elsewhere, survive [clear], draw above the board, and be
// addressable so a state change replaces it. Those are the Layer and Sticky fields plus
// a per-id track pass - deliberately not a new event type, which would also have to be
// added to two EventType skip lists or it raises phantom "missing asset" errors.
func (c *timelineCompiler) handleCharacter(ctx *actionContext) {
	tag := ctx.tag
	if len(tag.Parts) == 0 || tag.Parts[0] == "" {
		return
	}
	asset := tag.Parts[0]
	x, y, w, h := c.placement(ctx, tag, "br")
	opacity := 1.0
	if v, ok := tagNum(tag, "opacity"); ok {
		opacity = v
	}
	ev := model.FrameEvent{
		TargetImage: asset,
		EventType:   constant.EVENT_OVERLAY,
		StartFrame:  ctx.startFrame,
		EndFrame:    c.lifetimeEnd(ctx, tag, true),
		Opacity:     opacity,
		Layer:       c.layerOf(tag),
		Sticky:      tagBool(tag, "keep", true),
		ZoomFocus:   c.focusOf(tag, "br"),
		Transition:  constant.TRANSITION_NONE,
	}
	ev.ID = c.identity(ctx, tag)
	ev.X, ev.Y, ev.Width, ev.Height = x, y, w, h
	c.closePreviousState(ev.ID, ev.StartFrame)
	c.appendWithExit(ctx, ev, tagKey(tag, "exit"))
}

// handleSticker compiles `[sticker:meme_lol anchor=tr w=280 pop dur=2 exit=pop]`: a
// decoration that pops in and out on the overlay layer without a [clear].
func (c *timelineCompiler) handleSticker(ctx *actionContext) {
	tag := ctx.tag
	if len(tag.Parts) == 0 || tag.Parts[0] == "" {
		return
	}
	asset := tag.Parts[0]
	x, y, w, h := c.placement(ctx, tag, "tr")
	opacity := 1.0
	if v, ok := tagNum(tag, "opacity"); ok {
		opacity = v
	}
	ev := model.FrameEvent{
		TargetImage: asset,
		EventType:   constant.EVENT_OVERLAY,
		StartFrame:  ctx.startFrame,
		EndFrame:    c.lifetimeEnd(ctx, tag, false),
		Opacity:     opacity,
		Layer:       c.layerOf(tag),
		Sticky:      tagBool(tag, "keep", true),
		ZoomFocus:   c.focusOf(tag, "tr"),
		Transition:  constant.TRANSITION_NONE,
	}
	ev.ID = c.identity(ctx, tag)
	ev.X, ev.Y, ev.Width, ev.Height = x, y, w, h
	c.closePreviousState(ev.ID, ev.StartFrame)
	c.appendWithExit(ctx, ev, tagKey(tag, "exit"))
}

// handleCaption compiles `[caption:"IT WEDGES"@top color=#FFFF00 size=110 dur=2.5]`.
//
// Two differences from [text:] matter for the meme genre: the raster keeps its pixel
// size instead of being scaled to fill the layout box, and it is drawn on the overlay
// layer so a camera move cannot make the punchline disappear.
func (c *timelineCompiler) handleCaption(ctx *actionContext) {
	tag := ctx.tag
	if len(tag.Parts) == 0 {
		return
	}
	content := tag.Parts[0]
	if content == "" {
		return
	}
	size := 110.0
	if v, ok := tagNum(tag, "size"); ok {
		size = v
	}
	outline := 6.0
	if v, ok := tagNum(tag, "outline"); ok {
		outline = v
	}
	anchor := firstNonEmpty(tagKey(tag, "anchor"), tagKey(tag, "preset"), ctx.preset, "bc")
	x, y, w, h := c.placement(ctx, tag, anchor)

	textAssetID := c.newTextJob(TextRenderJob{
		Content:    content,
		FontFamily: firstNonEmpty(tagKey(tag, "font"), constant.FONT_SANS),
		FontSize:   size,
		IsBold:     true,
		Style:      c.currentStyle,
		OutlinePx:  int(outline),
		ColourHex:  tagKey(tag, "color"),
		PixelExact: true,
	})

	ev := model.FrameEvent{
		TargetImage: textAssetID,
		EventType:   constant.EVENT_TEXT,
		StartFrame:  ctx.startFrame,
		EndFrame:    c.lifetimeEnd(ctx, tag, false),
		Layer:       c.layerOf(tag),
		Sticky:      tagBool(tag, "keep", true),
		ZoomFocus:   c.focusOf(tag, anchor),
		FitMode:     constant.FIT_NATIVE,
		Align:       firstNonEmpty(tagKey(tag, "align"), "center"),
	}
	ev.ID = c.identity(ctx, tag)
	ev.X, ev.Y, ev.Width, ev.Height = x, y, w, h
	c.closePreviousState(ev.ID, ev.StartFrame)
	c.appendWithExit(ctx, ev, tagKey(tag, "exit"))
}

// layerOf reads layer=, defaulting to the overlay layer for the genre tags: the whole
// point of [character:] is that it does not vanish when the camera looks elsewhere.
func (c *timelineCompiler) layerOf(tag script.Tag) string {
	if v := tagKey(tag, "layer"); v != "" {
		return v
	}
	return constant.LAYER_OVERLAY
}

// focusOf records the anchor as the event's ZoomFocus only while the layer is on the
// overlay layer, where that field is a layout key. On the board layer it would be read
// as a camera target that matches nothing, and the layer would blink out on every zoom.
func (c *timelineCompiler) focusOf(tag script.Tag, def string) string {
	if c.layerOf(tag) != constant.LAYER_OVERLAY {
		return ""
	}
	return firstNonEmpty(tagKey(tag, "anchor"), def)
}

// lifetimeEnd decides how long a placed layer lives. A character persists until it is
// replaced or the video ends; a caption or sticker without dur= gets its reveal window.
func (c *timelineCompiler) lifetimeEnd(ctx *actionContext, tag script.Tag, persistent bool) int {
	if v, ok := tagNum(tag, "dur"); ok {
		return ctx.startFrame + int(v*float64(c.conf.FPS))
	}
	if persistent {
		return constant.FRAME_FOREVER
	}
	return ctx.endFrame
}

// identity gives a placed layer its address: id= from the tag, else the deterministic
// per-compile identity. The handler needs it before the generic stamping pass because a
// character state swap addresses the previous state by id.
func (c *timelineCompiler) identity(ctx *actionContext, tag script.Tag) string {
	if id := tagKey(tag, "id"); id != "" {
		return id
	}
	if ctx.generatedID == "" {
		ctx.generatedID = c.nextID(ctx.kind())
	}
	return ctx.generatedID
}

// appendWithExit writes the layer and, when exit= was given, a second event for the exit
// animation. A second event is not an optimisation: the cut loops rewrite the EndFrame
// of every earlier event, so folding the animation into the primary window would let a
// later [clear] or [transition:] silently swallow it.
func (c *timelineCompiler) appendWithExit(ctx *actionContext, ev model.FrameEvent, exitKind string) {
	c.timeline.Events = append(c.timeline.Events, ev)
	if exitKind == "" {
		return
	}
	if ev.EndFrame == constant.FRAME_FOREVER {
		c.record(diag.Finding{
			Code: diag.W0009, Severity: diag.SeverityWarning, Stage: "compile",
			File: c.sourceName(ctx.action), Line: ctx.action.SrcLine, Col: ctx.action.SrcCol,
			Snippet: ctx.action.SrcRaw,
			Message: fmt.Sprintf("exit=%s was ignored: a layer without dur= never ends, so it has nothing to exit from", exitKind),
			FixHint: "give the layer a lifetime with dur=, or drop exit=",
		})
		return
	}
	frames := int(ctx.revealDuration * float64(c.conf.FPS))
	if frames <= 0 {
		frames = c.conf.FPS / 2
	}
	// The video ends at the narration plus the freeze tail; an exit scheduled past that
	// would never be drawn, which looks like a tag that silently does nothing.
	last := c.totalFrames()
	if ev.EndFrame >= last {
		c.record(diag.Finding{
			Code: diag.W0010, Severity: diag.SeverityWarning, Stage: "compile",
			File: c.sourceName(ctx.action), Line: ctx.action.SrcLine, Col: ctx.action.SrcCol,
			Snippet: ctx.action.SrcRaw,
			Message: fmt.Sprintf("exit=%s starts at frame %d, at or past the end of the video (%d frames)", exitKind, ev.EndFrame, c.totalFrames()),
			FixHint: "shorten dur=, move the tag earlier, or lengthen the freeze tail",
		})
		return
	}
	if ev.EndFrame+frames > last {
		frames = last - ev.EndFrame
	}
	exit := ev
	// The frame window is inclusive, so the exit starts on the frame *after* the layer
	// last appears: sharing one boundary frame would composite the layer twice.
	exit.StartFrame = ev.EndFrame + 1
	exit.EndFrame = ev.EndFrame + frames
	exit.ExitKind = exitKind
	exit.ExitFrames = frames
	// The entrance transition must not replay on the way out.
	exit.Transition = constant.TRANSITION_NONE
	exit.ID = ev.ID + ":exit"
	c.timeline.Events = append(c.timeline.Events, exit)
}

// totalFrames is the frame count of the finished video, freeze tail included - the same
// number the renderer loops over.
func (c *timelineCompiler) totalFrames() int {
	return int(c.timeline.Duration*float64(c.conf.FPS)) + c.conf.FreezeFrames
}

// closePreviousState is the anti-ghosting pass: a new state with the same identity
// closes the previous state's window at the new start frame, so swapping
// `[character:robot_happy]` for `[character:robot_thinking]` replaces the sprite instead
// of stacking two of them.
func (c *timelineCompiler) closePreviousState(id string, atFrame int) {
	if id == "" {
		return
	}
	for i := range c.timeline.Events {
		ev := &c.timeline.Events[i]
		// The exit event of the outgoing state is part of that state: leaving it running
		// would fade the old sprite out on top of the new one.
		if (ev.ID != id && ev.ID != id+":exit") || ev.EndFrame < atFrame {
			continue
		}
		// Inclusive windows: end the old state one frame before the new one starts, or
		// both sprites are on screen together on the boundary frame - the ghost itself.
		c.timeline.Events[i].EndFrame = atFrame - 1
	}
}

// onlyOneOf names whichever of w=/h= was supplied alone.
func onlyOneOf(tw float64, hasW bool, th float64, hasH bool) string {
	if hasW {
		return fmt.Sprintf("w=%d without h=", int(tw))
	}
	return fmt.Sprintf("h=%d without w=", int(th))
}
