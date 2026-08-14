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
	_, _ = w.WriteString("# zen-board eventlog v2: action - start_sec - end_sec\n")
	return &EventLogger{w: w, f: f}, nil
}

func (l *EventLogger) LogTransition(dir string, frame int, ev model.FrameEvent, fps int) {
	if l == nil {
		return
	}
	if l.broken.Load() {
		return
	}
	if dir != "EXIT" {
		return
	}
	rate := fps
	if rate <= 0 {
		rate = 1
	}
	startFrame := ev.StartFrame
	if startFrame < 0 {
		startFrame = 0
	}
	if startFrame > frame {
		startFrame = frame
	}
	_, err := fmt.Fprintf(l.w, "%s - %.3f - %.3f\n",
		ev.EventType, float64(startFrame)/float64(rate), float64(frame)/float64(rate))
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
