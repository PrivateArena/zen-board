package script

import (
	"regexp"
)

// ActionSpec is one entry of the scripting grammar. This table is the single
// source of truth: the parser matches against these patterns, the compiler
// re-reads the same specs to split parts, the linter reports their argument
// counts and enums, and `check --dsl` prints them. Nothing else may define what
// a tag looks like.
type ActionSpec struct {
	// Tag is the bracket vocabulary without the colon, e.g. "draw", "clear".
	Tag string
	// Status is "stable", "legacy" (kept for backward compatibility), or "noop"
	// (accepted, deliberately does nothing).
	Status string
	// Patterns are tried in order; the first match consumes the span. Tags with
	// several shapes (erase / erase:*) share one spec.
	Patterns []*regexp.Regexp
	// HasCoords marks a pattern with a trailing @x,y / @x,y,w,h group.
	HasCoords bool
	// QuoteAware splits the body on colons outside double quotes (the complex tag
	// family). Everything else keeps the body intact for its handler.
	QuoteAware bool
	// Positional are the colon-separated arguments, in order. Names double as the
	// key= spellings accepted by the unified tail.
	Positional []Param
	// Keys are the named parameters the tag understands.
	Keys []Param
	// MinArgs, MaxArgs bound the positional argument count.
	MinArgs int
	MaxArgs int
	// AssetArgs names which positional arguments must resolve to an asset id.
	AssetArgs []string
	// EmitsEvents lists the FrameEvent.EventType values the compiler produces.
	EmitsEvents []string
	// Notes carries the documentation line printed by `check --dsl`.
	Notes string
}

// Param is one positional or named argument of a tag.
type Param struct {
	Name    string
	Kind    ParamKind
	Enum    []string
	Default string
	// Unit documents what a numeric value means ("s", "%", "px").
	Unit string
	Help string
}

// ParamKind classifies the value space of a parameter.
type ParamKind string

const (
	KindAsset   ParamKind = "asset"   // must resolve to an asset id or @ref
	KindPreset  ParamKind = "preset"  // a layout/camera preset name
	KindNumber  ParamKind = "number"  // numeric
	KindText    ParamKind = "text"    // free text, quoted when it contains ':'
	KindKeyword ParamKind = "keyword" // one of Enum
	KindColor   ParamKind = "color"   // #rrggbb
	KindCoords  ParamKind = "coords"  // x,y or x,y,w,h
	// KindElement marks an @id reference resolved at compile time.
	KindElement ParamKind = "element"
)

// Tag status values.
const (
	StatusStable = "stable"
	StatusLegacy = "legacy"
	StatusNoop   = "noop"
)

