package inspect

import (
	"encoding/json"
	"sort"

	"zen-board/internal/builder"
	"zen-board/internal/constant"
	"zen-board/internal/model"
	"zen-board/internal/script"
)

// TimelineDoc is the compiled timeline as data: what an agent reads instead of
// watching a video. It is built straight after compile, so the asset column shows the
// id from the script and never a rasterisation key.
type TimelineDoc struct {
	Script      string    `json:"script"`
	ScriptHash  string    `json:"script_hash"`
	FPS         int       `json:"fps"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	DurationSec float64   `json:"duration_sec"`
	TotalFrames int       `json:"total_frames"`
	Chapters    []Chapter `json:"chapters,omitempty"`
	Events      []Event   `json:"events"`
	Gaps        []Gap     `json:"gaps,omitempty"`
	Words       []Word    `json:"words,omitempty"`
}

// Chapter is an ffmetadata chapter marker.
type Chapter struct {
	Title      string  `json:"title"`
	StartTime  float64 `json:"start_sec"`
	StartFrame int     `json:"start_frame"`
}

// Event is one compiled frame event, attributed back to the tag that produced it.
type Event struct {
	Index      int     `json:"index"`
	Type       string  `json:"type"`
	Asset      string  `json:"asset,omitempty"`
	ID         string  `json:"id,omitempty"`
	StartFrame int     `json:"start_frame"`
	EndFrame   int     `json:"end_frame"`
	StartSec   float64 `json:"start_sec"`
	EndSec     float64 `json:"end_sec"`
	Rect       *Rect   `json:"rect,omitempty"`
	Dest       *Point  `json:"dest,omitempty"`
	// CameraFocus and LayoutPreset are split deliberately. FrameEvent.ZoomFocus is
	// overloaded today: the overlay and counter handlers store a *layout* key in the
	// same field the camera reads, so a naive dump would report a box as a camera move
	// and teach the reader wrong.
	CameraFocus  string `json:"camera_focus,omitempty"`
	LayoutPreset string `json:"layout_preset,omitempty"`
	// Layer and Sticky describe the genre semantics: an overlay-layer event ignores the
	// camera, a sticky one survives the board wipe.
	Layer  string `json:"layer,omitempty"`
	Sticky bool   `json:"sticky,omitempty"`
	// ExitFrames / ExitKind are set on the exit event exit= produced, not on the layer
	// it follows: an exit is a second event because a cut would otherwise swallow it.
	ExitFrames int     `json:"exit_frames,omitempty"`
	ExitKind   string  `json:"exit_kind,omitempty"`
	MaskStyle  string  `json:"mask_style,omitempty"`
	HandStyle  string  `json:"hand_style,omitempty"`
	Cursor     string  `json:"cursor,omitempty"`
	Transition string  `json:"transition,omitempty"`
	RevealSec  float64 `json:"reveal_sec,omitempty"`
	Persistent bool    `json:"persistent,omitempty"`
	SrcLine    int     `json:"src_line,omitempty"`
	SrcCol     int     `json:"src_col,omitempty"`
}

// Rect is the canvas box of an event.
type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Point is a canvas position.
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Gap is a stretch of video with no visual event - the most common cause of a
// "the screen looks empty" report.
type Gap struct {
	StartFrame int     `json:"start_frame"`
	EndFrame   int     `json:"end_frame"`
	StartSec   float64 `json:"start_sec"`
	Duration   float64 `json:"duration_sec"`
}

// Word is one narration word with its absolute timing.
type Word struct {
	Word  string  `json:"word"`
	Start float64 `json:"start_sec"`
	End   float64 `json:"end_sec"`
}

// layoutOnlyEvents are the event types that store a layout key in ZoomFocus.
var layoutOnlyEvents = map[string]bool{
	constant.EVENT_OVERLAY:   true,
	constant.EVENT_COUNTER:   true,
	constant.EVENT_WATERMARK: true,
}

// BuildTimeline converts a compilation into the reportable document.
func BuildTimeline(comp *builder.TimelineCompilation, duration float64, totalFrames int, conf *model.Project, src *script.Source) *TimelineDoc {
	sec := func(frame int) float64 {
		if conf.FPS <= 0 {
			return 0
		}
		return float64(frame) / float64(conf.FPS)
	}
	doc := &TimelineDoc{
		FPS: conf.FPS, Width: conf.Width, Height: conf.Height,
		DurationSec: duration, TotalFrames: totalFrames,
	}
	if src != nil {
		doc.Script = src.Path
		doc.ScriptHash = src.Hash()
	}
	for _, ch := range comp.Chapters {
		doc.Chapters = append(doc.Chapters, Chapter{
			Title: ch.Title, StartTime: ch.StartTime,
			StartFrame: int(ch.StartTime * float64(conf.FPS)),
		})
	}
	for i, ev := range comp.Timeline.Events {
		out := Event{
			Index: i, Type: ev.EventType, Asset: ev.TargetImage, ID: ev.ID,
			StartFrame: ev.StartFrame, EndFrame: ev.EndFrame,
			StartSec: sec(ev.StartFrame), EndSec: sec(ev.EndFrame),
			Layer: ev.Layer, Sticky: ev.Sticky, ExitFrames: ev.ExitFrames, ExitKind: ev.ExitKind,
			MaskStyle: ev.MaskStyle, HandStyle: ev.HandStyle, Cursor: ev.Cursor,
			Transition: ev.Transition, RevealSec: ev.RevealDuration,
			Persistent: ev.EndFrame >= totalFrames-1,
			SrcLine:    ev.SrcLine, SrcCol: ev.SrcCol,
		}
		if ev.X != 0 || ev.Y != 0 || ev.Width != 0 || ev.Height != 0 {
			out.Rect = &Rect{X: ev.X, Y: ev.Y, W: ev.Width, H: ev.Height}
		}
		if ev.DestX != 0 || ev.DestY != 0 {
			out.Dest = &Point{X: ev.DestX, Y: ev.DestY}
		}
		if ev.EventType == constant.EVENT_COMPARE {
			out.Asset = ev.CompareLeft + " | " + ev.CompareRight
		}
		if ev.EventType == constant.EVENT_ARROW || ev.EventType == constant.EVENT_ARROW_STATIC {
			out.Asset = ev.ArrowFrom + " -> " + ev.ArrowTo
		}
		if layoutOnlyEvents[ev.EventType] {
			out.LayoutPreset = ev.ZoomFocus
		} else {
			out.CameraFocus = ev.ZoomFocus
			if ev.ZoomFocus == constant.FOCUS_RESET {
				out.CameraFocus = ""
			}
		}
		doc.Events = append(doc.Events, out)
	}
	for _, w := range comp.Timeline.Words {
		doc.Words = append(doc.Words, Word{Word: w.Word, Start: w.Start, End: w.End})
	}
	doc.Gaps = findGaps(comp.Timeline.Events, totalFrames, conf.FPS)
	return doc
}

// findGaps reports stretches where no visual event is on screen.
func findGaps(events []model.FrameEvent, totalFrames, fps int) []Gap {
	occupied := make([]bool, totalFrames)
	for _, ev := range events {
		if !visualEvent(ev) {
			continue
		}
		for f := ev.StartFrame; f <= ev.EndFrame && f < totalFrames; f++ {
			if f >= 0 {
				occupied[f] = true
			}
		}
	}
	var gaps []Gap
	start := -1
	for f := 0; f < totalFrames; f++ {
		if !occupied[f] {
			if start < 0 {
				start = f
			}
			continue
		}
		if start >= 0 {
			gaps = append(gaps, Gap{StartFrame: start, EndFrame: f - 1, StartSec: seconds(start, fps), Duration: seconds(f-start, fps)})
			start = -1
		}
	}
	if start >= 0 {
		gaps = append(gaps, Gap{StartFrame: start, EndFrame: totalFrames - 1, StartSec: seconds(start, fps), Duration: seconds(totalFrames-start, fps)})
	}
	return gaps
}

func seconds(n, fps int) float64 {
	if fps <= 0 {
		return 0
	}
	return float64(n) / float64(fps)
}

// JSON renders the document.
func (d *TimelineDoc) JSON() ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}

// EventByID is a convenience for tests and for @ref resolution.
func (d *TimelineDoc) EventByID(id string) (Event, bool) {
	for _, ev := range d.Events {
		if ev.ID == id {
			return ev, true
		}
	}
	return Event{}, false
}

// FrameAt maps a time to a frame number, clamped to the video.
func (d *TimelineDoc) FrameAt(sec float64) int {
	f := int(sec * float64(d.FPS))
	if f < 0 {
		f = 0
	}
	if f > d.TotalFrames-1 {
		f = d.TotalFrames - 1
	}
	return f
}

// SortedEvents returns events ordered by start frame; the compiled order is authoring
// order, which is what the renderer consumes, so this is only for readers.
func (d *TimelineDoc) SortedEvents() []Event {
	out := append([]Event(nil), d.Events...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartFrame < out[j].StartFrame })
	return out
}
