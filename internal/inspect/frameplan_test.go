package inspect

import (
	"testing"

	"zen-board/internal/constant"
	"zen-board/internal/model"
)

const hourFPS = 30

func hourTimeline() []model.FrameEvent {
	return []model.FrameEvent{
		{EventType: constant.EVENT_DRAW, StartFrame: 0, EndFrame: 60, ID: "draw#1"},
		{EventType: constant.EVENT_STATIC, StartFrame: 60, EndFrame: constant.FRAME_FOREVER, ID: "draw#1"},
	}
}

func TestPlanCapsBeforeAnyPixelWork(t *testing.T) {
	total := 60 * 60 * hourFPS // one hour
	plan, err := Plan(hourFPS, total, hourTimeline(), Request{
		Selector:     EventsAll,
		EverySeconds: 0.5,
		Max:          24,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Frames) > 24 {
		t.Errorf("plan produced %d frames, must be capped at -max 24", len(plan.Frames))
	}
	if plan.Dropped == 0 {
		t.Error("truncation must be reported, not silent")
	}
	prev := -1
	for _, f := range plan.Frames {
		if f <= prev {
			t.Errorf("frames are not ascending: %d after %d", f, prev)
		}
		if f < 0 || f >= total {
			t.Errorf("frame %d outside [0,%d)", f, total)
		}
		prev = f
	}
}

// A `[draw:]` reveal persists forever; its "end" is the last frame of the video, not
// the 999998 sentinel, and never a frame that will not be rendered.
func TestPlanResolvesForeverEventsToTheLastFrame(t *testing.T) {
	total := 300
	events := []model.FrameEvent{
		{EventType: constant.EVENT_DRAW, StartFrame: 10, EndFrame: 40, ID: "robot"},
		{EventType: constant.EVENT_STATIC, StartFrame: 40, EndFrame: constant.FRAME_FOREVER, ID: "robot"},
	}
	plan, err := Plan(30, total, events, Request{Selector: EventsEnd, Frame: -1, Max: 50})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range plan.Frames {
		if f == total-1 {
			found = true
		}
		if f == constant.FRAME_FOREVER || f >= total {
			t.Errorf("plan contains the unclamped sentinel: %d", f)
		}
	}
	if !found {
		t.Errorf("a persisted layer's end must resolve to the last frame %d, got %v", total-1, plan.Frames)
	}

	mid, err := Plan(30, total, events, Request{Selector: EventsMid, Frame: -1, Max: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range mid.Frames {
		if f >= total {
			t.Errorf("mid resolved past the end: %d", f)
		}
	}
}

func TestPlanDrawEndPicksLastRevealedFrame(t *testing.T) {
	total := 300
	events := []model.FrameEvent{{EventType: constant.EVENT_DRAW, StartFrame: 30, EndFrame: 90}}
	plan, err := Plan(30, total, events, Request{Selector: EventsEnd, Frame: -1, Max: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Frames) != 1 || plan.Frames[0] != 90 {
		t.Errorf("-events end on a reveal must pick the last frame of the reveal, got %v", plan.Frames)
	}
}

func TestPlanSelectorsAndExplicitRequests(t *testing.T) {
	total := 100
	events := []model.FrameEvent{{EventType: constant.EVENT_TEXT, StartFrame: 10, EndFrame: 20, ID: "t"}}
	plan, err := Plan(10, total, events, Request{Selector: EventsStart, Frame: -1, Max: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Frames) != 1 || plan.Frames[0] != 10 {
		t.Errorf("-events start must pick the reveal frame, got %v", plan.Frames)
	}

	plan, err = Plan(10, total, events, Request{Selector: EventsAll, At: []string{"3s", "42"}, Frame: -1, Max: 10})
	if err != nil {
		t.Fatal(err)
	}
	// 3s at 10fps = 30, plus 42, plus the three event moments (10, 15, 20).
	want := map[int]bool{10: true, 15: true, 20: true, 30: true, 42: true}
	if len(plan.Frames) != len(want) {
		t.Errorf("got %v, want the %d distinct selections", plan.Frames, len(want))
	}
	for _, f := range plan.Frames {
		if !want[f] {
			t.Errorf("unexpected frame %d in %v", f, plan.Frames)
		}
	}
	if plan.Labels[42] == "" {
		t.Error("every planned frame must carry a label explaining why it exists")
	}
}

func TestPlanRejectsBadInput(t *testing.T) {
	cases := []struct {
		name       string
		fps, total int
		req        Request
	}{
		{"zero fps", 0, 10, Request{Selector: EventsAll, Frame: -1, Max: 5}},
		{"zero frames", 30, 0, Request{Selector: EventsAll, Frame: -1, Max: 5}},
		{"zero max", 30, 10, Request{Selector: EventsAll, Max: 0}},
		{"bad time", 30, 10, Request{Selector: EventsAll, At: []string{"abc"}, Frame: -1, Max: 5}},
	}
	for _, tc := range cases {
		if _, err := Plan(tc.fps, tc.total, nil, tc.req); err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
	if _, err := SelectorOf("sometimes"); err == nil {
		t.Error("unknown selector must be rejected")
	}
}

func TestPlanSkipsInertMarkers(t *testing.T) {
	events := []model.FrameEvent{
		{EventType: constant.EVENT_STYLE, StartFrame: 5, EndFrame: constant.FRAME_FOREVER},
		{EventType: constant.EVENT_CHAPTER, StartFrame: 6, EndFrame: constant.FRAME_FOREVER},
		{EventType: constant.EVENT_CLEAR, StartFrame: 7, EndFrame: constant.FRAME_FOREVER},
		{EventType: constant.EVENT_DRAW, StartFrame: 8, EndFrame: 9},
	}
	plan, err := Plan(30, 50, events, Request{Selector: EventsAll, Frame: -1, Max: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range plan.Frames {
		if f == 5 || f == 6 || f == 7 {
			t.Errorf("inert marker frame %d must not be planned; nothing to look at", f)
		}
	}
}