// The 21 patterns the parser has always inlined, now owned by the table. They
// are reproduced byte-for-byte: TestPatternTableMatchesLegacy proves the compiled
// regexps equal the historical ones, and the parser matches through this table.
var (
	reDraw      = regexp.MustCompile(`\[draw:([^\]@:]+)((?:[:\w-]+(?:=[^\]@:]+)?)*)(?:@([\d,]+))?\]`)
	reTargetXY  = regexp.MustCompile(`\[text:([^\]@]+)(?:@([\d,]+))?\]`)
	reErase     = regexp.MustCompile(`\[erase:([^\]@]+)(?:@([\d,]+))?\]`)
	reEraseAll  = regexp.MustCompile(`\[erase:(\*)\]`)
	reMove      = regexp.MustCompile(`\[move:([^\]@]+)(?:@([\d,]+))?\]`)
	reGen       = regexp.MustCompile(`\[gen:([^\]@]+)(?:@([\d,]+))?\]`)
	reWait      = regexp.MustCompile(`\[wait:([\d.]+)\]`)
	reClear     = regexp.MustCompile(`\[clear\]`)
	reOpenEnded = map[string]*regexp.Regexp{
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
)

// Preset names accepted by the layout engine, and the authoritative list the
// linter validates against (E0005).
var presetNames = []string{"TL", "TR", "BL", "BR", "HT", "HB", "LH", "RH", "center", "fullscreen"}

// CursorPresets returns the cursor vocabulary that is always valid. Additional
// presets may exist on disk; the linter asks the catalog about those.
func CursorPresets() []string {
	return append([]string(nil), cursorNames...)
}

// PresetNames returns the layout and camera preset vocabulary.
func PresetNames() []string {
	out := make([]string, len(presetNames))
	copy(out, presetNames)
	return out
}

// keyword sets for the enum-typed parameters (E0006).
var (
	styleNames       = []string{"whiteboard", "blackboard", "glassboard"}
	maskNames        = []string{"diagonal", "ltr", "ttb"}
	handNames        = []string{"pencil", "chalk", "eraser", "marker"}
	cursorNames      = []string{"hand", "magic_wand", "two_finger"}
	arrowStyles      = []string{"straight", "curved", "double", "dashed"}
	highlightStyles  = []string{"rect", "circle", "pulse"}
	slideTransitions = []string{"none", "fade", "pop", "slide-left", "slide-right", "slide-up", "slide-down", "fade-black", "fade-white", "wipe-left", "wipe-right", "wipe-up", "wipe-down"}
	fitModes         = []string{"fit", "fill", "stretch"}
	watermarkPos     = []string{"TL", "TR", "BL", "BR"}
	subtitleStates   = []string{"top", "bottom", "off"}
)

// commonKeys are the parameters every placement tag accepts.
var commonKeys = []Param{
	{Name: "id", Kind: KindText, Help: "addressable identity for @refs, [clear] survival and character state switching"},
	{Name: "dur", Kind: KindNumber, Unit: "s", Help: "lifetime window; without it a placed layer persists forever"},
	{Name: "exit", Kind: KindKeyword, Enum: []string{"fade", "pop", "slide"}, Help: "exit animation, emitted as a second event"},
	{Name: "layer", Kind: KindKeyword, Enum: []string{"board", "overlay"}, Default: "board", Help: "draw order and camera behaviour"},
}

// placementKeys are shared by the tags that place a visual element.
var placementKeys = []Param{
	{Name: "anchor", Kind: KindKeyword, Enum: presetNames, Help: "named box instead of absolute coordinates"},
	{Name: "size", Kind: KindNumber, Unit: "%", Help: "fraction of the canvas, resolution independent"},
	{Name: "w", Kind: KindNumber, Unit: "px"},
	{Name: "h", Kind: KindNumber, Unit: "px"},
	{Name: "fit", Kind: KindKeyword, Enum: fitModes, Default: "fit"},
	{Name: "color", Kind: KindColor, Help: "#rrggbb"},
	{Name: "keep", Kind: KindKeyword, Enum: []string{"true", "false"}, Default: "true", Help: "survive [clear]"},
	{Name: "cursor", Kind: KindKeyword, Enum: cursorNames, Default: "hand"},
	{Name: "mask", Kind: KindKeyword, Enum: maskNames},
	{Name: "hand", Kind: KindKeyword, Enum: handNames},
}

func merge(params ...[]Param) []Param {
	var out []Param
	for _, p := range params {
		out = append(out, p...)
	}
	return out
}

func openEnded(tag string, positional []Param, keys []Param, minArgs, maxArgs int, emits []string, status, notes string) ActionSpec {
	return ActionSpec{
		Tag:         tag,
		Status:      status,
		Patterns:    []*regexp.Regexp{reOpenEnded[tag]},
		QuoteAware:  true,
		Positional:  positional,
		Keys:        keys,
		MinArgs:     minArgs,
		MaxArgs:     maxArgs,
		EmitsEvents: emits,
		Notes:       notes,
	}
}

// specs is the whole grammar. Ordering matters only in that it is the order
// `check --dsl` prints and the order the parser tries its patterns in.
func specs() []ActionSpec {
	return []ActionSpec{
		{
			Tag:       "draw",
			Status:    StatusStable,
			Patterns:  []*regexp.Regexp{reDraw},
			HasCoords: true,
			Positional: []Param{
				{Name: "asset", Kind: KindAsset},
				{Name: "preset", Kind: KindPreset, Enum: presetNames},
				{Name: "duration", Kind: KindNumber, Unit: "s", Default: "2"},
			},
			Keys:        merge(commonKeys, placementKeys),
			MinArgs:     1,
			MaxArgs:     3,
			AssetArgs:   []string{"asset"},
			EmitsEvents: []string{"draw", "static"},
			Notes:       "hand-drawn reveal of an asset; persists until [clear] unless dur= is given",
		},
		{
			Tag:       "text",
			Status:    StatusStable,
			Patterns:  []*regexp.Regexp{reTargetXY},
			HasCoords: true,
			Positional: []Param{
				{Name: "content", Kind: KindText},
				{Name: "preset", Kind: KindPreset, Enum: presetNames},
				{Name: "font", Kind: KindKeyword, Enum: []string{"sans", "serif", "mono"}, Default: "sans"},
				{Name: "size", Kind: KindNumber, Unit: "px", Default: "48"},
				{Name: "weight", Kind: KindKeyword, Enum: []string{"regular", "bold"}, Default: "regular"},
			},
			Keys:        merge(commonKeys, placementKeys),
			MinArgs:     1,
			MaxArgs:     5,
			EmitsEvents: []string{"text", "static"},
			Notes:       "hand-written text; the content must be quoted",
		},
		{
			Tag:    "erase",
			Status: StatusStable,
			// One spec, two shapes. The wildcard form is listed first because the
			// compiler dispatch tests the exact match before its prefix sibling.
			Patterns:  []*regexp.Regexp{reEraseAll, reErase},
			HasCoords: true,
			Positional: []Param{
				{Name: "asset", Kind: KindAsset},
			},
			MinArgs:     1,
			MaxArgs:     1,
			AssetArgs:   []string{"asset"},
			EmitsEvents: []string{"erase", "erase_all"},
			Notes:       "removes one layer; [erase:*] removes the whole board",
		},
		{
			Tag:       "move",
			Status:    StatusStable,
			Patterns:  []*regexp.Regexp{reMove},
			HasCoords: true,
			Positional: []Param{
				{Name: "asset", Kind: KindAsset},
				{Name: "preset", Kind: KindPreset, Enum: presetNames},
			},
			MinArgs:     1,
			MaxArgs:     2,
			AssetArgs:   []string{"asset"},
			EmitsEvents: []string{"move", "static"},
		},
		{
			Tag:       "gen",
			Status:    StatusStable,
			Patterns:  []*regexp.Regexp{reGen},
			HasCoords: true,
			Positional: []Param{
				{Name: "prompt", Kind: KindText},
				{Name: "preset", Kind: KindPreset, Enum: presetNames},
			},
			MinArgs:     1,
			MaxArgs:     2,
			EmitsEvents: []string{"draw", "static"},
			Notes:       "generates the image through zen-lights",
		},
		{
			Tag:      "wait",
			Status:   StatusStable,
			Patterns: []*regexp.Regexp{reWait},
			Positional: []Param{
				{Name: "seconds", Kind: KindNumber, Unit: "s"},
			},
			MinArgs: 1, MaxArgs: 1,
			Notes: "silence; compiles to a timing gap only",
		},
		{
			Tag:      "clear",
			Status:   StatusStable,
			Patterns: []*regexp.Regexp{reClear},
			MinArgs:  0, MaxArgs: 0,
			Keys:        []Param{{Name: "scope", Kind: KindKeyword, Enum: []string{"board", "all"}, Default: "board"}},
			EmitsEvents: []string{"clear"},
			Notes:       "wipe the board",
		},
		openEnded("zoom",
			[]Param{{Name: "preset", Kind: KindPreset, Enum: append([]string{"reset"}, presetNames...)}},
			nil, 1, 1, nil, StatusStable, "camera move; unknown names silently make other events invisible"),
		openEnded("style",
			[]Param{{Name: "style", Kind: KindKeyword, Enum: styleNames}},
			nil, 1, 1, []string{"style"}, StatusStable, "board style"),
		openEnded("chapter",
			[]Param{{Name: "title", Kind: KindText}},
			nil, 1, 1, []string{"chapter"}, StatusStable, "container chapter marker"),
		openEnded("sfx",
			[]Param{{Name: "name", Kind: KindText}},
			nil, 1, 1, nil, StatusNoop, "documented no-op: nothing is mixed into the audio"),
		openEnded("subtitle",
			[]Param{{Name: "state", Kind: KindKeyword, Enum: subtitleStates}},
			nil, 1, 1, nil, StatusStable, "subtitle position switch"),
		openEnded("slide",
			[]Param{
				{Name: "asset", Kind: KindAsset},
				{Name: "preset", Kind: KindPreset, Enum: presetNames},
				{Name: "transition", Kind: KindKeyword, Enum: slideTransitions, Default: "none"},
				{Name: "fit", Kind: KindKeyword, Enum: fitModes, Default: "fit"},
			},
			merge(commonKeys, placementKeys), 1, 4, []string{"slide"}, StatusStable,
			"full-canvas image with no hand reveal"),
		openEnded("banner",
			[]Param{
				{Name: "title", Kind: KindText},
				{Name: "subtitle", Kind: KindText},
				{Name: "duration", Kind: KindNumber, Unit: "s", Default: "4"},
				{Name: "color", Kind: KindColor},
			},
			merge(commonKeys), 1, 4, []string{"banner"}, StatusStable, "lower-third panel that animates in and out"),
		openEnded("watermark",
			[]Param{
				{Name: "source", Kind: KindText},
				{Name: "position", Kind: KindKeyword, Enum: watermarkPos, Default: "TL"},
			},
			nil, 1, 2, []string{"watermark"}, StatusStable, "persistent corner badge"),
		openEnded("arrow",
			[]Param{
				{Name: "from", Kind: KindElement},
				{Name: "to", Kind: KindElement},
				{Name: "style", Kind: KindKeyword, Enum: arrowStyles, Default: "straight"},
				{Name: "duration", Kind: KindNumber, Unit: "s", Default: "1"},
			},
			merge(commonKeys), 2, 4, []string{"arrow", "arrow_static"}, StatusStable,
			"drawn connector; endpoints are presets, x,y pairs, or @element ids"),
		openEnded("highlight",
			[]Param{
				{Name: "region", Kind: KindElement},
				{Name: "style", Kind: KindKeyword, Enum: highlightStyles, Default: "rect"},
				{Name: "duration", Kind: KindNumber, Unit: "s", Default: "2"},
			},
			merge(commonKeys), 1, 3, []string{"highlight"}, StatusStable,
			"attention box or ring over a region or @element"),
		openEnded("compare",
			[]Param{
				{Name: "left", Kind: KindAsset},
				{Name: "right", Kind: KindAsset},
				{Name: "label_left", Kind: KindText},
				{Name: "label_right", Kind: KindText},
			},
			merge(commonKeys), 2, 4, []string{"compare"}, StatusStable,
			"full-screen split; both sides are assets"),
		openEnded("overlay",
			[]Param{
				{Name: "asset", Kind: KindAsset},
				{Name: "opacity", Kind: KindNumber, Default: "0.5"},
				{Name: "preset", Kind: KindPreset, Enum: presetNames},
			},
			merge(commonKeys, placementKeys), 1, 3, []string{"overlay"}, StatusLegacy,
			"low-level persistent layer; prefer [character:] and [sticker:]"),
		openEnded("transition",
			[]Param{
				{Name: "type", Kind: KindKeyword, Enum: []string{"fade-black", "fade-white", "flash", "wipe-left", "wipe-right"}},
				{Name: "duration", Kind: KindNumber, Unit: "s", Default: "0.5"},
			},
			nil, 1, 2, []string{"transition"}, StatusStable, "scene change; truncates every earlier event at its midpoint"),
		openEnded("counter",
			[]Param{
				{Name: "from", Kind: KindNumber},
				{Name: "to", Kind: KindNumber},
				{Name: "duration", Kind: KindNumber, Unit: "s", Default: "2"},
				{Name: "format", Kind: KindText, Default: "%d"},
				{Name: "preset", Kind: KindPreset, Enum: presetNames},
			},
			merge(commonKeys), 2, 5, []string{"counter"}, StatusStable, "animated number"),
	}
}

var table = specs()

// All returns the grammar table.
func All() []ActionSpec {
	out := make([]ActionSpec, len(table))
	copy(out, table)
	return out
}

// ByTag looks a spec up by its bracket vocabulary ("draw", "erase_all", "clear").
func ByTag(tag string) (ActionSpec, bool) {
	for _, s := range table {
		if s.Tag == tag {
			return s, true
		}
	}
	return ActionSpec{}, false
}

// TagNames lists every bracket vocabulary in table order.
func TagNames() []string {
	out := make([]string, 0, len(table))
	for _, s := range table {
		out = append(out, s.Tag)
	}
	return out
}

// Match reports the spec whose pattern matches raw, and the submatch indices.
func Match(raw string) (ActionSpec, []int, bool) {
	for _, s := range table {
		for _, re := range s.Patterns {
			if idx := re.FindStringSubmatchIndex(raw); idx != nil {
				return s, idx, true
			}
		}
	}
	return ActionSpec{}, nil, false
}

// Patterns returns every compiled pattern in table order, for the parity test
// and for the linter's span masking.
func Patterns() []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, s := range table {
		out = append(out, s.Patterns...)
	}
	return out
}

