package script

import (
	"fmt"
	"strings"

	"zen-board/internal/diag"
)

// residualSpanFindings inspects the bytes a tag pattern did NOT consume and
// reports bracket spans that survived into the narration text. This is the
// "silently read aloud by TTS" family of defects: an outer tag that swallowed an
// inner one (`[text:"a [draw:x] b"]`), a typo'd prefix, an unclosed span, or a
// literal code token (`array[0]`).
//
// consumed[i] is true when line[i] was part of an accepted tag span.
func residualSpanFindings(line string, consumed []bool, lineNo int, file string, pad int) []diag.Finding {
	var out []diag.Finding
	for i := 0; i < len(line); i++ {
		if consumed[i] || line[i] != '[' {
			continue
		}
		end := -1
		for j := i + 1; j < len(line); j++ {
			if line[j] == ']' && !consumed[j] {
				end = j
				break
			}
		}
		markConsumed(consumed, i, end)

		open := snippetAt(line, i, end+1)
		body := line[i+1:]
		if end >= 0 {
			body = line[i+1 : end]
		}
		col := runeCol(line, i) + 1 + pad
		add := func(code, msg, hint, legacy string) {
			out = append(out, diag.Finding{
				Code:      code,
				Severity:  diag.SeverityOf(code),
				Stage:     "parse",
				File:      file,
				Line:      lineNo,
				Col:       col,
				Snippet:   open,
				Message:   msg,
				FixHint:   hint,
				LegacyMsg: legacy,
			})
		}

		if end < 0 {
			add(diag.E0003,
				fmt.Sprintf("unclosed tag span %q reaches the end of the line", open),
				"close the tag with ']' (or drop the bracket if you meant it literally)",
				fmt.Sprintf("Warning: unclosed tag %q at line %d col %d", body, lineNo, col))
			continue
		}
		prefix, _, _ := strings.Cut(body, ":")
		if isKnownTagPrefix(prefix) && hasDecimalTailArg(body) {
			add(diag.E0007,
				fmt.Sprintf("tag %q was not consumed: the legacy argument form cannot carry a decimal duration", open),
				"use an integer duration ([draw:robot:3]) or name it ([draw:robot:dur=2.5] once supported)",
				fmt.Sprintf("Warning: unparseable duration in tag %q at line %d col %d", body, lineNo, col))
			continue
		}
		if isKnownTagPrefix(prefix) || looksLikeTag(body) {
			fix := "check the spelling and argument count against the DSL table"
			if suggest := SuggestTag(prefix); suggest != "" && suggest != prefix {
				fix = fmt.Sprintf("did you mean [%s:...]? (also check the argument count)", suggest)
			}
			add(diag.E0002,
				fmt.Sprintf("tag %q was not consumed by any pattern", open),
				fix,
				fmt.Sprintf("Warning: unrecognised tag %q at line %d col %d", body, lineNo, col))
			continue
		}
		// Plausible non-tag token (array[0], [1], [TODO]) - a warning, never a block.
		add(diag.W0001,
			fmt.Sprintf("narration contains a literal bracket span %q which TTS will read aloud", open),
			"rephrase it in words if it is prose, or use a real tag",
			fmt.Sprintf("Warning: literal bracket %q at line %d col %d", body, lineNo, col))
	}

	// Orphan closing brackets: an outer tag that swallowed an inner one leaves a
	// trailing ']' behind once the inner span is what actually matched.
	for i := 0; i < len(line); i++ {
		if consumed[i] || line[i] != ']' {
			continue
		}
		consumed[i] = true
		out = append(out, diag.Finding{
			Code:      diag.E0003,
			Severity:  diag.SeverityOf(diag.E0003),
			Stage:     "parse",
			File:      file,
			Line:      lineNo,
			Col:       runeCol(line, i) + 1 + pad,
			Snippet:   snippetAt(line, maxInt(i-16, 0), i+1),
			Message:   "unmatched ']' - a nested tag closed a span that never opened one",
			FixHint:   "put nested tags outside the quoted text of the outer tag",
			LegacyMsg: fmt.Sprintf("Warning: unmatched ']' at line %d col %d", lineNo, i+1+pad),
		})
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func markConsumed(consumed []bool, from, to int) {
	if to < 0 {
		to = len(consumed) - 1
	}
	for i := from; i <= to && i < len(consumed); i++ {
		consumed[i] = true
	}
}

var knownTagPrefixes = map[string]bool{
	"draw": true, "text": true, "erase": true, "move": true, "gen": true,
	"wait": true, "clear": true, "zoom": true, "style": true, "chapter": true,
	"sfx": true, "subtitle": true, "slide": true, "banner": true, "arrow": true,
	"highlight": true, "compare": true, "transition": true, "overlay": true,
	"counter": true, "watermark": true,
}

func isKnownTagPrefix(prefix string) bool {
	return knownTagPrefixes[strings.TrimSpace(prefix)]
}

// looksLikeTag reports whether a bare bracket span carries tag structure - a
// short alphabetic prefix followed by a colon - which is what makes a typo'd tag
// invisible: no regex matched it, so the raw text reaches the TTS.
func looksLikeTag(body string) bool {
	i := strings.Index(body, ":")
	if i <= 0 {
		return false
	}
	head := body[:i]
	if len(head) > 12 {
		return false
	}
	for _, r := range head {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// hasDecimalTailArg reports a trailing ":N.N" argument, which the legacy tag
// pattern cannot match: `[draw:robot:2.5]` is not a tag to the parser at all, so
// the whole span reaches the TTS queue as prose.
func hasDecimalTailArg(body string) bool {
	parts := strings.Split(body, ":")
	if len(parts) < 2 {
		return false
	}
	tail := strings.TrimSpace(parts[len(parts)-1])
	dot := strings.IndexByte(tail, '.')
	if dot <= 0 || dot == len(tail)-1 {
		return false
	}
	for i := 0; i < len(tail); i++ {
		c := tail[i]
		if !(c >= '0' && c <= '9' || c == '.') {
			return false
		}
	}
	return true
}
