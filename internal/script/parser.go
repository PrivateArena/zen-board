package script

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"zen-board/internal/constant"
	"zen-board/internal/diag"
	"zen-board/internal/model"
)

// The parser matches through the DSL table (dsl.go): every pattern is declared
// exactly once, and the linter, the compiler and `check --dsl` all read the same
// specs. The indices are the shapes listed there.
var (
	drawRegex       = mustPattern("draw", 0)
	textRegex       = mustPattern("text", 0)
	eraseRegex      = mustPattern("erase", 1) // the single-layer shape
	moveRegex       = mustPattern("move", 0)
	genRegex        = mustPattern("gen", 0)
	waitRegex       = mustPattern("wait", 0)
	clearRegex      = mustPattern("clear", 0)
	clearScopeRegex = mustPattern("clear", 1)
	zoomRegex       = mustPattern("zoom", 0)
	styleRegex      = mustPattern("style", 0)
	chapterRegex    = mustPattern("chapter", 0)
	sfxRegex        = mustPattern("sfx", 0)
	subtitleRegex   = mustPattern("subtitle", 0)
	slideRegex      = mustPattern("slide", 0)
	bannerRegex     = mustPattern("banner", 0)
	arrowRegex      = mustPattern("arrow", 0)
	highlightRegex  = mustPattern("highlight", 0)
	compareRegex    = mustPattern("compare", 0)
	transitionRegex = mustPattern("transition", 0)
	overlayRegex    = mustPattern("overlay", 0)
	counterRegex    = mustPattern("counter", 0)
	watermarkRegex  = mustPattern("watermark", 0)
	characterRegex  = mustPattern("character", 0)
	stickerRegex    = mustPattern("sticker", 0)
	captionRegex    = mustPattern("caption", 0)
)

func mustPattern(tag string, i int) *regexp.Regexp {
	re := Pattern(tag, i)
	if re == nil {
		panic(fmt.Sprintf("script: DSL table has no pattern %s[%d]", tag, i))
	}
	return re
}

// Parse splits a script into lines and their embedded actions. Spans that
// overlap (nested or unclosed tags) are dropped and reported by Lint; Parse
// itself never panics. Use ParseChecked when the caller wants the findings.
func Parse(input string) []model.ScriptLine {
	lines, _ := ParseChecked(input, "")
	return lines
}

// ParseChecked is Parse plus the diagnostics produced while parsing. file is
// only used to label findings.
func ParseChecked(input, file string) ([]model.ScriptLine, []diag.Finding) {
	lines := strings.Split(input, "\n")
	var result []model.ScriptLine
	var findings []diag.Finding
	lineOff := 0

	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			lineOff += len(raw) + 1
			continue
		}
		// pad: byte offset of the trimmed line within its file line, so columns
		// match what an editor shows. lineOff: absolute offset of the raw line in
		// the whole script.
		pad := len(raw) - len(strings.TrimLeft(raw, " \t\v\f\r"))

		cleanText, actions, ff := extractActions(line, i+1, file, lineOff+pad, pad)
		findings = append(findings, ff...)
		for j := range actions {
			actions[j].SrcPath = file
		}
		result = append(result, model.ScriptLine{
			Text:    cleanText,
			Actions: actions,
		})
		lineOff += len(raw) + 1 // + newline
	}

	return result, findings
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// tagMatch is one raw tag span recognised in a script line, carrying its byte
// position plus everything the compiler needs (parsed tag, coords, variants).
type tagMatch struct {
	start, end int
	tag        string
	isWait     bool
	waitVal    float64
	x, y, w, h int
	variants   map[string]string
}

