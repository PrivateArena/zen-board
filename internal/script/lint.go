package script

import (
	"fmt"
	"strings"

	"zen-board/internal/diag"
	"zen-board/internal/model"
)

// AssetLookup answers what the linter needs from the asset catalog. Declaring it
// here instead of importing the assets package keeps the grammar layer free of I/O,
// so the parser, the linter and their tests never touch a disk.
type AssetLookup interface {
	// Lookup resolves an asset id. found is false when no file matches; fromIndex
	// says the id came from index.json rather than the .png fallback rule; isSVG
	// reports a vector asset, which is rasterised at its layout size.
	Lookup(id string) (found, fromIndex, isSVG bool)
	// HasCursorPreset reports whether a cursor sprite set is installed. A cursor
	// name that resolves to nothing used to mean no hand was drawn at all.
	HasCursorPreset(name string) bool
	// SupportsVariants reports whether unknown key=value pairs on an asset are SVG
	// variant attributes (the documented mechanism) rather than a typo. Only a
	// vector asset can take them.
	SupportsVariants(id string) bool
}

// noAssets is the null lookup for linting without a catalog.
type noAssets struct{}

func (noAssets) Lookup(string) (bool, bool, bool) { return true, false, false }
func (noAssets) HasCursorPreset(string) bool      { return false }
func (noAssets) SupportsVariants(string) bool     { return false }

// LintOptions selects how much of the catalog is consulted.
type LintOptions struct {
	// SkipAssetResolution turns the E0001 pass off (`check -fast`), leaving only
	// the rules that need no files. It is not a speed mode: nothing here is slow.
	SkipAssetResolution bool
}

// lintRun is one lint pass: the file, the catalog and the findings so far.
type lintRun struct {
	file       string
	lookup     AssetLookup
	report     *diag.Report
	skipAssets bool
}

// Lint checks a script against the grammar table and the asset catalog.
//
// The rules run over the raw bytes and over the actions the parser produced -
// never over the cleaned ScriptLine text, whose tags have already been removed.
// Nothing here opens a socket, which is what makes `check` free to run before
// every edit. Error-tier findings block; warnings never do.
func Lint(src *Source, lookup AssetLookup, opts LintOptions) *diag.Report {
	if lookup == nil {
		lookup = noAssets{}
	}
	l := &lintRun{file: src.Path, lookup: lookup, report: &diag.Report{}, skipAssets: opts.SkipAssetResolution}

	lines, parseFindings := ParseChecked(string(src.Raw), src.Path)
	l.report.Extend(&diag.Report{Findings: parseFindings})

	ids := newIDTracker(l)
	for _, line := range lines {
		for i := range line.Actions {
			act := &line.Actions[i]
			spec, tag := lintTag(act)
			refs := lintRefs(spec, tag)

			l.validateArity(spec, tag, act)
			l.validateCoords(spec, tag, act)
			l.validateEnums(spec, tag, act)
			l.validateNoop(spec, act)
			l.validateEmits(spec, tag, act)
			l.validateAssets(spec, tag, act)
			ids.define(spec, tag, act)
			for _, ref := range refs {
				ids.use(spec, act, ref)
			}
		}
	}
	return l.report
}

// lintTag reads the tag out of the raw bracket span through the table, so the
// linter sees exactly what the compiler sees: same spec, same splitter, same key
// routing. The re-serialised action tag is the fallback for timelines built
// without a source file.
func lintTag(a *model.DrawAction) (ActionSpec, Tag) {
	if a.SrcRaw != "" {
		if spec, tag, ok := ParseTagSpec(a.SrcRaw); ok {
			if len(tag.Coords) == 0 {
				tag.Coords = coordList(a)
			}
			return spec, tag
		}
	}
	spec, _ := Route(a.Tag)
	body := a.Tag
	if spec.Tag != "draw" && spec.Tag != "" {
		body = strings.TrimPrefix(a.Tag, spec.Prefix())
	}
	return spec, ParseTag(spec, a.SrcRaw, body, coordList(a))
}

// lintRefs returns the @element references an action carries.
func lintRefs(spec ActionSpec, tag Tag) []string {
	var refs []string
	for _, p := range spec.Positional {
		if p.Kind != KindElement {
			continue
		}
		v := positionalValue(spec, tag, p.Name)
		if strings.HasPrefix(v, "@") {
			refs = append(refs, strings.TrimPrefix(v, "@"))
		}
	}
	return refs
}

