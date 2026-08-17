package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zen-board/internal/model"
)

func readLogFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	return string(b)
}

func newTestLogger(t *testing.T) (*EventLogger, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.log")
	l, err := NewEventLogger(path)
	if err != nil {
		t.Fatalf("NewEventLogger: %v", err)
	}
	return l, path
}

func TestEventLoggerWritesHeader(t *testing.T) {
	l, path := newTestLogger(t)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, want := readLogFile(t, path), "# zen-board eventlog v2: action - start_sec - end_sec\n"; got != want {
		t.Errorf("header = %q, want %q", got, want)
	}
}

func TestLogTransitionConsolidatesIntoOneLine(t *testing.T) {
	l, path := newTestLogger(t)
	ev := model.FrameEvent{EventType: "slide", StartFrame: 120, EndFrame: 240}
	l.LogTransition("ENTER", 120, ev, 30)
	l.LogTransition("EXIT", 240, ev, 30)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(readLogFile(t, path)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (1 header + 1 data):\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if lines[1] != "slide - 4.000 - 8.000" {
		t.Errorf("data line = %q, want %q", lines[1], "slide - 4.000 - 8.000")
	}
}

func TestLogTransitionEnterIsNoop(t *testing.T) {
	l, path := newTestLogger(t)
	ev := model.FrameEvent{EventType: "draw", StartFrame: 60, EndFrame: 120}
	l.LogTransition("ENTER", 60, ev, 30)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(readLogFile(t, path)), "\n")
	if len(lines) != 1 {
		t.Errorf("got %d lines, want 1 (header only):\n%s", len(lines), strings.Join(lines, "\n"))
	}
}

func TestLogTransitionExitWithoutEnter(t *testing.T) {
	l, path := newTestLogger(t)
	ev := model.FrameEvent{EventType: "banner", StartFrame: 30, EndFrame: 90}
	l.LogTransition("EXIT", 90, ev, 30)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, want := readLogFile(t, path), "# zen-board eventlog v2: action - start_sec - end_sec\nbanner - 1.000 - 3.000\n"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

func TestLogTransitionUsesEventStartFrame(t *testing.T) {
	l, path := newTestLogger(t)
	ev := model.FrameEvent{EventType: "text", StartFrame: 0, EndFrame: 300}
	l.LogTransition("EXIT", 300, ev, 30)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, want := readLogFile(t, path), "# zen-board eventlog v2: action - start_sec - end_sec\ntext - 0.000 - 10.000\n"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

func TestLogTransitionClampsInvertedEvent(t *testing.T) {
	l, path := newTestLogger(t)
	ev := model.FrameEvent{EventType: "move", StartFrame: 300, EndFrame: 120}
	l.LogTransition("EXIT", 120, ev, 30)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, want := readLogFile(t, path), "# zen-board eventlog v2: action - start_sec - end_sec\nmove - 4.000 - 4.000\n"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

func TestLogTransitionClampsNegativeStartFrame(t *testing.T) {
	l, path := newTestLogger(t)
	ev := model.FrameEvent{EventType: "static", StartFrame: -10, EndFrame: 60}
	l.LogTransition("EXIT", 60, ev, 30)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, want := readLogFile(t, path), "# zen-board eventlog v2: action - start_sec - end_sec\nstatic - 0.000 - 2.000\n"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

func TestLogTransitionFPSZeroGuard(t *testing.T) {
	l, path := newTestLogger(t)
	ev := model.FrameEvent{EventType: "draw", StartFrame: 100, EndFrame: 200}
	l.LogTransition("EXIT", 200, ev, 0)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, want := readLogFile(t, path), "# zen-board eventlog v2: action - start_sec - end_sec\ndraw - 100.000 - 200.000\n"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

func TestLogTransitionMultipleEvents(t *testing.T) {
	l, path := newTestLogger(t)
	ev1 := model.FrameEvent{EventType: "draw", StartFrame: 0, EndFrame: 90}
	ev2 := model.FrameEvent{EventType: "slide", StartFrame: 90, EndFrame: 180}
	l.LogTransition("ENTER", 0, ev1, 30)
	l.LogTransition("ENTER", 90, ev2, 30)
	l.LogTransition("EXIT", 90, ev1, 30)
	l.LogTransition("EXIT", 180, ev2, 30)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	want := "# zen-board eventlog v2: action - start_sec - end_sec\n" +
		"draw - 0.000 - 3.000\n" +
		"slide - 3.000 - 6.000\n"
	if got := readLogFile(t, path); got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

func TestEventLoggerNilSafe(t *testing.T) {
	var l *EventLogger
	l.LogTransition("EXIT", 100, model.FrameEvent{StartFrame: 0, EventType: "draw"}, 30)
	if err := l.Close(); err != nil {
		t.Fatalf("nil Close: %v", err)
	}
}
