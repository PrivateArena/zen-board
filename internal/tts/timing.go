package tts

import (
	"strings"
	"zen-board/internal/model"
)

func countSyllables(word string) int {
	word = strings.ToLower(word)
	// Remove punctuation and keep letters and single quotes
	word = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || r == '\'' {
			return r
		}
		return -1
	}, word)

	if len(word) == 0 {
		return 1
	}

	vowels := "aeiouy"
	count := 0
	isPrevVowel := false

	for _, r := range word {
		isVowel := strings.ContainsRune(vowels, r)
		if isVowel && !isPrevVowel {
			count++
		}
		isPrevVowel = isVowel
	}

	// Simple trailing e rule: "silent e" at the end of a word doesn't add a syllable
	if strings.HasSuffix(word, "e") && count > 1 {
		// Exception: words ending in "le" (like bottle, handle) do count as a syllable
		if !strings.HasSuffix(word, "le") {
			count--
		}
	}

	if count == 0 {
		count = 1
	}
	return count
}

func EstimateWordTimings(text string, duration float64, startTime float64) []model.WordTiming {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	syllableCounts := make([]int, len(words))
	totalSyllables := 0

	for i, w := range words {
		c := countSyllables(w)
		syllableCounts[i] = c
		totalSyllables += c
	}

	var timings []model.WordTiming
	currentStart := startTime

	for i, w := range words {
		var wordDuration float64
		if totalSyllables > 0 {
			wordDuration = (float64(syllableCounts[i]) / float64(totalSyllables)) * duration
		} else {
			wordDuration = duration / float64(len(words))
		}

		timings = append(timings, model.WordTiming{
			Word:  w,
			Start: currentStart,
			End:   currentStart + wordDuration,
		})
		currentStart += wordDuration
	}

	return timings
}
