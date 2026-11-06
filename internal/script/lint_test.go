package script

import (
	"os"
	"path/filepath"
	"testing"

	"zen-board/internal/diag"
)

// lintFixture is a catalog on disk, so the asset rules are exercised against real
// files rather than against a mock of the interface.
type lintFixture struct {
	dir string
}

func newLintFixture(t *testing.T) *lintFixture {
	t.Helper()
	dir := t.TempDir()
	write := func(rel string) {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("robot.png")
	write("city.png")
	write("shield.svg")
	write("cursors/hand/config.json")
	return &lintFixture{dir: dir}
}

func (f *lintFixture) lookup() AssetLookup { return fixtureLookup{f.dir} }

type fixtureLookup struct{ dir string }

func (l fixtureLookup) Lookup(id string) (bool, bool, bool) {
	path := filepath.Join(l.dir, id)
	if st, err := os.Stat(path + ".png"); err == nil && !st.IsDir() {
		return true, false, false
	}
	if st, err := os.Stat(path + ".svg"); err == nil && !st.IsDir() {
		return true, true, true
	}
	return false, false, false
}

func (l fixtureLookup) HasCursorPreset(name string) bool {
	st, err := os.Stat(filepath.Join(l.dir, "cursors", name, "config.json"))
	return err == nil && !st.IsDir()
}

func (l fixtureLookup) SupportsVariants(id string) bool {
	_, err := os.Stat(filepath.Join(l.dir, id+".svg"))
	return err == nil
}

func lintText(t *testing.T, f *lintFixture, text string) *diag.Report {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case.zen")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return Lint(src, f.lookup(), LintOptions{})
}

func hasCode(rep *diag.Report, code string) bool { return len(rep.ByCode(code)) > 0 }

// TestLintLiveInputs are the five inputs the plan requires the gate to handle,
// each with today's broken behaviour named.
func TestLintLiveInputs(t *testing.T) {
	f := newLintFixture(t)
	cases := []struct {
		text     string
		code     string
		why      string
		blocked  bool
		notCode  string
		contains string
	}{
		{`[drw:robot] typo tag`, diag.E0002, "an unknown prefix is read aloud by TTS", true, "", "did you mean [draw"},
		{`[draw:robot:2.5] decimal duration`, diag.E0007, "0 actions produced, text read aloud", true, "", "decimal duration"},
		{`value is array[0] here`, diag.W0001, "literal bracket read aloud", false, "", ""},
		{`[text:"a [draw:x] b"]`, diag.E0003, "the inner tag is swallowed", true, "", "nested tag"},
		{`[overlay:x:1.0:BR:3.0]`, diag.E0004, "extra argument ignored", true, "", "at most 3 arguments"},
	}
	for _, tc := range cases {
		rep := lintText(t, f, tc.text)
		if !hasCode(rep, tc.code) {
			t.Errorf("%q: missing %s (%s); got %s", tc.text, tc.code, tc.why, rep)
			continue
		}
		if rep.Blocked() != tc.blocked {
			t.Errorf("%q: blocked = %v, want %v (%s)", tc.text, rep.Blocked(), tc.blocked, tc.why)
		}
		if tc.contains != "" {
			f := rep.ByCode(tc.code)[0]
			haystack := f.Message + " " + f.FixHint
			if !contains(haystack, tc.contains) {
				t.Errorf("%q: %s text %q lacks %q", tc.text, tc.code, haystack, tc.contains)
			}
		}
	}
}

func contains(hay, needle string) bool {
	return len(needle) == 0 || (len(hay) >= len(needle) && indexOf(hay, needle) >= 0)
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// TestLintErrorCodes covers the rest of the code table.
func TestLintErrorCodes(t *testing.T) {
	f := newLintFixture(t)
	cases := []struct {
		name string
		text string
		code string
	}{
		{"E0001 unknown asset", `[draw:nosuchthing]`, diag.E0001},
		{"E0004 malformed coords", `[draw:city@1,2,3]`, diag.E0004},
		{"E0005 unknown preset", `[draw:city:MIDDLE]`, diag.E0005},
		{"E0006 unknown cursor", `[draw:city:cursor=wand_of_dreams]`, diag.E0006},
		{"E0006 unknown style", `[style:chalkboard]`, diag.E0006},
		{"E0008 text without quotes", `[text:no quotes here]`, diag.E0008},
		{"E0009 arrow arity", `[arrow:TL] prose`, diag.E0009},
		{"E0009 compare arity", `[compare:onlyone]`, diag.E0009},
		{"E0010 coords on a tag without a coord group", `[overlay:city:0.5:BR@10,20]`, diag.E0010},
		{"E0011 svg with coords but no size", `[draw:shield@10,20]`, diag.E0011},
		{"E0012 unknown zoom name", `[zoom:kitchen]`, diag.E0012},
		{"E0014 documented no-op", `[sfx:ding]`, diag.E0014},
	}
	for _, tc := range cases {
		rep := lintText(t, f, tc.text)
		if !hasCode(rep, tc.code) {
			t.Errorf("%s (%q): missing %s, got %s", tc.name, tc.text, tc.code, rep)
			continue
		}
		if !rep.Blocked() {
			t.Errorf("%s: an error-tier finding must block the run", tc.name)
		}
		loc := rep.ByCode(tc.code)[0]
		if loc.Line == 0 || loc.Col == 0 || loc.File == "" {
			t.Errorf("%s: finding needs file:line:col, got %+v", tc.name, loc)
		}
		if loc.FixHint == "" {
			t.Errorf("%s: finding needs a Fix hint, got %+v", tc.name, loc)
		}
	}
}

// TestWarningsNeverBlocks: narration may legitimately contain a code token.
func TestWarningsNeverBlocks(t *testing.T) {
	f := newLintFixture(t)
	rep := lintText(t, f, `Index array[0] is the first element.`)
	if !hasCode(rep, diag.W0001) {
		t.Fatalf("expected W0001, got %s", rep)
	}
	if rep.Blocked() {
		t.Errorf("a warning must never block: %s", rep)
	}
}

// TestElementRefResolution covers @id addressing: forward and unknown refs are
// errors, a backward ref is fine, and a duplicate id is reported once.
func TestElementRefResolution(t *testing.T) {
	f := newLintFixture(t)
	rep := lintText(t, f, "[arrow:@bot:@curve:curved] prose")
	if !hasCode(rep, diag.E0013) {
		t.Errorf("unresolved @refs must be E0013, got %s", rep)
	}

	rep = lintText(t, f, `[draw:city:id=bot] first [draw:robot:id=bot] second`)
	if len(rep.ByCode(diag.E0013)) != 1 {
		t.Errorf("duplicate id must be reported once, got %s", rep)
	}
}

// TestLintOverRawBytesNotCleanedText: a rule that ran over ScriptLine.Text would see
// the tags already stripped and could never fire.
func TestLintOverRawBytesNotCleanedText(t *testing.T) {
	f := newLintFixture(t)
	rep := lintText(t, f, `Prose [draw:nosuchasset] more prose`)
	if !hasCode(rep, diag.E0001) {
		t.Fatalf("asset rule did not fire: %s", rep)
	}
	// the same span in cleaned text carries no tag at all
	lines, _ := ParseChecked(`Prose [draw:nosuchasset] more prose`, "x.zen")
	if contains(lines[0].Text, "draw:") {
		t.Error("fixture assumption broken: cleaned text still carries the tag")
	}
}
