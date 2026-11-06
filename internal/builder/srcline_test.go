package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zen-board/internal/model"
	"zen-board/internal/script"
)

// compileExample compiles a shipped .zen file with synthetic word timings (two
// words per line-second) and the real assets directory, so asset validation is
// satisfied by the repo's own catalog.
func compileExample(t *testing.T, name string) (*TimelineCompilation, []model.ScriptLine, *script.Source) {
	t.Helper()
	path := filepath.Join("..", "..", "examples", name)
	src, err := script.Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	lines, findings := script.ParseChecked(string(src.Raw), src.Path)
	for _, f := range findings {
		t.Errorf("%s: parse finding: %s", name, f)
	}

	conf := model.NewDefaultProject()
	conf.FPS = 30
	conf.ScriptPath = name
	conf.AssetsDir = filepath.Join("..", "..", "assets")

	var timings []model.WordTiming
	var pLines []model.ProcessedLine
	t0 := 0.0
	for _, l := range lines {
		words := strings.Fields(l.Text)
		pLines = append(pLines, model.ProcessedLine{
			StartTime:  t0,
			Duration:   2.0,
			WordOffset: len(timings),
			Actions:    l.Actions,
		})
		for i := range words {
			timings = append(timings, model.WordTiming{
				Word:  words[i],
				Start: t0 + float64(i)*0.25,
				End:   t0 + float64(i+1)*0.25,
			})
		}
		t0 += 2.0
	}

	comp, err := CompileTimeline(conf, timings, pLines, t0, "")
	if err != nil {
		t.Fatalf("CompileTimeline(%s): %v", name, err)
	}
	for _, f := range comp.Findings.Findings {
		t.Errorf("%s: compile finding: %s", name, f)
	}
	return comp, lines, src
}

func exampleNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("..", "..", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".zen") {
			out = append(out, e.Name())
		}
	}
	if len(out) == 0 {
		t.Fatal("no examples")
	}
	return out
}

// TestSrcLinePropagation: every compiled event points back at the tag that
// produced it, for every shipped example. This is what makes a rendering bug
// attributable to a line of script instead of to "the video looks wrong".
func TestSrcLinePropagation(t *testing.T) {
	for _, name := range exampleNames(t) {
		t.Run(name, func(t *testing.T) {
			comp, _, src := compileExample(t, name)
			if len(comp.Timeline.Events) == 0 {
				t.Fatal("no events compiled")
			}
			for i, ev := range comp.Timeline.Events {
				if ev.SrcLine <= 0 {
					t.Errorf("event %d (%s/%s) has no source line", i, ev.EventType, ev.TargetImage)
					continue
				}
				if ev.SrcLine > len(src.Lines) {
					t.Errorf("event %d claims line %d, script has %d", i, ev.SrcLine, len(src.Lines))
					continue
				}
				// The tag text must actually be on the reported line.
				line := src.Lines[ev.SrcLine-1].Text
				if !strings.Contains(line, "[") {
					t.Errorf("event %d (%s) claims line %d, which holds no tag: %q", i, ev.EventType, ev.SrcLine, line)
				}
			}
		})
	}
}

// TestEventIdentityIsStable: the derived identity must be stable across runs and
// shared by the reveal event and the persistence event it produced.
func TestEventIdentityIsStable(t *testing.T) {
	for _, name := range exampleNames(t) {
		t.Run(name, func(t *testing.T) {
			a, _, _ := compileExample(t, name)
			b, _, _ := compileExample(t, name)
			if len(a.Timeline.Events) != len(b.Timeline.Events) {
				t.Fatalf("event count differs between compiles: %d vs %d", len(a.Timeline.Events), len(b.Timeline.Events))
			}
			for i := range a.Timeline.Events {
				if a.Timeline.Events[i].ID != b.Timeline.Events[i].ID {
					t.Fatalf("event %d identity unstable: %q vs %q", i, a.Timeline.Events[i].ID, b.Timeline.Events[i].ID)
				}
				if a.Timeline.Events[i].ID == "" {
					t.Errorf("event %d (%s) has no identity", i, a.Timeline.Events[i].EventType)
				}
			}
			// One identity per authored element: the reveal and the persistence event
			// it spawns are one logical thing, so they must share an ID - and nothing
			// else may share it. Two different tags can never collide on an ID.
			type tagPos struct{ line, col int }
			owners := map[string]tagPos{}
			for _, ev := range a.Timeline.Events {
				p := tagPos{ev.SrcLine, ev.SrcCol}
				if first, seen := owners[ev.ID]; seen && first != p {
					t.Errorf("id %q claimed by two different tags: %v and %v", ev.ID, first, p)
				}
				owners[ev.ID] = p
			}
		})
	}
}

// TestRevealDurationRecorded proves the authored window is carried on the event
// instead of only living in the frame arithmetic.
func TestRevealDurationRecorded(t *testing.T) {
	comp, _, _ := compileExample(t, "demo.zen")
	found := false
	for _, ev := range comp.Timeline.Events {
		if ev.RevealDuration > 0 {
			found = true
		}
		if ev.RevealDuration != 0 && ev.RevealDuration != 2.0 && ev.RevealDuration != 3.0 {
			t.Errorf("unexpected reveal duration %v", ev.RevealDuration)
		}
	}
	if !found {
		t.Error("no event carried a reveal duration")
	}
}
