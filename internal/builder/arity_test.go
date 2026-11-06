package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zen-board/internal/constant"
	"zen-board/internal/diag"
	"zen-board/internal/model"
	"zen-board/internal/script"
)

func compileFindings(t *testing.T, lines ...string) (*TimelineCompilation, *diag.Report) {
	t.Helper()
	conf := model.NewDefaultProject()
	conf.FPS = 30
	conf.AssetsDir = t.TempDir()
	conf.ScriptPath = "arity.zen"

	var pLines []model.ProcessedLine
	for i, l := range lines {
		parsed, _ := script.ParseChecked(l, conf.ScriptPath)
		if len(parsed) != 1 {
			t.Fatalf("want 1 line from %q, got %d", l, len(parsed))
		}
		pLines = append(pLines, model.ProcessedLine{
			StartTime: float64(i) * 2,
			Actions:   parsed[0].Actions,
		})
	}

	comp, err := CompileTimeline(conf, nil, pLines, 10.0, "")
	if err != nil {
		t.Fatalf("CompileTimeline: %v", err)
	}
	return comp, comp.Findings
}

// TestArityErrorsAreFindings: the two panic sites the plan calls out must produce
// an E0009 finding and drop the action instead of taking the process down.
func TestArityErrorsAreFindings(t *testing.T) {
	cases := []struct {
		line string
		tag  string
	}{
		{`one argument only [arrow:TL]`, "[arrow:from:to[:style][:duration]]"},
		{`[compare:onlyone]`, "[compare:left:right[:labelL][:labelR]]"},
		{`[compare:a]`, "[compare:left:right[:labelL][:labelR]]"},
	}
	for _, c := range cases {
		comp, rep := compileFindings(t, c.line, "next line of narration")
		if rep == nil {
			t.Fatalf("%q: no report", c.line)
		}
		fs := rep.ByCode(diag.E0009)
		if len(fs) != 1 {
			t.Errorf("%q: got %d E0009 findings, want 1 (%s)", c.line, len(fs), rep)
			continue
		}
		f := fs[0]
		if !strings.Contains(f.Message, c.tag) {
			t.Errorf("%q: message %q should name the canonical form %q", c.line, f.Message, c.tag)
		}
		if f.FixHint == "" {
			t.Errorf("%q: missing FixHint", c.line)
		}
		if f.Line == 0 || f.Col == 0 {
			t.Errorf("%q: finding has no location: %+v", c.line, f)
		}
		// A blocked report is what makes `check` exit non-zero.
		if !rep.Blocked() {
			t.Errorf("%q: report with an error must block", c.line)
		}
		// The malformed action produced no events, the rest of the script compiled.
		if got := len(comp.Timeline.Events); got != 0 {
			t.Errorf("%q: malformed action emitted %d events, want 0", c.line, got)
		}
	}
}

// TestValidArrowAndCompareStillCompile is the positive control: correct arity is
// unaffected and emits the animated + static event pair.
func TestValidArrowAndCompareStillCompile(t *testing.T) {
	conf := model.NewDefaultProject()
	conf.FPS = 30
	conf.AssetsDir = t.TempDir()
	for _, id := range []string{"city", "pyramids"} {
		if err := os.WriteFile(filepath.Join(conf.AssetsDir, id+".png"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var pLines []model.ProcessedLine
	for i, l := range []string{`[arrow:TL:BR:curved:2]`, `[compare:city:pyramids:"a":"b"]`} {
		parsed, _ := script.ParseChecked(l, "ok.zen")
		pLines = append(pLines, model.ProcessedLine{StartTime: float64(i) * 2, Actions: parsed[0].Actions})
	}
	comp, err := CompileTimeline(conf, nil, pLines, 10.0, "")
	if err != nil {
		t.Fatalf("CompileTimeline: %v", err)
	}
	rep := comp.Findings
	if rep.Blocked() {
		t.Fatalf("valid tags blocked: %s", rep)
	}
	var arrows, compares int
	for _, ev := range comp.Timeline.Events {
		switch ev.EventType {
		case constant.EVENT_ARROW, constant.EVENT_ARROW_STATIC:
			arrows++
		case constant.EVENT_COMPARE:
			compares++
		}
	}
	if arrows != 2 || compares != 2 {
		t.Fatalf("arrow=%d compare=%d, want 2 and 2", arrows, compares)
	}
}

// TestFindingsSurviveCompile asserts the report travels out of CompileTimeline
// so the CLI can gate on it.
func TestFindingsReportString(t *testing.T) {
	_, rep := compileFindings(t, `[arrow:TL]`)
	s := rep.String()
	want := "arity.zen:1:1: error[E0009]:"
	if !strings.Contains(s, want) {
		t.Errorf("report string %q does not start a finding with %q", s, want)
	}
}
