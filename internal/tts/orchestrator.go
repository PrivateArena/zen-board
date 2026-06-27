package tts

import (
	"fmt"
	"strings"
	"sync"
	"zen-board/internal/model"
)

type SynthJob struct {
	Index int
	Text  string
}

type SynthResult struct {
	Chunk    []byte
	Timings  []model.WordTiming
	Duration float64
	Err      error
}

// OrchestrateTTS runs parallel TTS synthesis on script lines, generates silent WAVs for wait actions,
// compiles absolute word timings, and returns the concatenated audio data and processed line boundaries.
func OrchestrateTTS(client *TTSClient, lines []model.ScriptLine, speed float64, voice string) ([]byte, []model.WordTiming, []model.ProcessedLine, error) {
	var jobs []SynthJob
	for i, line := range lines {
		if line.Text != "" {
			jobs = append(jobs, SynthJob{Index: i, Text: line.Text})
		}
	}

	results := make([]*SynthResult, len(lines))
	var wg sync.WaitGroup
	semTTS := make(chan struct{}, 1)

	fmt.Println("Synthesizing TTS in parallel...")
	for _, job := range jobs {
		wg.Add(1)
		go func(j SynthJob) {
			defer wg.Done()
			semTTS <- struct{}{}
			defer func() { <-semTTS }()

			res, err := client.SynthesizeWithTimings(j.Text, speed, voice)
			if err != nil {
				results[j.Index] = &SynthResult{Err: err}
				return
			}
			results[j.Index] = &SynthResult{
				Chunk:    res.Audio,
				Timings:  res.Timings,
				Duration: res.Duration,
			}
		}(job)
	}
	wg.Wait()

	// Check for synthesis errors and extract WAV parameters if available
	var wavParams WAVParams
	var gotParams bool
	for i, res := range results {
		if res != nil {
			if res.Err != nil {
				return nil, nil, nil, fmt.Errorf("TTS Error on line %d: %w", i+1, res.Err)
			}
			if len(res.Chunk) > 0 && !gotParams {
				params, err := ParseWAVParams(res.Chunk)
				if err == nil {
					wavParams = params
					gotParams = true
				}
			}
		}
	}

	// Default fallback WAV parameters if no TTS is synthesized in the script
	if !gotParams {
		wavParams = WAVParams{
			Channels:      1,
			SampleRate:    24000,
			BitsPerSample: 16,
		}
	}

	var audioChunks [][]byte
	var allWordTimings []model.WordTiming
	var pLines []model.ProcessedLine
	currentTime := 0.0

	for i, line := range lines {
		if line.Text == "" {
			waitDuration := 0.0
			for _, action := range line.Actions {
				if strings.HasPrefix(action.Tag, "WAIT:") {
					var waitVal float64
					fmt.Sscanf(strings.TrimPrefix(action.Tag, "WAIT:"), "%f", &waitVal)
					waitDuration += waitVal
				}
			}
			if waitDuration > 0 {
				silentChunk := CreateSilentWAV(wavParams, waitDuration)
				audioChunks = append(audioChunks, silentChunk)
			}
			pLines = append(pLines, model.ProcessedLine{
				StartTime: currentTime,
				Duration:  waitDuration,
				Actions:   line.Actions,
			})
			currentTime += waitDuration
			continue
		}

		res := results[i]
		if res == nil || len(res.Chunk) == 0 {
			return nil, nil, nil, fmt.Errorf("missing synthesized audio for line %d", i+1)
		}
		chunk := res.Chunk
		audioChunks = append(audioChunks, chunk)

		// Use exact WAV duration; fall back to pre-computed duration from SynthesizeWithTimings
		duration := res.Duration
		if wavDur, err := GetWAVDuration(chunk); err == nil {
			duration = wavDur
		}
		if duration == 0 {
			return nil, nil, nil, fmt.Errorf("zero duration for line %d", i+1)
		}

		wordOffset := len(allWordTimings)
		var wordTimings []model.WordTiming
		if res.Timings != nil {
			// Ground-truth timings from PCM analysis: shift from segment-relative to absolute
			for _, t := range res.Timings {
				wordTimings = append(wordTimings, model.WordTiming{
					Word:  t.Word,
					Start: t.Start + currentTime,
					End:   t.End + currentTime,
				})
			}
		} else {
			// Fallback: syllable-heuristic estimate
			wordTimings = EstimateWordTimings(line.Text, duration, currentTime)
		}
		allWordTimings = append(allWordTimings, wordTimings...)

		pLines = append(pLines, model.ProcessedLine{
			StartTime:  currentTime,
			Duration:   duration,
			WordOffset: wordOffset,
			Actions:    line.Actions,
		})

		currentTime += duration
	}

	finalAudio, err := ConcatenateWAVs(audioChunks)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("WAV Concat Error: %w", err)
	}

	return finalAudio, allWordTimings, pLines, nil
}
