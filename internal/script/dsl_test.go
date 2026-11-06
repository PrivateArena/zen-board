package script

import (
	"regexp"
	"strings"
	"testing"
)

// legacyPatterns are the 21 regexps the parser carried inline before the table
// existed (internal/script/parser.go at the commit that added this test). They
// are pinned here verbatim: the table may add specs, but it must not silently
// change an existing shape, because a changed shape changes which text reaches
// TTS.
var legacyPatterns = map[string]*regexp.Regexp{
	"draw":       regexp.MustCompile(`\[draw:([^\]@:]+)((?:[:\w-]+(?:=[^\]@:]+)?)*)(?:@([\d,]+))?\]`),
	"text":       regexp.MustCompile(`\[text:([^\]@]+)(?:@([\d,]+))?\]`),
	"erase":      regexp.MustCompile(`\[erase:([^\]@]+)(?:@([\d,]+))?\]`),
	"move":       regexp.MustCompile(`\[move:([^\]@]+)(?:@([\d,]+))?\]`),
	"gen":        regexp.MustCompile(`\[gen:([^\]@]+)(?:@([\d,]+))?\]`),
	"wait":       regexp.MustCompile(`\[wait:([\d.]+)\]`),
	"clear":      regexp.MustCompile(`\[clear\]`),
	"zoom":       regexp.MustCompile(`\[zoom:([^\]]+)\]`),
	"style":      regexp.MustCompile(`\[style:([^\]]+)\]`),
	"chapter":    regexp.MustCompile(`\[chapter:([^\]]+)\]`),
	"sfx":        regexp.MustCompile(`\[sfx:([^\]]+)\]`),
	"subtitle":   regexp.MustCompile(`\[subtitle:([^\]]+)\]`),
	"slide":      regexp.MustCompile(`\[slide:([^\]]+)\]`),
	"banner":     regexp.MustCompile(`\[banner:([^\]]+)\]`),
	"arrow":      regexp.MustCompile(`\[arrow:([^\]]+)\]`),
	"highlight":  regexp.MustCompile(`\[highlight:([^\]]+)\]`),
	"compare":    regexp.MustCompile(`\[compare:([^\]]+)\]`),
	"transition": regexp.MustCompile(`\[transition:([^\]]+)\]`),
	"overlay":    regexp.MustCompile(`\[overlay:([^\]]+)\]`),
	"counter":    regexp.MustCompile(`\[counter:([^\]]+)\]`),
	"watermark":  regexp.MustCompile(`\[watermark:([^\]]+)\]`),
}

// TestPatternTableMatchesLegacy proves the DSL table reproduces the historical
// patterns exactly, wildcard-erase shape excepted (it is a second pattern on the
// erase spec, listed deliberately).
func TestPatternTableMatchesLegacy(t *testing.T) {
	for tag, legacy := range legacyPatterns {
		spec, ok := ByTag(tag)
		if !ok {
			t.Errorf("DSL table has no spec for legacy tag %q", tag)
			continue
		}
		found := false
		for _, re := range spec.Patterns {
			if re.String() == legacy.String() {
				found = true
			}
		}
		if !found {
			t.Errorf("spec %q changed shape:\n table:  %v\n legacy: %v", tag, spec.Patterns, legacy)
		}
	}
	if got := len(All()); got < len(legacyPatterns) {
		t.Errorf("table shrank to %d specs, legacy had %d", got, len(legacyPatterns))
	}
}

// syntheticCorpus is the input class the shipped examples do not cover: nested
// brackets and ":num.num" durations. Alone, the examples would have caught
// neither flagship defect, so the table is exercised against these too.
var syntheticCorpus = []struct {
	line         string
	wantActions  int
	wantCleanHas string
}{
	{`[draw:robot:2.5]`, 0, "[draw:robot:2.5]"}, // legacy regex cannot match .5
	{`[text:"a [draw:x] b"]`, 1, `b"]`},         // nested, outer swallows inner
	{`prose [zoom:kitchen] more`, 1, "prose more"},
	{`[drw:robot]`, 0, "[drw:robot]"}, // typo, unconsumed
	{`[overlay:x:1.0:BR:3.0]`, 1, ""},
	{`array[0] prose`, 0, "array[0] prose"},
	// The unified named-parameter tail is Wave 3 work; until then a space inside
	// the tag body is not part of the grammar, and this must be a finding, not a
	// silently spoken tag.
	{`[draw:city:TR id=bot dur=6 exit=fade]`, 0, "id=bot"},
	{`[clear]`, 1, ""},
	{`[erase:*]`, 1, ""},
	{`[wait:1]`, 1, ""},
}

func TestSyntheticCorpusParity(t *testing.T) {
	for _, tc := range syntheticCorpus {
		lines, findings := ParseChecked(tc.line, "corpus.zen")
		actions := 0
		clean := ""
		if len(lines) == 1 {
			actions = len(lines[0].Actions)
			clean = lines[0].Text
		}
		if actions != tc.wantActions {
			t.Errorf("%q: %d actions, want %d", tc.line, actions, tc.wantActions)
		}
		if tc.wantCleanHas != "" && !strings.Contains(clean, tc.wantCleanHas) && clean != tc.wantCleanHas {
			t.Errorf("%q: clean text %q does not contain %q", tc.line, clean, tc.wantCleanHas)
		}
		// Anything left unconsumed must be reported, never silently spoken.
		if actions != tc.wantActions && len(findings) == 0 {
			t.Errorf("%q: mismatch with no finding", tc.line)
		}
	}
}

// TestEverySpecMatchesACanonicalExample: the table must be self-consistent - each
// spec's documented spellings really are matched by that spec's patterns. This is
// the check that catches a pattern edited out from under its own documentation.
func TestEverySpecMatchesACanonicalExample(t *testing.T) {
	for _, spec := range All() {
		examples := spec.TagExamples()
		if len(examples) == 0 {
			t.Errorf("%s: no canonical example documented", spec.Tag)
			continue
		}
		for _, raw := range examples {
			matched := false
			for _, re := range spec.Patterns {
				if re.MatchString(raw) {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("%s: documented spelling %s matches none of its patterns", spec.Tag, raw)
			}
			// and it must survive the parser
			lines, _ := ParseChecked("narration "+raw, "ex.zen")
			if len(lines) != 1 {
				t.Errorf("%s: %q produced %d lines", spec.Tag, raw, len(lines))
			}
		}
	}
}

func TestSplitPartsQuoteAware(t *testing.T) {
	cases := map[string][]string{
		`"Act I: Finale":BL:sans:48:bold`: {`"Act I: Finale"`, "BL", "sans", "48", "bold"},
		`x:0.5:BR:3.0`:                    {"x", "0.5", "BR", "3.0"},
		`"":empty`:                        {`""`, "empty"},
		`single`:                          {"single"},
		`"unbalanced:stillone:part`:       {`"unbalanced:stillone:part`},
		`robot:cursor=magic_wand:TR`:      {"robot", "cursor=magic_wand", "TR"},
	}
	for in, want := range cases {
		got := SplitParts(in)
		if len(got) != len(want) {
			t.Errorf("SplitParts(%q) = %q, want %q", in, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("SplitParts(%q)[%d] = %q, want %q", in, i, got[i], want[i])
			}
		}
	}
}
