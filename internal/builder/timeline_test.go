package builder

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"zen-board/internal/model"
	"zen-board/internal/render"
	"zen-board/internal/script"
)

func pLineAt(t *testing.T, start float64, line string) model.ProcessedLine {
	t.Helper()
	parsed := script.Parse(line)
	if len(parsed) != 1 {
		t.Fatalf("script.Parse returned %d lines for %q, want 1", len(parsed), line)
	}
	return model.ProcessedLine{StartTime: start, WordOffset: 0, Actions: parsed[0].Actions}
}

func compileMarkers(t *testing.T, lines ...model.ProcessedLine) *model.Timeline {
	t.Helper()
	conf := model.NewDefaultProject()
	conf.FPS = 30
	conf.AssetsDir = t.TempDir()
	comp, err := CompileTimeline(conf, nil, lines, 10.0, "")
	if err != nil {
		t.Fatalf("CompileTimeline: %v", err)
	}
	return comp.Timeline
}

func markersOf(t *testing.T, tl *model.Timeline, eventType string) []model.FrameEvent {
	t.Helper()
	var out []model.FrameEvent
	for _, ev := range tl.Events {
		if ev.EventType == eventType {
			out = append(out, ev)
		}
	}
	return out
}

func TestCompileTimelineEmitsMarkerEventsForNonVisualActions(t *testing.T) {
	tl := compileMarkers(t,
		pLineAt(t, 1.0, `[chapter:"Intro"]`),
		pLineAt(t, 2.0, `[style:blackboard]`),
		pLineAt(t, 3.0, `[clear]`),
		pLineAt(t, 4.0, `[erase:*]`),
	)

	check := func(eventType string, startFrame, endFrame int) {
		ms := markersOf(t, tl, eventType)
		if len(ms) != 1 {
			t.Fatalf("event type %q: got %d marker events, want 1", eventType, len(ms))
		}
		m := ms[0]
		if m.StartFrame != startFrame {
			t.Errorf("event type %q: StartFrame = %d, want %d", eventType, m.StartFrame, startFrame)
		}
		// Later wipes clamp prior persisted windows: chapter/style end at the
		// following clear, clear ends at the following erase:*.
		if m.EndFrame != endFrame {
			t.Errorf("event type %q: EndFrame = %d, want %d", eventType, m.EndFrame, endFrame)
		}
		if m.TargetImage != "" {
			t.Errorf("event type %q: TargetImage = %q, want empty (must be inert for asset validation/render)", eventType, m.TargetImage)
		}
	}

	check("chapter", 30, 90)
	check("style", 60, 90)
	check("clear", 90, 120)
	check("erase_all", 120, 999999)
}

func TestCompileTimelineMarkerEventsDoNotFailAssetValidation(t *testing.T) {
	// AssetsDir is an empty temp dir; marker events must not be treated as
	// missing assets, otherwise CompileTimeline would return an error.
	tl := compileMarkers(t,
		pLineAt(t, 0.0, `[chapter:"Intro"]`),
		pLineAt(t, 1.0, `[style:whiteboard]`),
		pLineAt(t, 2.0, `[clear]`),
	)
	if len(tl.Events) != 3 {
		t.Fatalf("got %d events, want 3 marker events", len(tl.Events))
	}
}

func TestClearClampsPriorMarkerEndFrames(t *testing.T) {
	tl := compileMarkers(t,
		pLineAt(t, 0.0, `[chapter:"A"]`),
		pLineAt(t, 1.0, `[style:blackboard]`),
		pLineAt(t, 2.0, `[clear]`),
	)

	chapter := markersOf(t, tl, "chapter")
	if len(chapter) != 1 {
		t.Fatalf("chapter markers: got %d, want 1", len(chapter))
	}
	if chapter[0].EndFrame != 60 {
		t.Errorf("chapter marker EndFrame = %d after clear, want 60 (clamped at clear frame)", chapter[0].EndFrame)
	}

	clear := markersOf(t, tl, "clear")
	if len(clear) != 1 {
		t.Fatalf("clear markers: got %d, want 1", len(clear))
	}
	if clear[0].StartFrame != 60 {
		t.Errorf("clear marker StartFrame = %d, want 60", clear[0].StartFrame)
	}
}

// collectExitLines mirrors the ENTER/EXIT scan in builder.RenderTimeline's
// feeder goroutine (renderer.go) and returns the eventlog lines that the
// EventLogger would write for each event's EXIT. It pins the end-to-end
// contract: markers compiled by CompileTimeline surface as eventlog lines.
func collectExitLines(tl *model.Timeline, fps, totalFrames int) []string {
	for i := range tl.Events {
		if tl.Events[i].EndFrame >= totalFrames {
			tl.Events[i].EndFrame = totalFrames - 1
		}
	}
	prevActive := make(map[int]bool)
	var lines []string
	for f := 0; f < totalFrames; f++ {
		for i, ev := range tl.Events {
			nowActive := f >= ev.StartFrame && f <= ev.EndFrame
			if !nowActive && prevActive[i] {
				lines = append(lines, fmt.Sprintf("%s - %.3f - %.3f",
					ev.EventType, float64(ev.StartFrame)/float64(fps), float64(f)/float64(fps)))
			}
			prevActive[i] = nowActive
		}
	}
	for i, ev := range tl.Events {
		if prevActive[i] {
			lines = append(lines, fmt.Sprintf("%s - %.3f - %.3f",
				ev.EventType, float64(ev.StartFrame)/float64(fps), float64(totalFrames-1)/float64(fps)))
		}
	}
	return lines
}

func TestMarkerEventsSurfaceInEventlog(t *testing.T) {
	tl := compileMarkers(t,
		pLineAt(t, 0.0, `[chapter:"Intro"]`),
		pLineAt(t, 1.0, `[style:blackboard]`),
		pLineAt(t, 2.0, `[clear]`),
		pLineAt(t, 3.0, `[erase:*]`),
		pLineAt(t, 4.0, `[chapter:"End"]`),
	)

	lines := collectExitLines(tl, 30, 180)
	want := []string{
		"chapter - 0.000 - 2.033",
		"style - 1.000 - 2.033",
		"clear - 2.000 - 3.033",
		"erase_all - 3.000 - 5.967",
		"chapter - 4.000 - 5.967",
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d eventlog lines, want %d:\n%s", len(lines), len(want), strings.Join(lines, "\n"))
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestMarkerEventsRenderInert(t *testing.T) {
	engine, err := render.NewEngine(100, 100, 30, render.NewCursorRegistry(t.TempDir()), "hand")
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	markers := []model.FrameEvent{
		{EventType: "chapter", StartFrame: 0, EndFrame: 999999},
		{EventType: "style", StartFrame: 0, EndFrame: 999999},
		{EventType: "clear", StartFrame: 0, EndFrame: 999999},
		{EventType: "erase_all", StartFrame: 0, EndFrame: 999999},
	}

	frame := engine.RenderFrame(30, markers, render.CameraState{}, "whiteboard")
	if frame == nil {
		t.Fatal("RenderFrame returned nil")
	}
	if frame.Bounds().Dx() != 100 || frame.Bounds().Dy() != 100 {
		t.Fatalf("frame size = %dx%d, want 100x100", frame.Bounds().Dx(), frame.Bounds().Dy())
	}
	want := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for _, p := range []struct{ x, y int }{{0, 0}, {50, 50}, {99, 99}} {
		if got := frame.RGBAAt(p.x, p.y); got != want {
			t.Errorf("pixel (%d,%d) = %v, want %v (marker events must not draw)", p.x, p.y, got, want)
		}
	}
}