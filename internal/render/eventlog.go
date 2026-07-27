package render

import (
	"bufio"
	"fmt"
	"os"
	"sync/atomic"

	"zen-board/internal/model"
)

type EventLogger struct {
	w      *bufio.Writer
	f      *os.File
	broken atomic.Bool
}

func NewEventLogger(path string) (*EventLogger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	w := bufio.NewWriter(f)
	_, _ = w.WriteString("# zen-board eventlog v1\n")
	_, _ = w.WriteString("# dir\tframe\tts_sec\tevent_type\ttarget\tx,y\tzoom_focus\n")
	return &EventLogger{w: w, f: f}, nil
}

func (l *EventLogger) LogTransition(dir string, frame int, ev model.FrameEvent, fps int) {
	if l == nil {
		return
	}
	if l.broken.Load() {
		return
	}
	tsSec := float64(frame) / float64(fps)
	target := enrichTarget(ev)
	_, err := fmt.Fprintf(l.w, "%s\t%d\t%.3f\t%s\t%s\t%d,%d\t%s\n",
		dir, frame, tsSec, ev.EventType, target, ev.X, ev.Y, ev.ZoomFocus)
	if err != nil {
		l.broken.Store(true)
		fmt.Fprintf(os.Stderr, "[eventlog] write error: %v; logging disabled\n", err)
	}
}

func (l *EventLogger) Close() error {
	if l == nil {
		return nil
	}
	var err error
	if l.w != nil {
		err = l.w.Flush()
	}
	if l.f != nil {
		if cerr := l.f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	return err
}

func enrichTarget(ev model.FrameEvent) string {
	switch ev.EventType {
	case "draw", "erase", "static", "move", "text", "gen":
		return ev.TargetImage
	case "slide":
		return ev.TargetImage + "/" + ev.Transition
	case "lower3rd":
		return ev.TargetImage
	case "arrow", "arrow_static":
		return ev.ArrowFrom + "→" + ev.ArrowTo
	case "highlight":
		return ev.TargetImage + "/" + ev.HighlightStyle
	case "compare":
		return ev.CompareLeft + "|" + ev.CompareRight
	case "overlay":
		return ev.TargetImage
	case "transition":
		return ev.TransitionType
	case "counter":
		return ev.CounterFormat
	default:
		return ev.TargetImage
	}
}