// coordList re-derives the @coords of an action in the shape the grammar reports.
func coordList(a *model.DrawAction) []int {
	if a.X == 0 && a.Y == 0 && a.W == 0 && a.H == 0 {
		return nil
	}
	if a.W == 0 && a.H == 0 {
		return []int{a.X, a.Y}
	}
	return []int{a.X, a.Y, a.W, a.H}
}

// add records a finding against the action's source position.
func (l *lintRun) add(a *model.DrawAction, code, msg, hint string) {
	l.report.Add(diag.Finding{
		Code:      code,
		Severity:  diag.SeverityOf(code),
		Stage:     "lint",
		File:      l.file,
		Line:      a.SrcLine,
		Col:       a.SrcCol,
		Snippet:   a.SrcRaw,
		Message:   msg,
		FixHint:   hint,
		LegacyMsg: fmt.Sprintf("Warning: %s (%s)", msg, a.SrcRaw),
	})
}

// validateArity: an argument count the handler cannot index. Today `[arrow:TL]` and
// `[compare:onlyone]` panic inside the compiler.
func (l *lintRun) validateArity(spec ActionSpec, tag Tag, a *model.DrawAction) {
	if spec.Tag == "clear" || spec.Tag == "wait" {
		return
	}
	n := significantArgs(tag)
	if n < spec.MinArgs {
		l.add(a, diag.E0009,
			fmt.Sprintf("%s expects at least %d argument(s), got %d", canonicalForm(spec), spec.MinArgs, n),
			"e.g. "+exampleOf(spec))
		return
	}
	if n > spec.MaxArgs {
		l.add(a, diag.E0004,
			fmt.Sprintf("%s takes at most %d arguments, got %d", canonicalForm(spec), spec.MaxArgs, n),
			"extra parts are absorbed into the last argument or silently ignored; check the argument order")
	}
}

// significantArgs counts the positional arguments that carry a value. Trailing
// empty parts are how the splitter represents a missing argument.
func significantArgs(tag Tag) int {
	n := 0
	for _, p := range tag.Parts {
		if strings.TrimSpace(p) != "" {
			n++
		}
	}
	return n
}

// validateCoords: @coords on a tag whose pattern has no coordinate group. Today the
// coordinate text is absorbed into the asset id, producing either a hard error
// later or a silently missing draw.
func (l *lintRun) validateCoords(spec ActionSpec, tag Tag, a *model.DrawAction) {
	if !tag.HasCoordGroup {
		return
	}
	if !spec.HasCoords {
		l.add(a, diag.E0010,
			fmt.Sprintf("%s has no @coords group; %q was absorbed into an argument", canonicalForm(spec), tag.CoordGroup),
			"drop the @coords, or place the element with a preset / anchor= instead")
		return
	}
	if !tag.CoordGroupValid {
		l.add(a, diag.E0004,
			fmt.Sprintf("malformed @coords %q", tag.CoordGroup),
			"@coords must be x,y or x,y,w,h")
		return
	}
	switch len(tag.Coords) {
	case 2, 4:
	default:
		l.add(a, diag.E0004,
			fmt.Sprintf("@coords must be x,y or x,y,w,h, got %d numbers", len(tag.Coords)),
			"use @x,y to place at native size, or @x,y,w,h to set the box")
	}
}

// validateEnums checks every enum-typed argument against the table: presets and
// anchors (E0005) and the style/mask/hand/cursor vocabulary (E0006).
func (l *lintRun) validateEnums(spec ActionSpec, tag Tag, a *model.DrawAction) {
	for _, p := range spec.Positional {
		if len(p.Enum) == 0 || !fillsSlot(spec, p.Name, tag) {
			continue
		}
		val := positionalValue(spec, tag, p.Name)
		if val == "" || strings.HasPrefix(val, "@") {
			continue
		}
		if containsFold(p.Enum, val) {
			continue
		}
		code := enumCode(p)
		if spec.Tag == "zoom" {
			code = diag.E0012
		}
		l.add(a, code,
			fmt.Sprintf("%s: unknown %s %q", spec.Tag, p.Name, val),
			"known values: "+strings.Join(p.Enum, ", "))
	}

	for _, p := range spec.Keys {
		val, ok := tag.Key(p.Name)
		if !ok || val == "" {
			continue
		}
		if p.Name == "cursor" {
			// A cursor name is only valid if its sprite set is installed, which the
			// catalog knows and a static enum cannot.
			if !containsFold(cursorNames, val) && !l.lookup.HasCursorPreset(val) {
				l.add(a, diag.E0006,
					fmt.Sprintf("cursor preset %q is not installed under assets/cursors/", val),
					"install the preset directory, or use one of: "+strings.Join(l.cursorVocabulary(), ", "))
			}
			continue
		}
		if len(p.Enum) == 0 || containsFold(p.Enum, val) {
			continue
		}
		l.add(a, enumCode(p),
			fmt.Sprintf("%s: unknown %s %q", spec.Tag, p.Name, val),
			"known values: "+strings.Join(p.Enum, ", "))
	}

	for _, key := range tag.UnknownKeys() {
		if l.variantKeyOK(spec, tag, key) {
			continue
		}
		if near := l.nearParam(spec, key); near != "" {
			l.add(a, diag.E0006,
				fmt.Sprintf("%s: no parameter %q; did you mean %q?", spec.Tag, key, near+"="),
				"run `zen-board check -dsl` for the parameters of "+spec.Tag)
			continue
		}
		l.add(a, diag.E0006,
			fmt.Sprintf("%s: parameter %q is not part of the grammar", spec.Tag, key+"=..."),
			"run `zen-board check -dsl` for the accepted parameters of "+spec.Tag)
	}
}

