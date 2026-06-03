package subtitle

import (
	"fmt"
	"strings"
	"zen-board/internal/model"
)

func GenerateASS(timings []model.WordTiming, width, height int, events []model.SubtitleEvent) string {
	var b strings.Builder

	// Header
	b.WriteString("[Script Info]\n")
	b.WriteString("ScriptType: v4.00+\n")
	b.WriteString(fmt.Sprintf("PlayResX: %d\n", width))
	b.WriteString(fmt.Sprintf("PlayResY: %d\n\n", height))

	b.WriteString("[V4+ Styles]\n")
	b.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	
	fontSize := height / 18
	if fontSize < 12 {
		fontSize = 12
	}
	marginV := height / 20
	if marginV < 5 {
		marginV = 5
	}
	
	// Default Style (Bottom-Center, Alignment = 2)
	b.WriteString(fmt.Sprintf("Style: Default,Arial,%d,&H00FFFFFF,&H000000FF,&H00000000,&H64000000,0,0,0,0,100,100,0,0,1,3,2,2,10,10,%d,1\n", fontSize, marginV))
	// Top Style (Top-Center, Alignment = 8)
	b.WriteString(fmt.Sprintf("Style: TopStyle,Arial,%d,&H00FFFFFF,&H000000FF,&H00000000,&H64000000,0,0,0,0,100,100,0,0,1,3,2,8,10,10,%d,1\n", fontSize, marginV))
	// Off Style (Invisible, alpha channel &HFFFFFFFF/&HFF)
	b.WriteString(fmt.Sprintf("Style: OffStyle,Arial,%d,&HFFFFFFFF,&HFFFFFFFF,&HFFFFFFFF,&HFFFFFFFF,0,0,0,0,100,100,0,0,1,0,0,2,10,10,%d,1\n\n", fontSize, marginV))

	b.WriteString("[Events]\n")
	b.WriteString("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")

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
		
		startTimeSec := chunk[0].Start
		startTime := formatASSTime(startTimeSec)
		endTime := formatASSTime(chunk[len(chunk)-1].End)
		
		// Determine subtitle state at the chunk start time
		state := "bottom"
		for _, ev := range events {
			if startTimeSec >= ev.Time {
				state = ev.State
			}
		}

		styleName := "Default"
		if state == "top" {
			styleName = "TopStyle"
		} else if state == "off" {
			styleName = "OffStyle"
		}

		var lineBuilder strings.Builder
		for j, w := range chunk {
			durationCentis := int((w.End - w.Start) * 100)
			lineBuilder.WriteString(fmt.Sprintf("{\\k%d}%s", durationCentis, w.Word))
			if j < len(chunk)-1 {
				lineBuilder.WriteString(" ")
			}
		}
		
		b.WriteString(fmt.Sprintf("Dialogue: 0,%s,%s,%s,,0,0,0,,%s\n", startTime, endTime, styleName, lineBuilder.String()))
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
