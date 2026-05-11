package subtitle

import (
	"fmt"
	"strings"
	"zen-board/internal/model"
)

func GenerateASS(timings []model.WordTiming) string {
	var b strings.Builder

	// Header
	b.WriteString("[Script Info]\n")
	b.WriteString("ScriptType: v4.00+\n")
	b.WriteString("PlayResX: 1920\n")
	b.WriteString("PlayResY: 1080\n\n")

	b.WriteString("[V4+ Styles]\n")
	b.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	b.WriteString("Style: Default,Arial,60,&H00FFFFFF,&H000000FF,&H00000000,&H64000000,0,0,0,0,100,100,0,0,1,3,2,2,10,10,50,1\n\n")

	b.WriteString("[Events]\n")
	b.WriteString("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")

	// We'll group words into sentences or chunks for display.
	// For simplicity, let's just show one line that highlights words.
	// A more advanced version would break into 7-10 word chunks.

	if len(timings) == 0 {
		return b.String()
	}

	// Chunking logic: ~10 words per dialogue event
	chunkSize := 10
	for i := 0; i < len(timings); i += chunkSize {
		end := i + chunkSize
		if end > len(timings) {
			end = len(timings)
		}
		chunk := timings[i:end]
		
		startTime := formatASSTime(chunk[0].Start)
		endTime := formatASSTime(chunk[len(chunk)-1].End)
		
		var lineBuilder strings.Builder
		for j, w := range chunk {
			durationCentis := int((w.End - w.Start) * 100)
			lineBuilder.WriteString(fmt.Sprintf("{\\k%d}%s", durationCentis, w.Word))
			if j < len(chunk)-1 {
				lineBuilder.WriteString(" ")
			}
		}
		
		b.WriteString(fmt.Sprintf("Dialogue: 0,%s,%s,Default,,0,0,0,,%s\n", startTime, endTime, lineBuilder.String()))
	}

	return b.String()
}

func formatASSTime(seconds float64) string {
	h := int(seconds / 3600)
	m := int(seconds/60) % 60
	s := int(seconds) % 60
	c := int(seconds*100) % 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", h, m, s, c)
}