// findParam resolves a key= spelling against the spec's named parameters, then
// against the positional arguments (so `preset=TR` and `color=#EB4034` both work).
func (s ActionSpec) findParam(name string) (Param, bool) {
	for _, p := range s.Keys {
		if p.Name == name {
			return p, true
		}
	}
	for _, p := range s.Positional {
		if p.Name == name {
			return p, true
		}
	}
	return Param{}, false
}

// HasParam reports whether the tag understands a key= spelling at all.
func (s ActionSpec) HasParam(name string) bool {
	_, ok := s.findParam(name)
	return ok
}

// Pattern returns the i'th compiled pattern of a tag, or nil. The parser builds
// its matcher set from here, so no pattern is defined in two places.
func Pattern(tag string, i int) *regexp.Regexp {
	spec, ok := ByTag(tag)
	if !ok || i < 0 || i >= len(spec.Patterns) {
		return nil
	}
	return spec.Patterns[i]
}

// Prefix is the tag vocabulary in its action-tag spelling, e.g. "text:", "clear".
func (s ActionSpec) Prefix() string {
	if s.Tag == "clear" {
		return "clear"
	}
	return s.Tag + ":"
}

// IsNoop reports whether the tag is accepted but does nothing.
func (s ActionSpec) IsNoop() bool { return s.Status == StatusNoop }