// variantKeyOK accepts unknown key=value pairs on an asset argument that resolves
// to an SVG: those are variant attributes, the documented mechanism.
func (l *lintRun) variantKeyOK(spec ActionSpec, tag Tag, key string) bool {
	for _, name := range spec.AssetArgs {
		id := positionalValue(spec, tag, name)
		if id == "" || strings.HasPrefix(id, "@") {
			continue
		}
		if l.lookup.SupportsVariants(id) {
			return true
		}
	}
	return false
}

// nearParam suggests the closest declared parameter, so a `colour=` typo is named
// rather than reported as unknown.
func (l *lintRun) nearParam(spec ActionSpec, key string) string {
	best, bestDist := "", 3
	for _, p := range append(append([]Param{}, spec.Keys...), spec.Positional...) {
		if d := EditDistance(strings.ToLower(key), p.Name); d < bestDist {
			best, bestDist = p.Name, d
		}
	}
	if bestDist > 2 {
		return ""
	}
	return best
}

func (l *lintRun) cursorVocabulary() []string {
	return CursorPresets()
}

func enumCode(p Param) string {
	if p.Kind == KindPreset || p.Name == "anchor" {
		return diag.E0005
	}
	return diag.E0006
}

// fillsSlot reports whether the tag actually carries a value in the named
// positional slot.
func fillsSlot(spec ActionSpec, name string, tag Tag) bool {
	for i, p := range spec.Positional {
		if p.Name != name {
			continue
		}
		return i < len(tag.Parts) && strings.TrimSpace(tag.Parts[i]) != ""
	}
	return false
}

// validateNoop reports a tag that parses, is documented, and does nothing - the
// trap that makes an agent believe a sound effect exists in the mix.
func (l *lintRun) validateNoop(spec ActionSpec, a *model.DrawAction) {
	if !spec.IsNoop() {
		return
	}
	l.add(a, diag.E0014,
		fmt.Sprintf("[%s:...] is a documented no-op: nothing is emitted", spec.Tag),
		"there is no per-event audio channel yet; remove the tag or mix the sound in post")
}

// validateEmits reports a tag that is consumed but produces no event at all -
// `[text:unquoted]` is the classic case, and today it silently renders a gap while
// the words are read aloud.
func (l *lintRun) validateEmits(spec ActionSpec, tag Tag, a *model.DrawAction) {
	if spec.Tag != "text" {
		return
	}
	if strings.Count(a.Tag, `"`) < 2 {
		l.add(a, diag.E0008,
			"[text:...] without two quotes produces no event, and the raw text is read aloud",
			`quote the content: [text:"your words":preset]`)
	}
}

// validateAssets resolves every argument the grammar declares to be an asset.
func (l *lintRun) validateAssets(spec ActionSpec, tag Tag, a *model.DrawAction) {
	if l.skipAssets {
		return
	}
	for _, name := range spec.AssetArgs {
		id := positionalValue(spec, tag, name)
		if id == "" || strings.HasPrefix(id, "@") || id == "*" {
			continue
		}
		found, _, isSVG := l.lookup.Lookup(id)
		if !found {
			l.add(a, diag.E0001,
				fmt.Sprintf("unknown asset id %q", id),
				"list the catalog with `zen-board assets list`")
			continue
		}
		if isSVG && len(tag.Coords) == 2 {
			l.add(a, diag.E0011,
				fmt.Sprintf("SVG asset %q placed with @x,y but no w,h: it is never rasterised, so nothing is drawn", id),
				"use @x,y,w,h for a vector asset")
		}
	}
}