func extractActions(line string, lineNo int, file string, lineOff, pad int) (string, []model.DrawAction, []diag.Finding) {
	var actions []model.DrawAction
	var tags []tagMatch
	cleanBuilder := strings.Builder{}
	consumed := make([]bool, len(line))
	lastPos := 0

	extractDrawTag := func() {
		matches := drawRegex.FindAllStringSubmatchIndex(line, -1)
		for _, m := range matches {
			assetID := line[m[2]:m[3]]
			tag := assetID
			variants := make(map[string]string)
			var named []string
			if len(m) >= 5 && m[4] != -1 && m[5] != -1 {
				rawVariants := line[m[4]:m[5]]
				for _, vp := range strings.Split(rawVariants, ":") {
					vp = strings.TrimSpace(vp)
					if vp == "" {
						continue
					}
					for _, tok := range strings.Fields(vp) {
						key, _, isKV := cutKeyValue(tok)
						switch {
						case !isKV:
							// A bare word is a positional argument: a preset or a duration.
							if tag == assetID {
								tag = assetID + ":" + tok
							} else {
								tag += ":" + tok
							}
						case IsAuthoringKey(key) || DeclaresKey("draw", key):
							// An engine parameter, not a paint attribute: it stays in the
							// tag so the grammar can resolve it, and never reaches the SVG.
							named = append(named, tok)
						default:
							kv := strings.SplitN(tok, "=", 2)
							variants[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
						}
					}
				}
			}
			if len(named) > 0 {
				tag += " " + strings.Join(named, " ")
			}
			ti := tagMatch{start: m[0], end: m[1], tag: tag, variants: variants}
			if len(m) >= 8 && m[6] != -1 && m[7] != -1 {
				coords := strings.Split(line[m[6]:m[7]], ",")
				if len(coords) >= 2 {
					ti.x, _ = strconv.Atoi(coords[0])
					ti.y, _ = strconv.Atoi(coords[1])
				}
				if len(coords) >= 4 {
					ti.w, _ = strconv.Atoi(coords[2])
					ti.h, _ = strconv.Atoi(coords[3])
				}
			}
			tags = append(tags, ti)
		}
	}

	extractStandardTag := func(re *regexp.Regexp, prefix string) {
		matches := re.FindAllStringSubmatchIndex(line, -1)
		for _, m := range matches {
			content := line[m[2]:m[3]]
			ti := tagMatch{start: m[0], end: m[1], tag: prefix + content}
			if len(m) >= 6 && m[4] != -1 && m[5] != -1 {
				coords := strings.Split(line[m[4]:m[5]], ",")
				if len(coords) >= 2 {
					ti.x, _ = strconv.Atoi(coords[0])
					ti.y, _ = strconv.Atoi(coords[1])
				}
				if len(coords) >= 4 {
					ti.w, _ = strconv.Atoi(coords[2])
					ti.h, _ = strconv.Atoi(coords[3])
				}
			}
			tags = append(tags, ti)
		}
	}

	extractSimpleTag := func(re *regexp.Regexp, prefix string) {
		matches := re.FindAllStringSubmatchIndex(line, -1)
		for _, m := range matches {
			content := line[m[2]:m[3]]
			tags = append(tags, tagMatch{start: m[0], end: m[1], tag: prefix + content})
		}
	}

	extractComplexTag := func(re *regexp.Regexp, prefix string) {
		matches := re.FindAllStringSubmatchIndex(line, -1)
		for _, m := range matches {
			rawContent := line[m[2]:m[3]]
			parts := SplitParts(rawContent)
			tag := prefix
			for i, p := range parts {
				if i == 0 {
					tag += p
				} else {
					tag += ":" + p
				}
			}
			tags = append(tags, tagMatch{start: m[0], end: m[1], tag: tag})
		}
	}

	extractDrawTag()
	extractStandardTag(textRegex, constant.TAG_TEXT)
	extractStandardTag(eraseRegex, constant.TAG_ERASE)
	extractStandardTag(moveRegex, constant.TAG_MOVE)
	extractStandardTag(genRegex, constant.TAG_GEN)

	extractComplexTag(slideRegex, constant.TAG_SLIDE)
	extractComplexTag(bannerRegex, constant.TAG_BANNER)
	extractComplexTag(watermarkRegex, constant.TAG_WATERMARK)
	extractComplexTag(arrowRegex, constant.TAG_ARROW)
	extractComplexTag(highlightRegex, constant.TAG_HIGHLIGHT)
	extractComplexTag(compareRegex, constant.TAG_COMPARE)
	extractComplexTag(transitionRegex, constant.TAG_TRANSITION)
	extractComplexTag(overlayRegex, constant.TAG_OVERLAY)
	extractComplexTag(characterRegex, constant.TAG_CHARACTER)
	extractComplexTag(stickerRegex, constant.TAG_STICKER)
	extractComplexTag(captionRegex, constant.TAG_CAPTION)
	extractComplexTag(counterRegex, constant.TAG_COUNTER)

	extractSimpleTag(zoomRegex, constant.TAG_ZOOM)
	extractSimpleTag(styleRegex, constant.TAG_STYLE)
	extractSimpleTag(chapterRegex, constant.TAG_CHAPTER)
	extractSimpleTag(sfxRegex, constant.TAG_SFX)
	extractSimpleTag(subtitleRegex, constant.TAG_SUBTITLE)

	waitMatches := waitRegex.FindAllStringSubmatchIndex(line, -1)
	for _, m := range waitMatches {
		val, _ := strconv.ParseFloat(line[m[2]:m[3]], 64)
		tags = append(tags, tagMatch{start: m[0], end: m[1], isWait: true, waitVal: val})
	}

	clearMatches := clearRegex.FindAllStringSubmatchIndex(line, -1)
	clearMatches = append(clearMatches, clearScopeRegex.FindAllStringSubmatchIndex(line, -1)...)
	for _, m := range clearMatches {
		tag := constant.TAG_CLEAR
		// reClear has no capture group; reClearScoped has one. Indexing blind here
		// panics on a plain [clear].
		if len(m) > 3 && m[2] >= 0 {
			tag = constant.TAG_CLEAR + ":" + line[m[2]:m[3]]
		}
		tags = append(tags, tagMatch{start: m[0], end: m[1], tag: tag})
	}

	sort.SliceStable(tags, func(i, j int) bool { return tags[i].start < tags[j].start })

	var findings []diag.Finding
	for _, t := range tags {
		// Overlapping span: two different patterns matched the same bytes, e.g.
		// the nested `[text:"a [draw:x] b"]`. Slicing line[lastPos:t.start] here
		// used to panic; the tag is dropped and reported instead.
		if t.start < lastPos {
			findings = append(findings, diag.Finding{
				Code:      diag.E0003,
				Severity:  diag.SeverityOf(diag.E0003),
				Stage:     "parse",
				File:      file,
				Line:      lineNo,
				Col:       t.start + 1 + pad,
				Message:   fmt.Sprintf("tag %q overlaps a preceding tag that ends at column %d", tagLabel(t), lastPos+1),
				FixHint:   "close the outer tag before opening an inner one, or put the nested tag outside the quoted text",
				Snippet:   snippetAt(line, t.start, t.end),
				LegacyMsg: fmt.Sprintf("Warning: overlapping tag %q at line %d col %d ignored", tagLabel(t), lineNo, t.start+1),
			})
			continue
		}

		cleanBuilder.WriteString(line[lastPos:t.start])

		// A consumed span that still contains an opening bracket swallowed a nested
		// tag - `[text:"a [draw:x] b"]` is the canonical shape. The inner tag is
		// gone from the byte stream, so report it here rather than lose it.
		if strings.Contains(line[t.start+1:t.end], "[") {
			findings = append(findings, diag.Finding{
				Code:      diag.E0003,
				Severity:  diag.SeverityOf(diag.E0003),
				Stage:     "parse",
				File:      file,
				Line:      lineNo,
				Col:       pad + runeCol(line, t.start) + 1,
				Message:   fmt.Sprintf("tag %q swallows a nested tag", snippetAt(line, t.start, t.end)),
				FixHint:   "close the outer tag before opening an inner one, or move the inner tag outside the quoted text",
				Snippet:   snippetAt(line, t.start, t.end),
				LegacyMsg: fmt.Sprintf("Warning: tag %q at line %d swallows a nested tag", snippetAt(line, t.start, t.end), lineNo),
			})
		}

		currentClean := cleanBuilder.String()
		wordCount := len(strings.Fields(currentClean))

		src := model.DrawAction{
			SrcOffset: lineOff + t.start,
			SrcLength: t.end - t.start,
			SrcLine:   lineNo,
			SrcCol:    pad + runeCol(line, t.start) + 1,
			SrcRaw:    line[t.start:t.end],
		}

		if t.isWait {
			src.Tag = constant.TAG_WAIT + strconv.FormatFloat(t.waitVal, 'f', -1, 64)
			src.WordIndex = wordCount
			src.TriggerAfterWord = wordCount > 0
			actions = append(actions, src)
		} else {
			tag := t.tag
			triggerAfter := false
			if strings.HasSuffix(tag, "+") {
				tag = strings.TrimSuffix(tag, "+")
				triggerAfter = true
			}
			src.Tag = tag
			src.WordIndex = wordCount
			src.X = t.x
			src.Y = t.y
			src.W = t.w
			src.H = t.h
			src.TriggerAfterWord = triggerAfter
			src.AssetVariant = t.variants
			actions = append(actions, src)
		}

		lastPos = t.end
		for i := t.start; i < t.end && i < len(consumed); i++ {
			consumed[i] = true
		}
	}
	cleanBuilder.WriteString(line[lastPos:])
	findings = append(findings, residualSpanFindings(line, consumed, lineNo, file, pad)...)

	cleanText := cleanBuilder.String()
	words := strings.Fields(cleanText)
	return strings.Join(words, " "), actions, findings
}

// runeCol converts a byte index inside a line into the 1-based rune column an
// editor reports. Byte columns drift on any non-ASCII narration.
func runeCol(line string, byteIdx int) int {
	if byteIdx > len(line) {
		byteIdx = len(line)
	}
	if byteIdx < 0 {
		byteIdx = 0
	}
	return utf8.RuneCountInString(line[:byteIdx])
}

// tagLabel names a matched tag for diagnostics without assuming its shape.
func tagLabel(t tagMatch) string {
	if t.isWait {
		return fmt.Sprintf("[wait:%g]", t.waitVal)
	}
	return "[" + t.tag + "]"
}

// snippetAt returns a bounded one-line excerpt for a diagnostic.
func snippetAt(line string, start, end int) string {
	const max = 72
	if start < 0 {
		start = 0
	}
	if end > len(line) || end < start {
		end = len(line)
	}
	s := line[start:end]
	if len(s) > max {
		s = s[:max] + "..."
	}
	return s
}
