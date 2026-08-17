// Package constant centralizes the string vocabulary shared across the
// pipeline: the scripting action tags, the frame event types, and the render
// keywords (mask styles, hand styles, cursor presets, drawing styles). Treat
// this file as the map of every scripting feature the engine understands.
package constant

// Action tag prefixes recognized by the script parser and the timeline
// compiler. Every scripting feature has exactly one tag constant here.
const (
	TAG_WAIT       = "WAIT:"
	TAG_ZOOM       = "zoom:"
	TAG_STYLE      = "style:"
	TAG_CHAPTER    = "chapter:"
	TAG_SFX        = "sfx:"
	TAG_SUBTITLE   = "subtitle:"
	TAG_TEXT       = "text:"
	TAG_ERASE      = "erase:"
	TAG_ERASE_ALL  = "erase:*"
	TAG_MOVE       = "move:"
	TAG_GEN        = "gen:"
	TAG_CLEAR      = "clear"
	TAG_SLIDE      = "slide:"
	TAG_BANNER     = "banner:"
	TAG_ARROW      = "arrow:"
	TAG_HIGHLIGHT  = "highlight:"
	TAG_COMPARE    = "compare:"
	TAG_OVERLAY    = "overlay:"
	TAG_TRANSITION = "transition:"
	TAG_COUNTER    = "counter:"
)

// FrameEvent.EventType values emitted by the timeline compiler and consumed by
// the frame engine.
const (
	EVENT_DRAW         = "draw"
	EVENT_STATIC       = "static"
	EVENT_ERASE        = "erase"
	EVENT_ERASE_ALL    = "erase_all"
	EVENT_CLEAR        = "clear"
	EVENT_MOVE         = "move"
	EVENT_TEXT         = "text"
	EVENT_GEN          = "gen"
	EVENT_SLIDE        = "slide"
	EVENT_BANNER       = "banner"
	EVENT_ARROW        = "arrow"
	EVENT_ARROW_STATIC = "arrow_static"
	EVENT_HIGHLIGHT    = "highlight"
	EVENT_COMPARE      = "compare"
	EVENT_COUNTER      = "counter"
	EVENT_TRANSITION   = "transition"
	EVENT_OVERLAY      = "overlay"
	EVENT_STYLE        = "style"
	EVENT_CHAPTER      = "chapter"
)

// FrameEvent.MaskStyle values.
const (
	MASK_DIAGONAL = "diagonal"
	MASK_LTR      = "ltr"
	MASK_TTB      = "ttb"
)

// FrameEvent.HandStyle values.
const (
	HAND_PENCIL = "pencil"
	HAND_MARKER = "marker"
	HAND_ERASER = "eraser"
	HAND_CHALK  = "chalk"
)

// Cursor preset names.
const (
	CURSOR_HAND = "hand"
)

// FRAME_FOREVER is the sentinel EndFrame for events that persist until the end
// of the video; it is clamped to the render duration at render time.
const FRAME_FOREVER = 999999

// Drawing style names (switch via style: and style:blackboard).
const (
	STYLE_WHITEBOARD = "whiteboard"
	STYLE_BLACKBOARD = "blackboard"
	STYLE_GLASSBOARD = "glassboard"
)

// Camera focus and layout preset names.
const (
	FOCUS_RESET       = "reset"
	PRESET_CENTER     = "center"
	PRESET_FULLSCREEN = "fullscreen"
)

// Slide transition and fit-mode defaults.
const (
	TRANSITION_NONE = "none"
	FIT_FIT         = "fit"
)

// Font and overlay keywords.
const (
	FONT_SANS      = "sans"
	HIGHLIGHT_RECT = "rect"
	ARROW_STRAIGHT = "straight"
)
