package script

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zen-board/internal/model"
)

// hasNestedBrackets reports whether a '[' opens while another bracket span is
// still unclosed - the shape that used to panic the parser.
func hasNestedBrackets(s string) bool {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[':
			if depth > 0 {
				return true
			}
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		}
	}
	return false
}

// TestPanicMatrix exercises the nested/unclosed tag shapes that used to panic in
// extractActions (slice bounds out of range once two patterns matched the same
// bytes). Every shape must parse without panicking; genuinely colliding spans
// must be reported.
func TestPanicMatrix(t *testing.T) {
	inners := []string{"[draw:x]", "[wait:1]", "[clear]", "[zoom:TR]", `[text:"hi":TL]`}
	outers := []string{
		`[text:"a IN b"]`, `[text:a IN b]`, `[draw:robot:TR IN]`, `[slide:city IN]`,
		`[banner:"t":"s" IN]`, `[chapter:IN Intro]`, `[erase:robot IN]`, `[overlay:x:0.5:BR:3 IN]`,
		`[arrow:TL:BR IN]`, `[compare:a:b IN]`, `[highlight:TL:rect:2 IN]`, `[watermark:txt:TL IN]`,
		`[counter:0:100:2:%d:center IN]`, `[style:blackboard IN]`, `[gen:prompt:TL IN]`,
		`[move:robot:BR IN]`, `[sfx:ding IN]`, `[subtitle:off IN]`, `[transition:fade:1 IN]`,
		`[draw:x] trailing prose [`, `unterminated [text:"open`, `[]`, `[:]`, `[`,
	}

	var shapes []string
	for _, o := range outers {
		for _, in := range inners {
			shapes = append(shapes, strings.ReplaceAll(o, "IN", in))
			shapes = append(shapes, in+" "+strings.ReplaceAll(o, "IN", "X"))
		}
	}
	shapes = append(shapes,
		`narration [text:"a [draw:x] b"] tail`,
		`narration [text:a [draw:x] b] tail`,
		`[slide:[draw:x]]`, `[banner:"a [draw:x] b"]`, `[chapter:[wait:1] Intro]`,
		`[erase:[clear]]`, `[draw:robot [zoom:TR]]`,
	)

	for i, shape := range shapes {
		shape := shape
		t.Run(fmt.Sprintf("shape%03d", i), func(t *testing.T) {
			lines, findings := ParseChecked(shape, "matrix.zen")
			if hasNestedBrackets(shape) && len(findings) == 0 {
				t.Errorf("shape %q: nested tag consumed with no finding (clean=%q)", shape, dumpLines(lines))
			}
		})
	}
}

func TestOverlapProducesE0003(t *testing.T) {
	cases := []string{
		`narration [text:"a [draw:x] b"] tail`,
		`narration [text:a [draw:x] b] tail`,
		`[slide:[draw:x]]`,
		`[banner:"a [draw:x] b"]`,
		`[chapter:[wait:1] Intro]`,
		`[erase:[clear]]`,
		`[draw:robot [zoom:TR]]`,
	}
	for _, c := range cases {
		_, findings := ParseChecked(c, "overlap.zen")
		if len(findings) == 0 {
			t.Errorf("%q: want an overlap finding, got none", c)
			continue
		}
		if f := findings[0]; f.Code != "E0003" {
			t.Errorf("%q: got code %s, want E0003", c, f.Code)
		} else if f.Line == 0 || f.Col == 0 {
			t.Errorf("%q: finding has no location: %+v", c, f)
		}
	}
}

// TestNoFindingsOnShippedExamples guards against the detector firing on the
// well-formed scripts the repo ships.
func TestNoFindingsOnShippedExamples(t *testing.T) {
	for _, ex := range exampleScripts(t) {
		_, findings := ParseChecked(ex.raw, ex.name)
		for _, f := range findings {
			t.Errorf("%s: unexpected finding while parsing a shipped example: %s", ex.name, f)
		}
	}
}

type exampleScript struct {
	name string
	raw  string
}

func exampleScripts(t *testing.T) []exampleScript {
	t.Helper()
	dir := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read examples: %v", err)
	}
	var out []exampleScript
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zen") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		out = append(out, exampleScript{name: e.Name(), raw: string(raw)})
	}
	if len(out) == 0 {
		t.Fatal("no .zen examples found")
	}
	return out
}

