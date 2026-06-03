package script

import (
	"sort"
	"strings"
	"zen-board/internal/model"
)

// SplitInlineWaits splits any ScriptLine that contains WAIT actions into multiple lines:
// 1. Text segments before/between/after the waits.
// 2. Separate empty lines containing only the wait actions.
// This allows the timeline builder to treat wait periods as separate audio durations.
func SplitInlineWaits(lines []model.ScriptLine) []model.ScriptLine {
	var result []model.ScriptLine
	for _, line := range lines {
		if line.Text == "" {
			result = append(result, line)
			continue
		}

		// Find if there are any WAIT actions
		var waitActions []model.DrawAction
		for _, act := range line.Actions {
			if strings.HasPrefix(act.Tag, "WAIT:") {
				waitActions = append(waitActions, act)
			}
		}

		if len(waitActions) == 0 {
			result = append(result, line)
			continue
		}

		// Sort wait actions by WordIndex
		sort.Slice(waitActions, func(i, j int) bool {
			return waitActions[i].WordIndex < waitActions[j].WordIndex
		})

		words := strings.Fields(line.Text)
		lastWordIdx := 0

		for _, waitAct := range waitActions {
			splitWordIdx := waitAct.WordIndex // 1-based index after which wait occurs
			if splitWordIdx > len(words) {
				splitWordIdx = len(words)
			}

			// 1. Emit preceding text if any
			if splitWordIdx > lastWordIdx {
				partWords := words[lastWordIdx:splitWordIdx]
				partText := strings.Join(partWords, " ")
				
				// Collect actions that fall in this range
				var partActions []model.DrawAction
				for _, act := range line.Actions {
					if !strings.HasPrefix(act.Tag, "WAIT:") {
						isMatch := false
						// Note: WordIndex == 0 means triggering at line start, before any word.
						// The >=0 vs >lastWordIdx asymmetry ensures that any action exactly at the
						// boundary is included in the preceding segment rather than subsequent segments.
						if lastWordIdx == 0 {
							isMatch = (act.WordIndex >= 0 && act.WordIndex <= splitWordIdx)
						} else {
							isMatch = (act.WordIndex > lastWordIdx && act.WordIndex <= splitWordIdx)
						}
						if isMatch {
							adjusted := act
							adjusted.WordIndex = act.WordIndex - lastWordIdx
							if adjusted.WordIndex < 0 {
								adjusted.WordIndex = 0
							}
							partActions = append(partActions, adjusted)
						}
					}
				}
				result = append(result, model.ScriptLine{
					Text:    partText,
					Actions: partActions,
				})
			}

			// 2. Emit the wait action as a separate line
			result = append(result, model.ScriptLine{
				Text: "",
				Actions: []model.DrawAction{
					waitAct,
				},
			})

			lastWordIdx = splitWordIdx
		}

		// 3. Emit remaining text if any
		if lastWordIdx < len(words) {
			partWords := words[lastWordIdx:]
			partText := strings.Join(partWords, " ")

			var partActions []model.DrawAction
			for _, act := range line.Actions {
				if !strings.HasPrefix(act.Tag, "WAIT:") {
					isMatch := false
					if lastWordIdx == 0 {
						isMatch = (act.WordIndex >= 0)
					} else {
						isMatch = (act.WordIndex > lastWordIdx)
					}
					if isMatch {
						adjusted := act
						adjusted.WordIndex = act.WordIndex - lastWordIdx
						if adjusted.WordIndex < 0 {
							adjusted.WordIndex = 0
						}
						partActions = append(partActions, adjusted)
					}
				}
			}
			result = append(result, model.ScriptLine{
				Text:    partText,
				Actions: partActions,
			})
		}
	}
	return result
}