// canonicalExamples are the spellings the coverage test asserts every spec
// accepts, in the order they are printed by `check --dsl`.
var canonicalExamples = map[string][]string{
	"draw":       {"[draw:robot]", "[draw:city:TR]", "[draw:world:LH]", "[draw:pyramids@100,200,400,300]", "[draw:shield:cursor=hand]"},
	"text":       {`[text:"Hello":center]`, `[text:"Act I: Finale":BL:sans:48:bold]`},
	"erase":      {"[erase:robot]", "[erase:*]"},
	"move":       {"[move:robot:BR]", "[move:city@400,300]"},
	"gen":        {`[gen:"a red door":TL]`},
	"wait":       {"[wait:1.5]"},
	"clear":      {"[clear]"},
	"zoom":       {"[zoom:TR]", "[zoom:reset]"},
	"style":      {"[style:blackboard]"},
	"chapter":    {`[chapter:"Intro"]`},
	"sfx":        {"[sfx:ding]"},
	"subtitle":   {"[subtitle:top]"},
	"slide":      {"[slide:pyramids]", "[slide:city:center:pop:fill]"},
	"banner":     {`[banner:"Title":"Sub"]`, `[banner:"Lower Third":"Name card":4:#FF4444]`},
	"watermark":  {"[watermark:CavemanExplains:BR]"},
	"arrow":      {"[arrow:TL:BR]", "[arrow:TL:BR:curved:2]"},
	"highlight":  {"[highlight:100,200,400,150:rect:2]", "[highlight:TL:circle:2]"},
	"compare":    {"[compare:city:pyramids]", `[compare:city:pyramids:"Modern":"Ancient"]`},
	"transition": {"[transition:fade-black:0.5]"},
	"overlay":    {"[overlay:world:0.3:fullscreen]"},
	"counter":    {"[counter:0:100:2:%d:center]"},
}

// TagExamples returns the canonical spellings of a spec, in the order `check
// --dsl` prints them.
func (s ActionSpec) TagExamples() []string {
	return append([]string(nil), canonicalExamples[s.Tag]...)
}