// idTracker resolves @refs against the ids defined earlier in the same script.
type idTracker struct {
	l       *lintRun
	defined map[string]int
}

func newIDTracker(l *lintRun) *idTracker { return &idTracker{l: l, defined: map[string]int{}} }

func (t *idTracker) define(spec ActionSpec, tag Tag, a *model.DrawAction) {
	id, ok := tag.Key("id")
	if !ok || id == "" {
		return
	}
	if line, dup := t.defined[id]; dup {
		t.l.add(a, diag.E0013,
			fmt.Sprintf("duplicate id %q, first defined on line %d", id, line),
			"ids must be unique so an @ref resolves to exactly one element")
		return
	}
	t.defined[id] = a.SrcLine
}

func (t *idTracker) use(spec ActionSpec, a *model.DrawAction, ref string) {
	if _, ok := t.defined[ref]; ok {
		return
	}
	t.l.add(a, diag.E0013,
		fmt.Sprintf("@%s does not resolve: no earlier tag defines id=%s", ref, ref),
		"place the element with id="+ref+" before referencing it")
}

// CanonicalForm renders the spec's canonical spelling, e.g.
// `[arrow:from:to[:style[:duration]]]`. Exported so `check -dsl` and the docs print
// the same string the diagnostics do.
func CanonicalForm(spec ActionSpec) string { return canonicalForm(spec) }

func canonicalForm(spec ActionSpec) string {
	var sb strings.Builder
	sb.WriteString("[")
	sb.WriteString(spec.Tag)
	for i, p := range spec.Positional {
		if i >= spec.MaxArgs {
			break
		}
		if i < spec.MinArgs {
			fmt.Fprintf(&sb, ":%s", p.Name)
			continue
		}
		fmt.Fprintf(&sb, "[:%s]", p.Name)
	}
	if len(spec.Keys) > 0 {
		sb.WriteString("[:key=value ...]")
	}
	if spec.HasCoords {
		sb.WriteString("[@x,y[,w,h]]")
	}
	sb.WriteString("]")
	return sb.String()
}

func exampleOf(spec ActionSpec) string {
	examples := spec.TagExamples()
	if len(examples) == 0 {
		return canonicalForm(spec)
	}
	return examples[0]
}

func positionalValue(spec ActionSpec, tag Tag, name string) string {
	if v, ok := tag.Key(name); ok {
		return v
	}
	for i, p := range spec.Positional {
		if p.Name == name {
			return tag.Pos(i)
		}
	}
	return ""
}

func containsFold(list []string, v string) bool {
	for _, item := range list {
		if strings.EqualFold(item, v) {
			return true
		}
	}
	return false
}

// Route names the handler an action tag dispatches to, derived from the table. The
// compiler routes through it, so the grammar, the linter and the compiler cannot
// disagree about which tag does what.
func Route(actionTag string) (ActionSpec, string) {
	// The parser spells the timing directive "WAIT:" (constant.TAG_WAIT) because it
	// must never collide with an asset named "wait".
	if strings.HasPrefix(actionTag, "WAIT:") || strings.HasPrefix(actionTag, "wait:") {
		return specOr("wait"), "wait"
	}
	if actionTag == "erase:*" || strings.HasPrefix(actionTag, "erase:") {
		return specOr("erase"), "erase"
	}
	if actionTag == "clear" {
		return specOr("clear"), "clear"
	}
	for _, spec := range table {
		prefix := spec.Prefix()
		if prefix == spec.Tag {
			continue
		}
		if strings.HasPrefix(actionTag, prefix) {
			return spec, spec.Tag
		}
	}
	return specOr("draw"), "draw"
}

func specOr(tag string) ActionSpec {
	spec, ok := ByTag(tag)
	if !ok {
		return ActionSpec{Tag: tag}
	}
	return spec
}

// EditDistance is the Levenshtein distance behind the "did you mean" suggestions.
func EditDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// SuggestTag returns the closest known tag within edit distance 2, or "".
func SuggestTag(prefix string) string {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	best, bestDist := "", 3
	for _, spec := range table {
		if d := EditDistance(prefix, spec.Tag); d < bestDist {
			best, bestDist = spec.Tag, d
		}
	}
	if bestDist > 2 {
		return ""
	}
	return best
}
