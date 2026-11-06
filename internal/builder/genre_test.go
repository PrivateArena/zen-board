package builder

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"zen-board/internal/constant"
	"zen-board/internal/diag"
	"zen-board/internal/model"
	"zen-board/internal/script"
)

// mustGenreParse parses one line of tags, failing on any parse finding.
func mustGenreParse(t *testing.T, text string) []model.DrawAction {
	t.Helper()
	lines, findings := script.ParseChecked("probe "+text, "probe.zen")
	for _, f := range findings {
		t.Fatalf("%s: unexpected parse finding: %s", text, f.Message)
	}
	if len(lines) != 1 || len(lines[0].Actions) == 0 {
		t.Fatalf("%s: parser produced no actions", text)
	}
	return lines[0].Actions
}

// genreConf points at a throwaway asset directory holding the ids the genre tests use,
// so the compile-time asset gate is satisfied the same way a real script would be.
func genreConf(t *testing.T) *model.Project {
	t.Helper()
	dir := t.TempDir()
	for _, id := range []string{"robot", "robot_happy", "robot_thinking", "city", "meme_lol"} {
		if err := os.WriteFile(filepath.Join(dir, id+".png"), []byte("stub"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &model.Project{Width: 1000, Height: 500, FPS: 10, CameraEnabled: false, AssetsDir: dir}
}

func compileGenre(t *testing.T, text string) *TimelineCompilation {
	t.Helper()
	conf := genreConf(t)
	conf.FreezeFrames = 30
	actions := mustGenreParse(t, text)
	lines := []model.ProcessedLine{{StartTime: 0, Duration: 2, Actions: actions}}
	comp, err := CompileTimeline(conf, []model.WordTiming{{Word: "hi", Start: 0, End: 1}}, lines, 2, "/tmp/a.wav")
	if err != nil {
		t.Fatal(err)
	}
	return comp
}

func eventsOfType(comp *TimelineCompilation, kind string) []model.FrameEvent {
	var out []model.FrameEvent
	for _, ev := range comp.Timeline.Events {
		if ev.EventType == kind {
			out = append(out, ev)
		}
	}
	return out
}

func TestCharacterIsAnOverlayStickyLayer(t *testing.T) {
	comp := compileGenre(t, "[character:robot anchor=br size=40%]")
	got := eventsOfType(comp, constant.EVENT_OVERLAY)
	if len(got) != 1 {
		t.Fatalf("compiled %d overlay events, want 1", len(got))
	}
	ev := got[0]
	if ev.Layer != constant.LAYER_OVERLAY {
		t.Errorf("Layer = %q, want %q: a mascot must ignore where the camera points", ev.Layer, constant.LAYER_OVERLAY)
	}
	if !ev.Sticky {
		t.Error("a character must survive [clear]")
	}
	if ev.TargetImage != "robot" {
		t.Errorf("asset = %q", ev.TargetImage)
	}
	// 40% of a 500px canvas is 200px tall, and it must sit in the bottom-right box
	// rather than fill the canvas.
	if ev.Height != 200 {
		t.Errorf("Height = %d, want 200 (40%% of 500)", ev.Height)
	}
	if ev.Y+ev.Height > 500 || ev.X+ev.Width > 1000 {
		t.Errorf("box %v,%v %dx%d escapes the canvas", ev.X, ev.Y, ev.Width, ev.Height)
	}
	if ev.X < 500 || ev.Y < 250 {
		t.Errorf("anchor=br must place the layer in the bottom-right quadrant, got %d,%d", ev.X, ev.Y)
	}
	if ev.ID == "" {
		t.Error("a character needs an identity to be addressed and replaced")
	}
}

// Two states of the same character must not stack: the plan calls the stacked result a
// ghost, and it is the first thing a viewer notices.
func TestCharacterStateSwapDoesNotGhost(t *testing.T) {
	conf := genreConf(t)
	actions := mustGenreParse(t, "[character:robot_happy id=bot]")
	later := mustGenreParse(t, "[character:robot_thinking id=bot]")
	lines := []model.ProcessedLine{
		{StartTime: 0, Duration: 1, Actions: actions},
		{StartTime: 1, Duration: 1, Actions: later},
	}
	comp, err := CompileTimeline(conf, []model.WordTiming{}, lines, 3, "/tmp/a.wav")
	if err != nil {
		t.Fatal(err)
	}
	got := eventsOfType(comp, constant.EVENT_OVERLAY)
	if len(got) != 2 {
		t.Fatalf("want two states, got %d", len(got))
	}
	first, second := got[0], got[1]
	if first.ID != second.ID {
		t.Fatalf("id= must be the identity: %q vs %q", first.ID, second.ID)
	}
	// The windows are inclusive, so the previous state must stop exactly one frame
	// before: the same frame is a ghost, a skipped frame is a flicker.
	if first.EndFrame+1 != second.StartFrame {
		t.Errorf("state windows are %d..%d and %d..%d; want the first to end one frame before the second starts",
			first.StartFrame, first.EndFrame, second.StartFrame, second.EndFrame)
	}
}

func TestClearKeepsCharactersAndClearAllDoesNot(t *testing.T) {
	for _, tc := range []struct {
		tag        string
		wantSticky bool
	}{
		{"[clear]", true},
		{"[clear:all]", false},
	} {
		conf := genreConf(t)
		char := mustGenreParse(t, "[character:robot id=bot]")
		board := mustGenreParse(t, "[draw:city]")
		cut := mustGenreParse(t, tc.tag)
		lines := []model.ProcessedLine{
			{StartTime: 0, Duration: 1, Actions: append(append([]model.DrawAction{}, char...), board...)},
			{StartTime: 1, Duration: 1, Actions: cut},
		}
		comp, err := CompileTimeline(conf, []model.WordTiming{}, lines, 3, "/tmp/a.wav")
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range comp.Timeline.Events {
			if !ev.Sticky {
				continue
			}
			// The cut lands at frame 10; a layer whose window reaches past it survived.
			alive := ev.EndFrame > 10
			if alive != tc.wantSticky {
				t.Errorf("%s: sticky layer alive = %v (EndFrame %d), want %v", tc.tag, alive, ev.EndFrame, tc.wantSticky)
			}
		}
	}
}

// A transition wipes the board at its midpoint; a presenter must not vanish mid-wipe.
func TestTransitionDoesNotEatTheMascot(t *testing.T) {
	conf := genreConf(t)
	char := mustGenreParse(t, "[character:robot id=bot]")
	wipe := mustGenreParse(t, "[transition:wipe-left:1]")
	lines := []model.ProcessedLine{
		{StartTime: 0, Duration: 1, Actions: char},
		{StartTime: 1, Duration: 1, Actions: wipe},
	}
	comp, err := CompileTimeline(conf, []model.WordTiming{}, lines, 4, "/tmp/a.wav")
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range comp.Timeline.Events {
		if ev.EventType != constant.EVENT_OVERLAY {
			continue
		}
		if ev.EndFrame != constant.FRAME_FOREVER {
			t.Errorf("transition cut the character at frame %d; a presenter must outlive a wipe", ev.EndFrame)
		}
	}
}

func TestExitIsASecondEvent(t *testing.T) {
	// A long enough timeline: an exit scheduled past the end of the video is dropped on
	// purpose (see TestExitPastTheEndIsReported).
	conf := genreConf(t)
	conf.FreezeFrames = 20
	actions := mustGenreParse(t, `[caption:"BOOM" dur=2 exit=pop]`)
	comp, err := CompileTimeline(conf, []model.WordTiming{}, []model.ProcessedLine{{StartTime: 0, Duration: 2, Actions: actions}}, 8, "/tmp/a.wav")
	if err != nil {
		t.Fatal(err)
	}
	var primary, exit *model.FrameEvent
	for i := range comp.Timeline.Events {
		ev := &comp.Timeline.Events[i]
		if ev.EventType != constant.EVENT_TEXT {
			continue
		}
		switch {
		case ev.ExitFrames > 0:
			exit = ev
		default:
			primary = ev
		}
	}
	if primary == nil || exit == nil {
		t.Fatalf("exit= must emit a second event: primary=%v exit=%v", primary != nil, exit != nil)
	}
	if exit.StartFrame != primary.EndFrame+1 {
		t.Errorf("exit starts at %d, want one frame after the primary window closes at %d", exit.StartFrame, primary.EndFrame)
	}
	if exit.ExitFrames <= 0 || exit.EndFrame <= exit.StartFrame {
		t.Errorf("exit window is empty: %+v", exit)
	}
	if exit.ID != primary.ID+":exit" {
		t.Errorf("exit identity = %q, want %q", exit.ID, primary.ID+":exit")
	}
}

// An exit scheduled after the last frame of the video is reported, not swallowed.
func TestExitPastTheEndIsReported(t *testing.T) {
	conf := genreConf(t)
	actions := mustGenreParse(t, `[caption:"LATE" dur=2 exit=pop]`)
	comp, err := CompileTimeline(conf, []model.WordTiming{}, []model.ProcessedLine{{StartTime: 0, Duration: 2, Actions: actions}}, 1, "/tmp/a.wav")
	if err != nil {
		t.Fatal(err)
	}
	if got := comp.Findings.ByCode(diag.W0010); len(got) != 1 {
		t.Errorf("%d W0010 findings, want 1: %v", len(got), comp.Findings)
	}
}

// A layer that never ends has nothing to exit from; saying so beats dropping the request
// silently, which is what the plan's "documented no-op" trap looks like.
func TestExitOnAPersistentLayerIsReported(t *testing.T) {
	comp := compileGenre(t, `[character:robot exit=fade]`)
	if len(eventsOfType(comp, constant.EVENT_OVERLAY)) != 1 {
		t.Errorf("a dropped exit must not add a phantom event")
	}
	if got := comp.Findings.ByCode(diag.W0009); len(got) != 1 {
		t.Errorf("exit= on a layer without dur= must be reported once, got %d findings", len(got))
	}
}

func TestCaptionKeepsItsPixelSize(t *testing.T) {
	comp := compileGenre(t, `[caption:"IT WEDGES" size=110 color=#FFFF00 outline=8]`)
	if len(comp.TextJobs) != 1 {
		t.Fatalf("%d text jobs, want 1", len(comp.TextJobs))
	}
	job := comp.TextJobs[0]
	if job.FontSize != 110 {
		t.Errorf("FontSize = %v, want the 110 the script asked for", job.FontSize)
	}
	if !job.PixelExact {
		t.Error("a caption must blit at native size, not be scaled to the box")
	}
	if job.OutlinePx != 8 {
		t.Errorf("OutlinePx = %d, want 8", job.OutlinePx)
	}
	if job.ColourHex != "#FFFF00" {
		t.Errorf("ColourHex = %q", job.ColourHex)
	}
	ev := eventsOfType(comp, constant.EVENT_TEXT)[0]
	if ev.FitMode != constant.FIT_NATIVE {
		t.Errorf("FitMode = %q, want %q", ev.FitMode, constant.FIT_NATIVE)
	}
	if ev.Layer != constant.LAYER_OVERLAY {
		t.Errorf("Layer = %q, want the overlay layer", ev.Layer)
	}
}

// [text:] reads the same named parameters, so the two text tags cannot drift apart.
func TestTextAcceptsTheUnifiedTail(t *testing.T) {
	comp := compileGenre(t, `[text:"hello" anchor=TR size=64 color=#123456 id=note]`)
	if len(comp.TextJobs) != 1 {
		t.Fatalf("%d text jobs, want 1", len(comp.TextJobs))
	}
	job := comp.TextJobs[0]
	if job.FontSize != 64 || job.ColourHex != "#123456" {
		t.Errorf("tail not applied: %+v", job)
	}
	ev := eventsOfType(comp, constant.EVENT_TEXT)[0]
	if ev.Layer == constant.LAYER_OVERLAY {
		t.Error("[text:] stays on the board layer; only the genre tags opt out of the camera")
	}
}

// An @ref must address the element, not the canvas. Before compile-time resolution an
// unknown token fell through GetPresetLayout's default branch and silently became "the
// whole canvas", which draws an arrow across the screen.
func TestArrowHitsTheAddressedElement(t *testing.T) {
	conf := genreConf(t)
	place := mustGenreParse(t, "[draw:city id=sky TR] [draw:robot id=hut BL]")
	arrow := mustGenreParse(t, "[arrow:@sky:@hut]")
	lines := []model.ProcessedLine{
		{StartTime: 0, Duration: 1, Actions: place},
		{StartTime: 1, Duration: 1, Actions: arrow},
	}
	comp, err := CompileTimeline(conf, []model.WordTiming{}, lines, 3, "/tmp/a.wav")
	if err != nil {
		t.Fatal(err)
	}
	var draw, arr *model.FrameEvent
	for i := range comp.Timeline.Events {
		ev := &comp.Timeline.Events[i]
		switch {
		case ev.EventType == constant.EVENT_DRAW && ev.ID == "sky":
			draw = ev
		case ev.EventType == constant.EVENT_ARROW:
			arr = ev
		}
	}
	if draw == nil || arr == nil {
		t.Fatalf("missing events: draw=%v arrow=%v", draw != nil, arr != nil)
	}
	wantX, wantY := draw.X+draw.Width/2, draw.Y+draw.Height/2
	gotX, gotY, ok := parsePointArg(arr.ArrowFrom)
	if !ok {
		t.Fatalf("arrow endpoint %q was not resolved to coordinates", arr.ArrowFrom)
	}
	if gotX != wantX || gotY != wantY {
		t.Errorf("arrow starts at %d,%d, want the centre of the addressed element %d,%d", gotX, gotY, wantX, wantY)
	}
	if toX, toY, ok := parsePointArg(arr.ArrowTo); !ok {
		t.Errorf("arrow endpoint %q was not resolved", arr.ArrowTo)
	} else if toX == gotX && toY == gotY {
		t.Error("both endpoints resolved to the same point")
	}
}

// An address that names nothing is an error, not a silent full-canvas annotation.
func TestUnresolvedElementRefIsReported(t *testing.T) {
	comp := compileGenre(t, "[arrow:@ghost:@TR]")
	got := comp.Findings.ByCode(diag.E0013)
	if len(got) != 1 {
		t.Fatalf("%d E0013 findings, want 1: %v", len(got), comp.Findings)
	}
	for _, ev := range comp.Timeline.Events {
		if ev.EventType == constant.EVENT_ARROW {
			t.Error("an unresolvable arrow must be dropped, not drawn across the canvas")
		}
	}
}

func TestHighlightSurroundsTheAddressedElement(t *testing.T) {
	conf := genreConf(t)
	place := mustGenreParse(t, "[character:robot id=bot anchor=br size=40%]")
	mark := mustGenreParse(t, "[highlight:@bot:circle:2]")
	lines := []model.ProcessedLine{
		{StartTime: 0, Duration: 1, Actions: place},
		{StartTime: 1, Duration: 1, Actions: mark},
	}
	comp, err := CompileTimeline(conf, []model.WordTiming{}, lines, 3, "/tmp/a.wav")
	if err != nil {
		t.Fatal(err)
	}
	var hl *model.FrameEvent
	for i := range comp.Timeline.Events {
		if comp.Timeline.Events[i].EventType == constant.EVENT_HIGHLIGHT {
			hl = &comp.Timeline.Events[i]
		}
	}
	if hl == nil {
		t.Fatal("no highlight event")
	}
	x, y, w, h, ok := parseRegionArg(hl.TargetImage)
	if !ok {
		t.Fatalf("highlight region %q is not resolved coordinates", hl.TargetImage)
	}
	if w < 300 || w > 500 || h < 150 || h > 250 {
		t.Errorf("highlight region %dx%d does not wrap the 400x200 character box", w, h)
	}
	if x+w > 1000 || y+h > 500 {
		t.Errorf("highlight box %d,%d %dx%d escapes the canvas", x, y, w, h)
	}
	if x >= 1000 || y >= 500 {
		t.Errorf("highlight at %d,%d is off canvas", x, y)
	}
}

// parsePointArg and parseRegionArg read back the coordinate spellings the compiler emits.
func parsePointArg(s string) (int, int, bool) {
	v, ok := parseIntList(s, 2)
	if !ok {
		return 0, 0, false
	}
	return v[0], v[1], true
}

func parseRegionArg(s string) (int, int, int, int, bool) {
	v, ok := parseIntList(s, 4)
	if !ok {
		return 0, 0, 0, 0, false
	}
	return v[0], v[1], v[2], v[3], true
}

func parseIntList(s string, want int) ([]int, bool) {
	fields := strings.Split(s, ",")
	if len(fields) != want {
		return nil, false
	}
	out := make([]int, 0, want)
	for _, f := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// One of w=/h= alone cannot describe a box, and silently falling back to the anchor box
// would leave the author believing the number did something.
func TestHalfABoxIsReported(t *testing.T) {
	comp := compileGenre(t, "[sticker:robot anchor=tr w=220]")
	if got := comp.Findings.ByCode(diag.W0012); len(got) != 1 {
		t.Errorf("%d W0012 findings, want 1: %v", len(got), comp.Findings)
	}
	comp = compileGenre(t, "[sticker:robot anchor=tr w=220 h=160]")
	if got := comp.Findings.ByCode(diag.W0012); len(got) != 0 {
		t.Errorf("a complete box was reported: %v", got)
	}
	ev := eventsOfType(comp, constant.EVENT_OVERLAY)[0]
	if ev.Width != 220 || ev.Height != 160 {
		t.Errorf("w=/h= gave %dx%d, want 220x160", ev.Width, ev.Height)
	}
}

// A top-corner sticker must hang from the top line, not float in the middle of its band.
func TestCornerAnchorsAlignToTheCorner(t *testing.T) {
	for _, tc := range []struct {
		anchor       string
		x, y, w, h   int
		wantX, wantY int
	}{
		{"tr", 0, 0, 100, 100, 900, 0},
		{"tl", 0, 0, 100, 100, 0, 0},
		{"br", 0, 0, 100, 100, 900, 400},
		{"bl", 0, 0, 100, 100, 0, 400},
	} {
		comp := compileGenre(t, "[sticker:robot anchor="+tc.anchor+" w=100 h=100]")
		ev := eventsOfType(comp, constant.EVENT_OVERLAY)[0]
		if ev.X != tc.wantX || ev.Y != tc.wantY {
			t.Errorf("anchor=%s placed the layer at %d,%d, want %d,%d (canvas 1000x500)", tc.anchor, ev.X, ev.Y, tc.wantX, tc.wantY)
		}
		if ev.Width != 100 || ev.Height != 100 {
			t.Errorf("anchor=%s sized the layer %dx%d, want 100x100", tc.anchor, ev.Width, ev.Height)
		}
	}
}

// An outgoing character state must not leave its exit animation running over the new one.
func TestStateSwapClosesTheOutgoingExit(t *testing.T) {
	conf := genreConf(t)
	first := mustGenreParse(t, `[character:robot_happy id=bot dur=2 exit=fade]`)
	second := mustGenreParse(t, "[character:robot_thinking id=bot]")
	comp, err := CompileTimeline(conf, []model.WordTiming{}, []model.ProcessedLine{
		{StartTime: 0, Duration: 1, Actions: first},
		{StartTime: 2, Duration: 1, Actions: second},
	}, 8, "/tmp/a.wav")
	if err != nil {
		t.Fatal(err)
	}
	var swap, exit *model.FrameEvent
	for i := range comp.Timeline.Events {
		ev := &comp.Timeline.Events[i]
		switch ev.ID {
		case "guide", "bot:exit":
			if ev.ExitFrames > 0 {
				exit = ev
			}
		case "bot":
			if ev.StartFrame > 0 {
				swap = ev
			}
		}
	}
	if exit == nil || swap == nil {
		t.Fatalf("missing events: swap=%v exit=%v", swap, exit)
	}
	if exit.EndFrame >= swap.StartFrame {
		t.Errorf("the outgoing exit runs to %d, past the new state starting at %d", exit.EndFrame, swap.StartFrame)
	}
}

// A named parameter that used to travel in the SVG variant map still reaches the event -
// and now reaches it through the grammar, so an unknown cursor name is a finding.
func TestLegacyVariantKeysStillWork(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"[draw:robot cursor=magic_wand]", "magic_wand"},
		{"[draw:robot:cursor=magic_wand:TR]", "magic_wand"},
		{"[draw:robot]", constant.CURSOR_HAND},
	} {
		comp := compileGenre(t, tc.text)
		var got *model.FrameEvent
		for i := range comp.Timeline.Events {
			if comp.Timeline.Events[i].EventType == constant.EVENT_DRAW {
				got = &comp.Timeline.Events[i]
			}
		}
		if got == nil {
			t.Fatalf("%s: no draw event", tc.text)
		}
		if got.Cursor != tc.want {
			t.Errorf("%s: cursor = %q, want %q", tc.text, got.Cursor, tc.want)
		}
	}
}
