package builder

import (
	"testing"

	"zen-board/internal/model"
	"zen-board/internal/script"
)

// routeTag parses one canonical tag and reports which handler dispatch chose.
func routeTag(t *testing.T, raw string) (route string, actionTag string) {
	t.Helper()
	lines, findings := script.ParseChecked("probe "+raw, "probe.zen")
	for _, f := range findings {
		t.Fatalf("%s: unexpected parse finding: %s", raw, f)
	}
	if len(lines) != 1 || len(lines[0].Actions) != 1 {
		t.Fatalf("%s: parser produced %d actions, want 1", raw, len(lines[0].Actions))
	}
	act := lines[0].Actions[0]

	conf := model.NewDefaultProject()
	conf.FPS = 30
	conf.AssetsDir = t.TempDir()
	c := newTimelineCompiler(conf, nil, nil, 10.0, "")
	ctx := &actionContext{
		action:         &act,
		triggerTime:    0,
		startFrame:     0,
		endFrame:       60,
		revealDuration: 2,
	}
	if err := c.dispatch(ctx); err != nil {
		t.Fatalf("%s: dispatch error: %v", raw, err)
	}
	return ctx.route, ctx.actionTag
}

// TestDSLCoverage is the grammar contract: every tag the table declares is routed
// to a handler, every route the compiler can take is declared in the table, and
// every canonical example spellings parses. Routing is derived by dispatching, not
// by reading the source, so a branch that exists but is unreachable - or a tag
// that parses but falls through to the default - fails here.
func TestDSLCoverage(t *testing.T) {
	specs := script.All()
	declared := map[string]bool{}
	for _, spec := range specs {
		declared[spec.Tag] = true
	}

	routed := map[string]bool{}
	for _, spec := range specs {
		examples := spec.TagExamples()
		if len(examples) == 0 {
			t.Errorf("%s: spec declares no canonical example", spec.Tag)
			continue
		}
		for _, raw := range examples {
			route, _ := routeTag(t, raw)
			if route == "" {
				t.Errorf("%s: example %s routed nowhere", spec.Tag, raw)
				continue
			}
			if spec.Tag == "draw" {
				// The bare-asset form is the default branch by definition.
				if route != "draw" {
					t.Errorf("draw: example %s routed to %q, want the default branch", raw, route)
				}
			} else if route != spec.Tag {
				t.Errorf("%s: example %s routed to %q", spec.Tag, raw, route)
			}
			routed[route] = true
		}
	}

	for name := range routed {
		if !declared[name] {
			t.Errorf("compiler routes to %q but the DSL table does not declare it", name)
		}
	}
	for tag := range declared {
		if !routed[tag] {
			t.Errorf("DSL table declares %q but nothing routed to it", tag)
		}
	}
}

// TestEraseWildcardSharesTheEraseSpec: [erase:*] and [erase:asset] are one verb
// with two shapes, so the single-route expectation has to be stated, not inferred.
func TestEraseWildcardSharesTheEraseSpec(t *testing.T) {
	spec, ok := script.ByTag("erase")
	if !ok {
		t.Fatal("no erase spec")
	}
	if len(spec.Patterns) != 2 {
		t.Fatalf("erase spec has %d patterns, want 2 ([erase:*] and [erase:asset])", len(spec.Patterns))
	}
	for _, raw := range []string{"[erase:*]", "[erase:robot]"} {
		if route, _ := routeTag(t, raw); route != "erase" {
			t.Errorf("%s routed to %q, want erase", raw, route)
		}
	}
	if route, _ := routeTag(t, "[erase:*]"); route != "erase" {
		t.Fatal("wildcard must route to the erase family")
	}
	// and it really is the whole-board variant
	comp := compileWildcardErase(t)
	var kinds int
	for _, ev := range comp {
		if ev == "erase_all" {
			kinds++
		}
	}
	if kinds != 1 {
		t.Errorf("erase:* produced %d erase_all events, want 1", kinds)
	}
}

func compileWildcardErase(t *testing.T) []string {
	t.Helper()
	conf := model.NewDefaultProject()
	conf.FPS = 30
	conf.AssetsDir = t.TempDir()
	lines, _ := script.ParseChecked("[draw:robot] prose [erase:*]", "e.zen")
	var timings []model.WordTiming
	for i := 0; i < 3; i++ {
		timings = append(timings, model.WordTiming{Word: "w", Start: float64(i), End: float64(i) + 0.5})
	}
	comp, err := CompileTimeline(conf, timings, []model.ProcessedLine{{StartTime: 0, Actions: lines[0].Actions}}, 4, "")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, ev := range comp.Timeline.Events {
		out = append(out, ev.EventType)
	}
	return out
}

// TestNoopTagsAreDeclaredNotSilent: [sfx:] compiles to nothing on purpose, and
// that must be a property of the table rather than an absent branch.
func TestNoopTagsAreDeclaredNotSilent(t *testing.T) {
	spec, ok := script.ByTag("sfx")
	if !ok {
		t.Fatal("no sfx spec")
	}
	if !spec.IsNoop() {
		t.Error("sfx must be declared status=noop")
	}
	if len(spec.EmitsEvents) != 0 {
		t.Errorf("sfx declares %d emitted events, want none", len(spec.EmitsEvents))
	}
	route, _ := routeTag(t, "[sfx:ding]")
	if route != "sfx" {
		t.Errorf("sfx routed to %q, want sfx", route)
	}
}
