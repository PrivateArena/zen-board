package tts

import (
	"strings"
	"zen-board/internal/model"
)

func EstimateWordTimings(text string, duration float64, startTime float64) []model.WordTiming {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	// Count total characters to distribute duration proportionally
	totalChars := 0
	for _, w := range words {
		totalChars += len(w)
	}

	// Add slight weight for spaces? Or just use word length.
	// Let's use word length for now.

	var timings []model.WordTiming
	currentStart := startTime

	for _, w := range words {
		wordDuration := (float64(len(w)) / float64(totalChars)) * duration
		timings = append(timings, model.WordTiming{
			Word:  w,
			Start: currentStart,
			End:   currentStart + wordDuration,
		})
		currentStart += wordDuration
	}

	return timings
}
