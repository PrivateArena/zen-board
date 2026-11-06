package script

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// SplitParts splits a tag body on colons that are not inside double quotes.
//
// This is the one tag splitter: the parser, the compiler handlers and the linter
// all call it, so a quoted argument containing a colon (`[banner:"Act I: Finale"]`)
// cannot be split differently in two places.
func SplitParts(body string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	for _, ch := range body {
		switch {
		case ch == '"':
			inQuote = !inQuote
			current.WriteRune(ch)
		case ch == ':' && !inQuote:
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}
	parts = append(parts, current.String())
	return parts
}

// Unquote strips the surrounding double quotes of an argument.
func Unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// Tag is a parsed tag: the positional arguments, the named key=value parameters,
// the bare flag words and the trailing @coords, all resolved against a spec.
//
// It is what lets the compiler and the linter agree on what a tag said. The
// legacy positional spellings keep working; named parameters win when both are
// present.
type Tag struct {
	Spec   ActionSpec
	Raw    string   // the whole bracket span as written
	Body   string   // everything between "tag:" and "@coords"
	Parts  []string // positional arguments, after stripping keys, unquoted
	Keys   map[string]string
	Flags  []string // bare words (pop, keep)
	Coords []int    // x,y or x,y,w,h from @coords
	// HasCoordGroup records that the span carried an @group at all, even when the
	// numbers were malformed - the difference between "no coords" and "coords this
	// tag cannot have".
	HasCoordGroup bool
	CoordGroup    string
	// CoordGroupValid says the group held x,y or x,y,w,h numbers. An invalid group
	// is a defect on its own, separate from a tag that cannot take coords at all.
	CoordGroupValid bool
}

// Key returns a named parameter value.
func (t Tag) Key(name string) (string, bool) {
	v, ok := t.Keys[name]
	return v, ok
}

// Pos returns the i'th positional argument, or "".
func (t Tag) Pos(i int) string {
	if i < 0 || i >= len(t.Parts) {
		return ""
	}
	return t.Parts[i]
}

// ParseTag resolves a tag body against its spec. body is the text between
// "tag:" and any "@coords"; raw is the whole bracket span.
//
// Grammar (ARCHITECTURE_PLAN_quick_wins §5):
//
//	[ <tag> : <target> ( : <part> )* ( @ <coords> )? ]
//	<part> ::= <preset> | <key>=<value> | <bare-legacy>
func ParseTag(spec ActionSpec, raw, body string, coords []int) Tag {
	t := Tag{Spec: spec, Raw: raw, Body: body, Coords: coords, Keys: map[string]string{}}
	if body != "" {
		t.Parts = splitTagBody(body, &t)
	}
	return t
}

// splitTagBody walks the argument parts once, routing each to positional arguments,
// named keys or flags. Legacy bare values keep their positional slot, which is
// how `[draw:robot:TR:3]` still means preset then duration.
func splitTagBody(body string, t *Tag) []string {
	var positional []string
	for _, part := range splitArguments(body) {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			if len(positional) > 0 || len(t.Keys) > 0 {
				positional = append(positional, "")
			}
			continue
		}
		if key, value, ok := cutKeyValue(trimmed); ok && isKeySpelling(key) {
			if specKey := t.specKeyLookup(key); specKey != "" {
				t.Keys[specKey] = Unquote(value)
				continue
			}
			// An unknown key= is kept verbatim so it stays visible to the linter
			// and to the SVG variant pass-through.
			t.Keys[unknownKeyPrefix+key] = value
			continue
		}
		positional = append(positional, trimmed)
	}
	return positional
}

