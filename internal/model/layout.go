package model

import (
	"sort"

	"zen-board/internal/constant"
)

// Named layout boxes. The uppercase names are the historical presets; the lowercase
// names are anchors, which are boxes chosen to *place* a layer rather than to frame
// a region of interest. Both resolve through GetPresetLayout, and both are listed by
// LayoutPresets so the linter can validate a name against the same table the renderer
// uses instead of a hand-maintained copy.
const (
	PRESET_FULLSCREEN_CN = "C"

	ANCHOR_LEFT     = "l"
	ANCHOR_CENTRE   = "c"
	ANCHOR_RIGHT    = "r"
	ANCHOR_TOP      = "t"
	ANCHOR_BOTTOM   = "b"
	ANCHOR_TOP_C    = "tc"
	ANCHOR_BOT_C    = "bc"
	ANCHOR_TOP_L    = "tl"
	ANCHOR_TOP_R    = "tr"
	ANCHOR_BOTTOM_L = "bl"
	ANCHOR_BOTTOM_R = "br"
	// The thirds a whiteboard layout actually wants: a mascot sits bottom-right and
	// leaves the two left columns free for content.
	ANCHOR_THIRD_L = "third-l"
	ANCHOR_THIRD_C = "third-c"
	ANCHOR_THIRD_R = "third-r"
)

// GetPresetLayout resolves a named box to pixel coordinates for a canvas size. Every
// box is derived from the canvas, so the same script is sane at 1920x1080 and 1280x720.
func GetPresetLayout(preset string, canvasW, canvasH int) (x, y, w, h int) {
	halfW := canvasW / 2
	halfH := canvasH / 2
	thirdW := canvasW / 3
	switch preset {
	case "TL":
		return 0, 0, halfW, halfH
	case "TR":
		return halfW, 0, halfW, halfH
	case "BL":
		return 0, halfH, halfW, halfH
	case "BR":
		return halfW, halfH, halfW, halfH
	case "HT":
		return 0, 0, canvasW, halfH
	case "HB":
		return 0, halfH, canvasW, halfH
	case "LH":
		return 0, 0, halfW, canvasH
	case "RH":
		return halfW, 0, halfW, canvasH
	// "center" and "fullscreen" are the historical spellings of the whole-canvas box.
	// They resolve here rather than through the default branch so the linter's list and
	// the renderer's behaviour stay one vocabulary - the drift the plan calls E0005.
	case PRESET_FULLSCREEN_CN, "FULL", "center", "centre", "fullscreen", "":
		return 0, 0, canvasW, canvasH
	case ANCHOR_LEFT:
		return 0, 0, halfW, canvasH
	case ANCHOR_CENTRE:
		return canvasW / 4, canvasH / 4, canvasW / 2, canvasH / 2
	case ANCHOR_RIGHT:
		return halfW, 0, halfW, canvasH
	case ANCHOR_TOP:
		return 0, 0, canvasW, canvasH / 3
	case ANCHOR_BOTTOM:
		return 0, canvasH - canvasH/3, canvasW, canvasH / 3
	case ANCHOR_TOP_C:
		return canvasW / 3, 0, canvasW / 3, canvasH / 3
	case ANCHOR_BOT_C:
		return canvasW / 3, canvasH - canvasH/3, canvasW / 3, canvasH / 3
	case ANCHOR_TOP_L:
		return 0, 0, canvasW / 3, canvasH / 3
	case ANCHOR_TOP_R:
		return canvasW - canvasW/3, 0, canvasW / 3, canvasH / 3
	case ANCHOR_BOTTOM_L:
		return 0, canvasH - canvasH/3, canvasW / 3, canvasH / 3
	case ANCHOR_BOTTOM_R:
		return canvasW - canvasW/3, canvasH - canvasH/3, canvasW / 3, canvasH / 3
	case ANCHOR_THIRD_L:
		return 0, 0, thirdW, canvasH
	case ANCHOR_THIRD_C:
		return thirdW, 0, thirdW, canvasH
	case ANCHOR_THIRD_R:
		return canvasW - thirdW, 0, thirdW, canvasH
	default:
		// An unknown name lands here. The linter rejects it before compile
		// (E0005); this fallback keeps a hand-built timeline from producing garbage
		// coordinates, which is what the whole-canvas answer means.
		return 0, 0, canvasW, canvasH
	}
}

// layoutPresetNames is derived from the switch above, never written by hand again.
var layoutPresetNames = []string{
	"TL", "TR", "BL", "BR", "HT", "HB", "LH", "RH", "C", "FULL",
	"center", "centre", "fullscreen",
	ANCHOR_LEFT, ANCHOR_CENTRE, ANCHOR_RIGHT, ANCHOR_TOP, ANCHOR_BOTTOM,
	ANCHOR_TOP_C, ANCHOR_BOT_C,
	ANCHOR_TOP_L, ANCHOR_TOP_R, ANCHOR_BOTTOM_L, ANCHOR_BOTTOM_R,
	ANCHOR_THIRD_L, ANCHOR_THIRD_C, ANCHOR_THIRD_R,
}

// LayoutPresets returns the authoritative list of named boxes. The linter validates
// preset= and anchor= against this list, so a typo can never silently become "the
// whole canvas" again.
func LayoutPresets() []string {
	out := append([]string(nil), layoutPresetNames...)
	sort.Strings(out)
	return out
}

// IsLayoutPreset reports whether name is a box the renderer understands.
func IsLayoutPreset(name string) bool {
	if name == "" {
		return true
	}
	// "reset" is a camera instruction, not a box: accepting it here would resolve it to
	// the whole canvas, which is the silent fallback this predicate exists to prevent.
	if name == constant.FOCUS_RESET {
		return false
	}
	for _, p := range layoutPresetNames {
		if p == name {
			return true
		}
	}
	return false
}