// dumpLines renders parse output without source metadata, so the golden files
// track behaviour rather than the presence of offsets.
func dumpLines(lines []model.ScriptLine) string {
	type action struct {
		Tag              string
		WordIndex        int
		X, Y, W, H       int
		TriggerAfterWord bool
		AssetVariant     map[string]string
	}
	type line struct {
		Text    string
		Actions []action
	}
	out := make([]line, 0, len(lines))
	for _, l := range lines {
		ol := line{Text: l.Text}
		for _, a := range l.Actions {
			ol.Actions = append(ol.Actions, action{
				Tag: a.Tag, WordIndex: a.WordIndex, X: a.X, Y: a.Y, W: a.W, H: a.H,
				TriggerAfterWord: a.TriggerAfterWord, AssetVariant: a.AssetVariant,
			})
		}
		out = append(out, ol)
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(data)
}

// TestParseGolden pins the parser's behaviour on every shipped example: the
// byte-identical contract required across the parser/DSL refactor. Regenerate
// with UPDATE_GOLDEN=1 only for an intentional DSL change.
func TestParseGolden(t *testing.T) {
	for _, ex := range exampleScripts(t) {
		t.Run(ex.name, func(t *testing.T) {
			got := dumpLines(Parse(ex.raw)) + "\n"
			golden := filepath.Join("testdata", strings.TrimSuffix(ex.name, ".zen")+".parse.golden")
			want, err := os.ReadFile(golden)
			if err != nil {
				if os.Getenv("UPDATE_GOLDEN") == "" {
					t.Fatalf("no golden for %s (%v); run UPDATE_GOLDEN=1 go test ./internal/script", ex.name, err)
				}
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("wrote %s", golden)
				return
			}
			if string(want) != got {
				t.Errorf("parse output changed for %s\n--- want ---\n%s\n--- got ---\n%s", ex.name, want, got)
			}
		})
	}
}

// TestSrcPositionsPropagation asserts the parser stamps every action with the
// file, line, column and byte offset of its tag, and that the stamps survive
// SplitInlineWaits.
func TestSrcPositionsPropagation(t *testing.T) {
	const src = `The king was [draw:king_death@100,200,400,300] killed.
[wait:2.0]
  His son [draw:prince] took the throne. [text:"Long live":TL]`

	lines, findings := ParseChecked(src, "king.zen")
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %v", findings)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}

	var actions []model.DrawAction
	for _, l := range lines {
		actions = append(actions, l.Actions...)
	}
	if len(actions) != 4 {
		t.Fatalf("got %d actions, want 4: %+v", len(actions), actions)
	}

	// Expected locations derived from the source itself, not hand-counted.
	type exp struct {
		raw  string
		line int
	}
	exps := []exp{
		{"[draw:king_death@100,200,400,300]", 1},
		{"[wait:2.0]", 2},
		{"[draw:prince]", 3},
		{`[text:"Long live":TL]`, 3},
	}
	for i, e := range exps {
		a := actions[i]
		off := strings.Index(src, e.raw)
		if off < 0 {
			t.Fatalf("fixture lost %q", e.raw)
		}
		lineStart := 0
		for k := 0; k < e.line-1; k++ {
			lineStart = strings.Index(src[lineStart:], "\n") + lineStart + 1
		}
		if a.SrcRaw != e.raw {
			t.Errorf("action %d: SrcRaw = %q, want %q", i, a.SrcRaw, e.raw)
		}
		if a.SrcOffset != off || a.SrcLength != len(e.raw) {
			t.Errorf("action %d: offset/length = %d/%d, want %d/%d", i, a.SrcOffset, a.SrcLength, off, len(e.raw))
		}
		if got := src[a.SrcOffset : a.SrcOffset+a.SrcLength]; got != e.raw {
			t.Errorf("action %d: Locate mismatch: %q", i, got)
		}
		if a.SrcLine != e.line {
			t.Errorf("action %d: line = %d, want %d", i, a.SrcLine, e.line)
		}
		if a.SrcCol != off-lineStart+1 {
			t.Errorf("action %d: col = %d, want %d", i, a.SrcCol, off-lineStart+1)
		}
		if a.SrcPath != "king.zen" {
			t.Errorf("action %d: SrcPath = %q", i, a.SrcPath)
		}
	}

	// The preprocessor moves actions between lines; positions must survive.
	for _, l := range SplitInlineWaits(lines) {
		for _, a := range l.Actions {
			if a.SrcLine == 0 {
				t.Errorf("action %q lost its source line through SplitInlineWaits", a.Tag)
			}
		}
	}
}
