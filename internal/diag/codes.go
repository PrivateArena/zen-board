package diag

// Diagnostic codes. The table is authoritative (ARCHITECTURE_PLAN_quick_wins:
// §5); every code the pipeline can emit is declared here with its default
// severity so that the DSL docs, the linter and the CLI all agree.
const (
	// E0001: referenced asset id does not resolve to a file in the index or on disk.
	E0001 = "E0001"
	// E0002: a "[prefix:...]" span was not consumed by any tag pattern and the
	// prefix is within edit distance 2 of a known tag.
	E0002 = "E0002"
	// E0003: overlapping or unclosed tag spans.
	E0003 = "E0003"
	// E0004: malformed coordinates or wrong part count.
	E0004 = "E0004"
	// E0005: unknown preset or layout anchor.
	E0005 = "E0005"
	// E0006: unknown style / mask / hand / cursor keyword.
	E0006 = "E0006"
	// E0007: ":{num}.{num}" duration the legacy integer-only path cannot match.
	E0007 = "E0007"
	// E0008: tag consumed but produced zero events.
	E0008 = "E0008"
	// E0009: arity below ActionSpec.MinArgs.
	E0009 = "E0009"
	// E0010: "@x,y" coords on a tag that has no coordinate group.
	E0010 = "E0010"
	// E0011: SVG asset placed with coords that omit w,h (rasterizer skipped).
	E0011 = "E0011"
	// E0012: unknown [zoom:] name (creates a phantom camera state).
	E0012 = "E0012"
	// E0013: duplicate id=, or an "@ref" to an id never defined or defined later.
	E0013 = "E0013"
	// E0014: directive is a documented no-op.
	E0014 = "E0014"

	// W0001: literal "[...]" in narration that is a plausible code token.
	W0001 = "W0001"
	// W0002: exit= without dur= or id=.
	W0002 = "W0002"

	// W0003: an asset could not be loaded or rasterised; the frame draws without it.
	W0003 = "W0003"
	// W0004: a trigger word index fell outside the word timings of its line.
	W0004 = "W0004"
	// W0005: an asset is flagged has_bg, so its rectangle carries a background.
	W0005 = "W0005"
	// W0006: an erase targeted something that is not on screen.
	W0006 = "W0006"
	// W0007: a placeholder stood in for a generated asset.
	W0007 = "W0007"
	// W0008: a text job failed to rasterise; the text is absent.
	W0008 = "W0008"
	// W0009: exit= was dropped because the layer it was written on never ends.
	W0009 = "W0009"
	// W0010: an exit animation was dropped because it lands past the end of the video.
	W0010 = "W0010"
	// W0011: a pixel-exact caption is larger than the canvas and will be clipped.
	W0011 = "W0011"
	// W0012: w= or h= was given without its partner, so the box fell back to the anchor.
	W0012 = "W0012"

	// I0001: snapshot truncated by -max.
	I0001 = "I0001"
)

// SeverityOf returns the default severity for a known code. Unknown codes are
// treated as errors so a new rule can never silently downgrade itself.
func SeverityOf(code string) Severity {
	switch code {
	case W0001, W0002, W0003, W0004, W0005, W0006, W0007, W0008, W0009, W0010, W0011, W0012:
		return SeverityWarning
	case I0001:
		return SeverityInfo
	default:
		return SeverityError
	}
}

// New builds a finding with the code's default severity.
func New(code, stage, message string) Finding {
	return Finding{Code: code, Severity: SeverityOf(code), Message: message, Stage: stage}
}