// splitArguments turns a tag body into its argument tokens. Arguments are separated by
// colons, which is what the legacy forms use, but a group of named parameters is
// space-separated, which is the documented spelling:
//
//	[draw:robot:TR id=bot dur=6 exit=fade]
//
// Only a stretch that actually carries a `key=` is split on spaces. Everything else is
// left whole, because legacy arguments are free text that legitimately contains spaces
// (`[chapter:Act One]`, `[caption:The end of an era]`).
func splitArguments(body string) []string {
	var out []string
	for _, part := range SplitParts(body) {
		if !carriesNamedParam(part) {
			out = append(out, part)
			continue
		}
		// Words that are not parameters belong to the argument they surround, so a
		// multi-word title survives the tail: [banner:Final score 1+1=2 id=x] keeps
		// "Final score 1+1=2" as one argument.
		var prose []string
		flush := func() {
			if len(prose) > 0 {
				out = append(out, strings.Join(prose, " "))
				prose = nil
			}
		}
		for _, tok := range splitWords(part) {
			key, _, isParam := cutKeyValue(tok)
			if isParam && isKeySpelling(key) {
				flush()
				out = append(out, tok)
				continue
			}
			prose = append(prose, tok)
		}
		flush()
	}
	return out
}

// carriesNamedParam reports whether the part carries an `identifier=` token. Requiring a
// key-shaped name - rather than any `=` - is what keeps a legacy free-text argument such
// as [banner:Sum 1+1=2] or [chapter:2+2=happy] in one piece, while a key the tag does not
// define still surfaces as E0006 instead of vanishing into the asset name.
// carriesNamedParam reports whether the part carries an `identifier=` token. Requiring a
// key-shaped name - rather than any `=` - is what keeps a legacy free-text argument such
// as [banner:Sum 1+1=2] or [chapter:2+2=happy] in one piece, while a parameter the tag
// does not define still surfaces as E0006 instead of vanishing into an asset name.
func carriesNamedParam(part string) bool {
	inQuote := false
	for i := 0; i < len(part); i++ {
		if part[i] == '"' {
			inQuote = !inQuote
			continue
		}
		if part[i] != '=' || inQuote {
			continue
		}
		start := i - 1
		for start >= 0 && part[start] != ' ' && part[start] != ':' && part[start] != '"' {
			start--
		}
		if isKeySpelling(part[start+1 : i]) {
			return true
		}
	}
	return false
}

// splitWords separates a named-parameter group on whitespace, keeping a double-quoted
// run (with its quotes) as one token.
func splitWords(part string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, ch := range part {
		switch {
		case ch == '"':
			inQuote = !inQuote
			cur.WriteRune(ch)
		case (ch == ' ' || ch == '\t') && !inQuote:
			flush()
		default:
			cur.WriteRune(ch)
		}
	}
	flush()
	return out
}

