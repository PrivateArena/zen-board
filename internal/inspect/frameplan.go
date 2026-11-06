// Package inspect turns a compiled timeline into artifacts an agent can read:
// PNGs of chosen frames, a contact sheet, and the event timeline as data.
package inspect

import (
	"fmt"
	"sort"
	"strconv"

	"zen-board/internal/constant"
	"zen-board/internal/model"
)

// EventSelector narrows a request to the interesting moments of each event.
type EventSelector string

// Selector values.
const (
	EventsStart EventSelector = "start"
	EventsMid   EventSelector = "mid"
	EventsEnd   EventSelector = "end"
	EventsAll   EventSelector = "all"
	// EventsNone plans only what -at / -frame / -every asked for. Without it,
	// `snapshot -frame 90` would also return every event moment, which is not what
	// "give me this one frame" means.
	EventsNone EventSelector = "none"
)

// SelectorOf validates a user-supplied selector.
func SelectorOf(v string) (EventSelector, error) {
	switch EventSelector(v) {
	case EventsStart, EventsMid, EventsEnd, EventsAll, EventsNone:
		return EventSelector(v), nil
	case "":
		return EventsAll, nil
	}
	return "", fmt.Errorf("unknown -events value %q: use start, mid, end, all or none", v)
}

// Request is the frame-addressing request of one snapshot run. It is plain data so
// the planner can be tested without an engine, a pipe or a filesystem.
type Request struct {
	// Selector picks event-relative moments.
	Selector EventSelector
	// At lists explicit selections: "3s" or "120" (frame).
	At []string
	// EverySeconds samples the whole video at an interval; 0 disables sampling.
	EverySeconds float64
	// Frame is a single explicit frame, or negative for "not given". Callers that
	// parse a flag must default it to -1, because frame 0 is a legitimate request.
	Frame int
	// Max caps the number of frames. Truncation is reported, never silent.
	Max int
}

// FramePlan is the resolved frame plan.
type FramePlan struct {
	Frames  []int
	Dropped int
	// Labels maps a frame to a human-readable reason it was selected, so a
	// manifest can say why a frame exists.
	Labels map[int]string
}

// Plan resolves a request against a compiled timeline.
//
// It is a pure function over plain data and it runs entirely before any pixel work,
// which is what bounds the cost of a snapshot: -max is applied here, not after
// rendering. Preconditions: EndFrame sentinels must already be clamped to
// totalFrames-1 by the compile stage.
func Plan(fps, totalFrames int, events []model.FrameEvent, r Request) (*FramePlan, error) {
	if fps <= 0 {
		return nil, fmt.Errorf("fps must be positive, got %d", fps)
	}
	if totalFrames <= 0 {
		return nil, fmt.Errorf("total frames must be positive, got %d", totalFrames)
	}
	if r.Max <= 0 {
		return nil, fmt.Errorf("-max must be positive, got %d", r.Max)
	}
	plan := &FramePlan{Labels: map[int]string{}}
	add := func(frame int, label string) {
		if frame < 0 {
			frame = 0
		}
		if frame > totalFrames-1 {
			frame = totalFrames - 1
		}
		if _, seen := plan.Labels[frame]; !seen {
			plan.Labels[frame] = label
			plan.Frames = append(plan.Frames, frame)
		}
	}

	if r.Frame >= 0 {
		add(r.Frame, fmt.Sprintf("explicit frame %d", r.Frame))
	}

	for _, spec := range r.At {
		frame, err := parseSelection(spec, fps)
		if err != nil {
			return nil, err
		}
		add(frame, "selected "+spec)
	}

	if r.EverySeconds > 0 {
		step := int(r.EverySeconds * float64(fps))
		if step < 1 {
			step = 1
		}
		for f := 0; f < totalFrames; f += step {
			add(f, fmt.Sprintf("every %gs", r.EverySeconds))
		}
	}

	switch r.Selector {
	case EventsStart, EventsMid, EventsEnd, EventsAll:
		for i, ev := range events {
			if !visualEvent(ev) {
				continue
			}
			for _, sel := range selectorsFor(r.Selector) {
				frame := eventFrame(ev, sel, totalFrames)
				add(frame, fmt.Sprintf("%s of event %d (%s%s)", sel, i, ev.EventType, eventIDSuffix(ev)))
			}
		}
	}

	sort.Ints(plan.Frames)
	if len(plan.Frames) > r.Max {
		plan.Dropped = len(plan.Frames) - r.Max
		plan.Frames = plan.Frames[:r.Max]
	}
	return plan, nil
}

func selectorsFor(sel EventSelector) []EventSelector {
	switch sel {
	case EventsStart:
		return []EventSelector{EventsStart}
	case EventsMid:
		return []EventSelector{EventsMid}
	case EventsEnd:
		return []EventSelector{EventsEnd}
	default:
		return []EventSelector{EventsStart, EventsMid, EventsEnd}
	}
}

// eventFrame resolves one moment of an event. A persistence event whose EndFrame is
// the "forever" sentinel resolves to the last frame of the video - never to 999998,
// which would be off the end of the timeline.
func eventFrame(ev model.FrameEvent, sel EventSelector, totalFrames int) int {
	last := totalFrames - 1
	if ev.EndFrame > last || ev.EndFrame == constant.FRAME_FOREVER {
		if sel == EventsEnd {
			return last
		}
	}
	end := ev.EndFrame
	if end > last {
		end = last
	}
	switch sel {
	case EventsStart:
		return ev.StartFrame
	case EventsEnd:
		if end == constant.FRAME_FOREVER {
			return last
		}
		return end
	default:
		if end <= ev.StartFrame {
			return ev.StartFrame
		}
		return ev.StartFrame + (end-ev.StartFrame)/2
	}
}

// visualEvent filters out the inert markers the compiler emits for the event log:
// they have no pixels to look at.
func visualEvent(ev model.FrameEvent) bool {
	switch ev.EventType {
	case constant.EVENT_STYLE, constant.EVENT_CHAPTER, constant.EVENT_CLEAR, constant.EVENT_ERASE_ALL:
		return false
	}
	return true
}

func eventIDSuffix(ev model.FrameEvent) string {
	if ev.ID == "" {
		return ""
	}
	return " id=" + ev.ID
}

// parseSelection reads "3s", "10.5s" or a bare frame number.
func parseSelection(spec string, fps int) (int, error) {
	if spec == "" {
		return 0, fmt.Errorf("empty selection")
	}
	if spec[len(spec)-1] == 's' {
		secs, err := parseFloat(spec[:len(spec)-1])
		if err != nil {
			return 0, fmt.Errorf("bad time selection %q", spec)
		}
		return int(secs * float64(fps)), nil
	}
	frame, err := parseInt(spec)
	if err != nil {
		return 0, fmt.Errorf("bad selection %q: use a frame number or a time like 3.5s", spec)
	}
	return frame, nil
}

func parseInt(s string) (int, error) { return strconv.Atoi(s) }

func parseFloat(s string) (float64, error) { return strconv.ParseFloat(s, 64) }
