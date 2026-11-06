package script

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture deliberately mixes multi-byte runes before the offsets we locate,
// because byte-column reporting is the classic editor mismatch.
const unicodeScript = "Der König war [draw:könig] gross — sehr [draw:ß:TL]!\nsecond line [wait:1]\n"

func TestSourceLocateIsEditorVisible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "u.zen")
	if err := os.WriteFile(path, []byte(unicodeScript), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Lines) != 3 {
		t.Fatalf("got %d lines, want 3 (trailing newline is a line)", len(src.Lines))
	}
	if src.Lines[0].Number != 1 || src.Lines[1].Number != 2 {
		t.Errorf("line numbering: %+v", src.Lines[:2])
	}
	if strings.HasSuffix(src.Lines[0].Text, "\r") {
		t.Error("line text keeps the terminator")
	}

	for _, tc := range []struct {
		frag      string
		line, col int
	}{
		{"[draw:könig]", 1, 15},
		{"[draw:ß:TL]", 1, 41},
		{"[wait:1]", 2, 13},
	} {
		off := strings.Index(unicodeScript, tc.frag)
		if off < 0 {
			t.Fatalf("fixture lost %q", tc.frag)
		}
		line, col := src.Locate(off)
		if line != tc.line || col != tc.col {
			t.Errorf("Locate(%q): got %d:%d, want %d:%d (byte col would be %d)",
				tc.frag, line, col, tc.line, tc.col, off-src.Lines[tc.line-1].Offset+1)
		}
	}

	// Offsets carried on parsed actions must agree with Locate.
	lines, findings := ParseChecked(unicodeScript, path)
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %v", findings)
	}
	for _, l := range lines {
		for _, a := range l.Actions {
			line, col := src.Locate(a.SrcOffset)
			if line != a.SrcLine || col != a.SrcCol {
				t.Errorf("action %q: Source.Locate = %d:%d but parser stamped %d:%d", a.Tag, line, col, a.SrcLine, a.SrcCol)
			}
		}
	}
}

func TestSourceSnippetAndHash(t *testing.T) {
	src := NewSource("x.zen", []byte(unicodeScript))
	snip := src.Snippet(strings.Index(unicodeScript, "[draw:ß"))
	if !strings.Contains(snip, "draw:ß") {
		t.Errorf("snippet lost the tag: %q", snip)
	}
	h1 := src.Hash()
	if h1 == "" || len(h1) != 64 {
		t.Errorf("hash looks wrong: %q", h1)
	}
	other := NewSource("x.zen", []byte(unicodeScript+" "))
	if other.Hash() == h1 {
		t.Error("hash must change when bytes change")
	}
}

func TestSourceLongLineSnippetKeepsContext(t *testing.T) {
	long := strings.Repeat("word ", 200) + "[draw:robot]"
	src := NewSource("long.zen", []byte(long))
	s := src.Snippet(strings.Index(long, "[draw:robot]"))
	if len(s) > 110 || !strings.Contains(s, "draw:robot") {
		t.Errorf("snippet not bounded or lost context: %q (len %d)", s, len(s))
	}
}
