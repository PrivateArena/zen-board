package builder

import (
	"fmt"
	"image"
	"strconv"

	"zen-board/internal/diag"
	"zen-board/internal/model"
)

// Anchor resolves an element reference (`@bot`) to the box of the layer that carries
// that identity. It runs at compile time, so FrameEvent stays self-contained and the
// render path keeps its purity - the renderer never has to know that an address existed.
//
// Resolution looks backwards: an `@ref` can only name something the script has already
// placed. A forward reference cannot be resolved without a second pass over the whole
// event list, which would make compile order observable in a way it is not today.
func (c *timelineCompiler) Anchor(id string, atFrame int) (image.Rectangle, bool) {
	ev, ok := c.anchorEvent(id, atFrame)
	if !ok {
		return image.Rectangle{}, false
	}
	return image.Rect(ev.X, ev.Y, ev.X+ev.Width, ev.Y+ev.Height), true
}

// anchorEvent is the addressed event: the latest placement of that identity that has
// already started, so a character that changed state is referred to by its current look.
func (c *timelineCompiler) anchorEvent(id string, atFrame int) (model.FrameEvent, bool) {
	if id == "" {
		return model.FrameEvent{}, false
	}
	var best model.FrameEvent
	found := false
	for _, ev := range c.timeline.Events {
		if ev.ID != id && ev.ID != id+":exit" {
			continue
		}
		// Both bounds matter: after [clear:all] or an expired dur= the element is gone,
		// and pointing at its old box would draw an annotation at nothing.
		if ev.StartFrame > atFrame || ev.EndFrame < atFrame {
			continue
		}
		if !found || ev.StartFrame >= best.StartFrame {
			best, found = ev, true
		}
	}
	return best, found
}

// resolveRef turns an `@id` argument into the box of the layer that carries that
// identity. Arguments that are not references - a named box or an x,y pair - are left
// alone, because the renderer already resolves those and did so correctly.
//
// The behaviour worth having is the failure: an address that names nothing is reported
// (E0013) instead of falling through to a box, which is how a typo'd reference used to
// draw an annotation across the whole canvas.
func (c *timelineCompiler) resolveRef(arg string, atFrame int) (image.Rectangle, bool) {
	ref, isRef := elementRef(arg)
	if !isRef {
		return image.Rectangle{}, false
	}
	return c.Anchor(ref, atFrame)
}

// elementRef strips the @ address sigil from an argument, reporting whether it carried
// one. `[arrow:@bot:@curve]` and `[highlight:@bot]` are the spellings this exists for.
func elementRef(arg string) (string, bool) {
	if len(arg) < 2 || arg[0] != '@' {
		return "", false
	}
	return arg[1:], true
}

// resolveRefArg rewrites an @ref argument into the concrete geometry the renderer
// already understands ("x,y" for a point, "x,y,w,h" for a region). This is what makes
// compile-time resolution real rather than decorative: the render path never learns that
// an address existed, so FrameEvent stays self-contained and the renderer stays pure.
func (c *timelineCompiler) resolveRefArg(arg string, atFrame int, wantRegion bool) (string, bool) {
	box, isRef := c.resolveRef(arg, atFrame)
	if !isRef {
		if _, wantsRef := elementRef(arg); wantsRef {
			return arg, false
		}
		return arg, true
	}
	x, y, w, h := box.Min.X, box.Min.Y, box.Dx(), box.Dy()
	if wantRegion {
		return strconv.Itoa(x) + "," + strconv.Itoa(y) + "," + strconv.Itoa(w) + "," + strconv.Itoa(h), true
	}
	px, py := anchorPoint(x, y, w, h)
	return strconv.Itoa(px) + "," + strconv.Itoa(py), true
}

// resolveEndpoint resolves one annotation endpoint, returning the bare address as the
// second value when it named nothing on screen.
func (c *timelineCompiler) resolveEndpoint(ctx *actionContext, arg string) (string, string) {
	resolved, ok := c.resolveRefArg(arg, ctx.startFrame, false)
	if !ok {
		if ref, isRef := elementRef(arg); isRef {
			return arg, ref
		}
	}
	return resolved, ""
}

// recordUnresolvedRef reports E0013 against the tag that carried the address.
func (c *timelineCompiler) recordUnresolvedRef(ctx *actionContext, ref string) {
	c.record(diag.Finding{
		Code: diag.E0013, Severity: diag.SeverityError, Stage: "compile",
		File: c.sourceName(ctx.action), Line: ctx.action.SrcLine, Col: ctx.action.SrcCol,
		Snippet: ctx.action.SrcRaw,
		Message: fmt.Sprintf("@%s does not address anything on screen at frame %d", ref, ctx.startFrame),
		FixHint: "place the element first with id=" + ref + ", or refer to a named box such as @TL or 100,200",
	})
}

// anchorPoint is where an annotation should attach to a box: the centre, which is what
// an arrow should point at and what a highlight should surround.
func anchorPoint(x, y, w, h int) (int, int) {
	if w <= 0 || h <= 0 {
		return x, y
	}
	return x + w/2, y + h/2
}

// endpointLayer reports the layer of the element an addressed endpoint sits on, so an
// annotation naming a presenter belongs to the presenter layer too.
func (c *timelineCompiler) endpointLayer(ctx *actionContext, arg string) string {
	ref, isRef := elementRef(arg)
	if !isRef {
		return ""
	}
	ev, ok := c.anchorEvent(ref, ctx.startFrame)
	if !ok {
		return ""
	}
	return ev.Layer
}
