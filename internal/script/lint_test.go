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
	write("meme_lol.png")
	write("robot_happy.png")
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

// newGenreFixture adds the asset ids the genre examples use.
func newGenreFixture(t *testing.T) *lintFixture {
	t.Helper()
	f := newLintFixture(t)
	return f
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

// The genre tags are new, so every parameter they add is validated the day it exists.
func TestLintGenreParameters(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"unknown anchor", `[character:robot anchor=nowhere]`, diag.E0005},
		{"unknown layer", `[character:robot layer=mystery]`, diag.E0006},
		{"unknown exit", `[sticker:x dur=2 exit=bounce]`, diag.E0006},
		{"bad colour", `[caption:"HI" color=reddish]`, diag.E0006},
		{"unknown size unit", `[character:robot size=big]`, diag.E0004},
		{"unknown asset", `[character:ghost_mascot]`, diag.E0001},
		{"unknown key", `[caption:"HI" shadow=4]`, diag.E0006},
		// A parameter the compiler does not act on must not be in the grammar.
		{"unimplemented pop", `[sticker:city pop=true]`, diag.E0006},
		{"layer on a board tag", `[draw:robot layer=overlay]`, diag.E0006},
		{"camera-only reset as anchor", `[draw:robot anchor=reset]`, diag.E0005},
	}
	for _, tc := range cases {
		report := lintText(t, newGenreFixture(t), tc.text)
		if len(report.ByCode(tc.want)) == 0 {
			t.Errorf("%s: expected %s, got %s", tc.name, tc.want, report)
		}
	}
}

func TestLintAcceptsTheGenreForms(t *testing.T) {
	for _, text := range []string{
		`[character:robot_happy anchor=br size=45% id=bot]`,
		`[sticker:city anchor=tr w=280 h=160 dur=2 exit=pop]`,
		`[caption:"IT WEDGES" anchor=bc color=#FFFF00 outline=6 size=110 dur=2.5]`,
		`[clear:all]`,
	} {
		report := lintText(t, newGenreFixture(t), text)
		if report.Blocked() {
			t.Errorf("%s: blocked: %s", text, report)
		}
	}
}

// A named parameter is separated from the positional arguments by spaces, but that must
// never eat an equals sign that is part of the author's prose.
func TestLegacyFreeTextKeepsItsEqualsSign(t *testing.T) {
	for _, text := range []string{
		`[chapter:Act One]`,
		`[banner:Sum 1+1=2]`,
		`[caption:2+2=happy id=ch]`,
	} {
		report := lintText(t, newGenreFixture(t), text)
		if report.Blocked() {
			t.Errorf("%s: blocked: %s", text, report)
		}
	}

	// A prose argument containing = stays one argument; a declared key still separates.
	_, tag, ok := ParseTagSpec(`[banner:Final score 1+1=2 id=x]`)
	if !ok {
		t.Fatal("tag not resolved")
	}
	if _, hasID := tag.Key("id"); !hasID {
		t.Errorf("named tail lost: parts=%q keys=%v", tag.Parts, tag.Keys)
	}
	if tag.Parts[0] != "Final score 1+1=2" {
		t.Errorf("positional argument was split at the equals sign in the prose: %q", tag.Parts[0])
	}
}

// A parameter only belongs in the grammar where the compiler acts on it. Promising
// layer=, keep= or exit= on a tag that ignores them is the "documented no-op" trap this
// plan exists to close, so those keys are refused outside the genre tags.
func TestGenreOnlyParametersAreNotAdmittedEverywhere(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{`[draw:robot layer=overlay]`, diag.E0006},
		{`[text:"hi" keep=false]`, diag.E0006},
		{`[draw:robot exit=fade]`, diag.E0006},
		{`[character:robot layer=board keep=false exit=pop]`, ""},
	} {
		report := lintText(t, newGenreFixture(t), tc.text)
		if got := len(report.ByCode(tc.want)); (got > 0) != (tc.want != "") {
			t.Errorf("%s: %s present=%v want=%v: %s", tc.text, tc.want, got > 0, tc.want != "", report)
		}
	}
}

// "reset" is a camera instruction. Accepted as an anchor it resolved to the whole canvas,
// which is the silent fallback the preset table exists to remove.
func TestCameraOnlyNamesAreNotBoxes(t *testing.T) {
	report := lintText(t, newGenreFixture(t), `[draw:robot anchor=reset]`)
	if len(report.ByCode(diag.E0005)) == 0 {
		t.Errorf("anchor=reset accepted: %s", report)
	}
}