// isKeySpelling accepts only an identifier before the `=`. That is what keeps prose with
// an equals sign - [banner:Sum 1+1=2], [chapter:2+2=happy] - in one piece instead of
// becoming a parameter nobody declared.
func isKeySpelling(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9', c == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// unknownKeyPrefix marks a key= spelling the spec does not define. Keeping it
// addressable (rather than dropping it) lets the linter report it and lets SVG
// variants pass through untouched.
const unknownKeyPrefix = "?"

// UnknownKeys lists key= spellings no parameter of the spec defines.
func (t Tag) UnknownKeys() []string {
	var out []string
	for k := range t.Keys {
		if strings.HasPrefix(k, unknownKeyPrefix) {
			out = append(out, strings.TrimPrefix(k, unknownKeyPrefix))
		}
	}
	return out
}

func (t Tag) specKeyLookup(key string) string {
	if _, ok := t.Spec.findParam(key); ok {
		return key
	}
	if alias, ok := paramAliases[key]; ok {
		return alias
	}
	return ""
}

// paramAliases spell historical key= spellings that never matched a parameter
// name, so a script that wrote `colour=` still gets `color=`.
var paramAliases = map[string]string{
	"colour":    "color",
	"seconds":   "dur",
	"duration":  "dur",
	"anchorpos": "anchor",
}

func cutKeyValue(s string) (key, value string, ok bool) {
	i := strings.Index(s, "=")
	if i <= 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// RuneOffsetInLine converts a byte index inside a line to a 1-based column.
func RuneOffsetInLine(line string, byteIdx int) int {
	if byteIdx > len(line) {
		byteIdx = len(line)
	}
	if byteIdx < 0 {
		byteIdx = 0
	}
	return utf8.RuneCountInString(line[:byteIdx]) + 1
}

// lastCoordMark finds the trailing coordinate group, and only that. It must not treat the
// `@` of an element reference as one: [arrow:@bot:@curve] carries no coordinates at all,
// and taking the last @ as a group made the linter reject the very references the
// compiler resolves happily. An `@` opens a coordinate group only when what follows it is
// number-shaped, or an `@` that the tag cannot absorb - "curve:curved:2" - where
// reporting a coordinate problem is more useful than reporting nothing.
func lastCoordMark(inner string) int {
	for at := len(inner) - 1; at >= 0; at-- {
		if inner[at] != '@' || insideQuotes(inner[:at]) {
			continue
		}
		group := inner[at+1:]
		if isNumericGroup(group) {
			return at
		}
		if !isElementRef(group) {
			return at
		}
		// A reference: it is an argument like any other, so stop looking and leave the
		// body intact.
		return -1
	}
	return -1
}

// isNumericGroup reports a group made only of digits, commas and spaces - valid "100,200"
// or malformed "10,x", both of which are coordinate problems.
func isNumericGroup(group string) bool {
	if group == "" {
		return false
	}
	for i := 0; i < len(group); i++ {
		c := group[i]
		if c >= '0' && c <= '9' || c == ',' || c == ' ' {
			continue
		}
		return false
	}
	return true
}

// isElementRef reports a bare address such as "@bot" or "@curve:curved:2" - a leading
// identifier after the sigil, which is an argument rather than a coordinate group.
func isElementRef(group string) bool {
	if group == "" {
		return false
	}
	c := group[0]
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// ParseTagSpec resolves a bracket span to its spec and structured tag. It is the
// only place bracket text is turned into arguments, so the compiler, the linter
// and `check -dsl` cannot disagree about what a tag said.
//
// ok is false when the span is not a tag at all (bare prose, or an unknown prefix
// that no pattern consumed).
func ParseTagSpec(raw string) (spec ActionSpec, tag Tag, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) < 3 || trimmed[0] != '[' || trimmed[len(trimmed)-1] != ']' {
		return ActionSpec{}, Tag{}, false
	}
	inner := trimmed[1 : len(trimmed)-1]

	var coords []int
	var coordRaw string
	coordGroup := false
	body := inner
	if at := lastCoordMark(inner); at >= 0 {
		coordRaw = inner[at+1:]
		coordGroup = true
		if parsed, valid := parseCoordList(coordRaw); valid {
			coords = parsed
		}
		body = inner[:at]
	}

	name := body
	rest := ""
	if colon := strings.IndexByte(body, ':'); colon >= 0 {
		name, rest = body[:colon], body[colon+1:]
	}
	spec, found := ByTag(name)
	if !found {
		if name == "erase" {
			spec, _ = ByTag("erase")
		} else {
			return ActionSpec{}, Tag{}, false
		}
	}
	if spec.Tag == "clear" {
		rest = ""
		if body != "clear" {
			rest = strings.TrimPrefix(body, "clear:")
		}
	}
	tag = ParseTag(spec, trimmed, rest, coords)
	tag.HasCoordGroup = coordGroup
	tag.CoordGroup = coordRaw
	tag.CoordGroupValid = coordGroup && len(coords) > 0
	return spec, tag, true
}

// insideQuotes reports an unbalanced double quote, which means the '@' that
// follows is part of the quoted text, not a coordinate group.
func insideQuotes(s string) bool {
	return strings.Count(s, "\"")%2 == 1
}

// parseCoordList reads "x,y" or "x,y,w,h" out of the trailing @ group.
func parseCoordList(s string) ([]int, bool) {
	fields := strings.Split(s, ",")
	if len(fields) != 2 && len(fields) != 4 {
		return nil, false
	}
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			return nil, false
		}
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}
