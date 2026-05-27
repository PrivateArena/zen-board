package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"zen-board/internal/ffmpeg"
	"zen-board/internal/model"
	"zen-board/internal/render"
	"zen-board/internal/script"
	"zen-board/internal/subtitle"
	"zen-board/internal/tts"
)

func getBinaryDir() string {
	exe, err := os.Executable()
	if err != nil {
		dir, _ := os.Getwd()
		return dir
	}
	dir := filepath.Dir(exe)
	if strings.Contains(exe, "go-build") || strings.HasPrefix(filepath.ToSlash(exe), filepath.ToSlash(os.TempDir())) {
		dir, _ = os.Getwd()
	}
	return dir
}

func loadConfig() *model.Project {
	p := model.NewDefaultProject()
	binDir := getBinaryDir()

	configPaths := []string{
		filepath.Join(binDir, "config.json"),
		"config.json",
	}

	for _, path := range configPaths {
		absPath, _ := filepath.Abs(path)
		if _, err := os.Stat(absPath); err == nil {
			data, err := os.ReadFile(absPath)
			if err != nil {
				continue
			}
			if err := json.Unmarshal(data, p); err == nil {
				log.Printf("[Config] Loaded from: %s", absPath)
				return p
			}
		}
	}
	return p
}

func main() {
	if err := Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func Run() error {
	conf := loadConfig()

	fs := flag.NewFlagSet("zen-board", flag.ContinueOnError)
	scriptPath := fs.String("script", "", "Path to .zen script file")
	assetsDir := fs.String("assets", conf.AssetsDir, "Directory containing image assets")
	handPath := fs.String("hand", "./assets/hand.png", "Path to hand.png sprite")
	outputPath := fs.String("o", conf.OutputPath, "Output video path")
	fps := fs.Int("fps", conf.FPS, "Frames per second")
	width := fs.Int("w", conf.Width, "Canvas width")
	height := fs.Int("h", conf.Height, "Canvas height")
	ttsAddr := fs.String("tts", conf.TTSAddr, "zen-tts server address")
	speed := fs.Float64("speed", conf.Speed, "TTS speed multiplier")
	voice := fs.String("voice", conf.Voice, "TTS voice ID")
	preview := fs.Bool("preview", false, "Preview render in real-time via ffplay")
	disableTranscript := fs.Bool("disable-transcript", conf.DisableTranscript, "Disable transcript/subtitle rendering")
	
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	// Apply final values back to project config
	conf.ScriptPath = *scriptPath
	conf.AssetsDir = *assetsDir
	conf.OutputPath = *outputPath
	conf.FPS = *fps
	conf.Width = *width
	conf.Height = *height
	conf.TTSAddr = *ttsAddr
	conf.Speed = *speed
	conf.Voice = *voice
	conf.DisableTranscript = *disableTranscript

	if conf.ScriptPath == "" {
		return fmt.Errorf("-script is required")
	}

	// 1. Parse Script
	scriptData, err := os.ReadFile(conf.ScriptPath)
	if err != nil {
		return fmt.Errorf("reading script: %w", err)
	}
	lines := script.Parse(string(scriptData))

	// 2. TTS & Timing
	client := tts.NewClient(conf.TTSAddr)
	var audioChunks [][]byte
	var allWordTimings []model.WordTiming
	currentTime := 0.0

	type processedLine struct {
		startTime  float64
		duration   float64
		wordOffset int
		actions    []model.DrawAction
	}
	var pLines []processedLine

	type synthJob struct {
		index int
		text  string
	}
	var jobs []synthJob
	for i, line := range lines {
		if line.Text != "" {
			jobs = append(jobs, synthJob{index: i, text: line.Text})
		}
	}

	type synthResult struct {
		chunk    []byte
		timings  []model.WordTiming // nil if server returned no timings
		duration float64            // exact duration from WAV; 0 if chunk is nil
		err      error
	}
	results := make([]*synthResult, len(lines))

	var wg sync.WaitGroup
	semTTS := make(chan struct{}, 5)

	fmt.Println("Synthesizing TTS in parallel...")
	for _, job := range jobs {
		wg.Add(1)
		go func(j synthJob) {
			defer wg.Done()
			semTTS <- struct{}{}
			defer func() { <-semTTS }()

			res, err := client.SynthesizeWithTimings(j.text, *speed, conf.Voice)
			if err != nil {
				results[j.index] = &synthResult{err: err}
				return
			}
			results[j.index] = &synthResult{
				chunk:    res.Audio,
				timings:  res.Timings,
				duration: res.Duration,
			}
		}(job)
	}
	wg.Wait()

	// Check for synthesis errors and extract WAV parameters if available
	var wavParams tts.WAVParams
	var gotParams bool
	for i, res := range results {
		if res != nil {
			if res.err != nil {
				return fmt.Errorf("TTS Error on line %d: %w", i+1, res.err)
			}
			if len(res.chunk) > 0 && !gotParams {
				params, err := tts.ParseWAVParams(res.chunk)
				if err == nil {
					wavParams = params
					gotParams = true
				}
			}
		}
	}

	// Default fallback WAV parameters if no TTS is synthesized in the script
	if !gotParams {
		wavParams = tts.WAVParams{
			Channels:      1,
			SampleRate:    24000,
			BitsPerSample: 16,
		}
	}

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
				silentChunk := tts.CreateSilentWAV(wavParams, waitDuration)
				audioChunks = append(audioChunks, silentChunk)

				pLines = append(pLines, processedLine{
					startTime: currentTime,
					duration:  waitDuration,
					actions:   line.Actions,
				})
				currentTime += waitDuration
			}
			continue
		}

		res := results[i]
		if res == nil || len(res.chunk) == 0 {
			return fmt.Errorf("missing synthesized audio for line %d", i+1)
		}
		chunk := res.chunk
		audioChunks = append(audioChunks, chunk)

		// Use exact WAV duration; fall back to pre-computed duration from SynthesizeWithTimings
		duration := res.duration
		if wavDur, err := tts.GetWAVDuration(chunk); err == nil {
			duration = wavDur
		}
		if duration == 0 {
			return fmt.Errorf("zero duration for line %d", i+1)
		}

		wordOffset := len(allWordTimings)
		var wordTimings []model.WordTiming
		if res.timings != nil {
			// Ground-truth timings from PCM analysis: shift from segment-relative to absolute
			for _, t := range res.timings {
				wordTimings = append(wordTimings, model.WordTiming{
					Word:  t.Word,
					Start: t.Start + currentTime,
					End:   t.End + currentTime,
				})
			}
		} else {
			// Fallback: syllable-heuristic estimate
			wordTimings = tts.EstimateWordTimings(line.Text, duration, currentTime)
		}
		allWordTimings = append(allWordTimings, wordTimings...)

		pLines = append(pLines, processedLine{
			startTime:  currentTime,
			duration:   duration,
			wordOffset: wordOffset,
			actions:    line.Actions,
		})

		currentTime += duration
	}

	finalAudio, err := tts.ConcatenateWAVs(audioChunks)
	if err != nil {
		return fmt.Errorf("WAV Concat Error: %w", err)
	}

	// Derive authoritative duration from the actual concatenated WAV, not estimated currentTime
	exactDuration, err := tts.GetWAVDuration(finalAudio)
	if err != nil {
		log.Printf("Warning: could not get exact WAV duration, using estimate: %v", err)
		exactDuration = currentTime
	}

	af, err := os.CreateTemp("", "zen-audio-*.wav")
	if err != nil {
		return fmt.Errorf("temp audio: %w", err)
	}
	af.Write(finalAudio)
	af.Close()
	audioTmp := af.Name()
	defer os.Remove(audioTmp)

	// 3. Build Timeline
	timeline := &model.Timeline{
		Words:     allWordTimings,
		AudioPath: audioTmp,
		Duration:  exactDuration,
	}

	gridIndex := 0
	marginX := int(float64(conf.Width) * 0.05)
	marginY := int(float64(conf.Height) * 0.05)
	colWidth := (conf.Width - 2*marginX) / 3
	rowHeight := (conf.Height - 2*marginY) / 2

	for _, pl := range pLines {
		for _, action := range pl.actions {
			if strings.HasPrefix(action.Tag, "WAIT:") {
				continue
			}

			// Find trigger time
			triggerTime := pl.startTime
			if action.WordIndex > 0 {
				triggerWordIdx := pl.wordOffset + action.WordIndex - 1
				if triggerWordIdx >= 0 && triggerWordIdx < len(allWordTimings) {
					triggerTime = allWordTimings[triggerWordIdx].Start
				} else {
					log.Printf("Warning: WordIndex %d OOB for line starting at %.2fs", action.WordIndex, pl.startTime)
				}
			}

			startFrame := int(triggerTime * float64(conf.FPS))
			// Default 2 second reveal
			revealDuration := 2.0
			endFrame := startFrame + int(revealDuration*float64(conf.FPS))

			if action.Tag == "clear" {
				clearFrame := startFrame
				for i := range timeline.Events {
					if timeline.Events[i].EndFrame > clearFrame {
						timeline.Events[i].EndFrame = clearFrame
					}
				}
				gridIndex = 0 // Reset grid index on clear
				continue
			}

			x, y := action.X, action.Y
			w, h := action.W, action.H

			if x == 0 && y == 0 {
				// Auto-position in a 3-column, 2-row grid
				col := gridIndex % 3
				row := (gridIndex / 3) % 2
				cellX := marginX + col*colWidth
				cellY := marginY + row*rowHeight

				if w == 0 && h == 0 {
					w = int(float64(colWidth) * 0.8)
					h = int(float64(rowHeight) * 0.8)
				}

				// Center scaled image in cell
				x = cellX + (colWidth-w)/2
				y = cellY + (rowHeight-h)/2
				gridIndex++
			}

			timeline.Events = append(timeline.Events, model.FrameEvent{
				TargetImage: action.Tag,
				StartFrame:  startFrame,
				EndFrame:    endFrame,
				X:           x,
				Y:           y,
				Width:       w,
				Height:      h,
			})
		}
	}

	// Validate Assets exist upfront
	var missingAssets []string
	seenAssets := make(map[string]bool)
	for _, ev := range timeline.Events {
		if ev.TargetImage == "clear" {
			continue
		}
		if seenAssets[ev.TargetImage] {
			continue
		}
		seenAssets[ev.TargetImage] = true
		assetPath := filepath.Join(conf.AssetsDir, ev.TargetImage+".png")
		if _, err := os.Stat(assetPath); os.IsNotExist(err) {
			missingAssets = append(missingAssets, ev.TargetImage)
		}
	}

	if len(missingAssets) > 0 {
		return fmt.Errorf("missing asset files in %s: %s (please make sure they exist as .png files)", conf.AssetsDir, strings.Join(missingAssets, ", "))
	}

	// 4. Subtitles
	var subsTmp string
	if !conf.DisableTranscript {
		assData := subtitle.GenerateASS(timeline.Words, conf.Width, conf.Height)
		sf, err := os.CreateTemp("", "zen-subs-*.ass")
		if err != nil {
			return fmt.Errorf("temp subs: %w", err)
		}
		sf.Write([]byte(assData))
		sf.Close()
		subsTmp = sf.Name()
		defer os.Remove(subsTmp)
	}

	// 5. Rendering Engine
	tipX, tipY := conf.HandTipX, conf.HandTipY
	if tipX == 30 && tipY == 20 {
		tipX = 219
		tipY = 130
	}
	engine, err := render.NewEngine(conf.Width, conf.Height, conf.FPS, *handPath, tipX, tipY)
	if err != nil {
		return fmt.Errorf("engine: %w", err)
	}

	// Load Assets
	fmt.Println("Loading assets...")
	for _, ev := range timeline.Events {
		if ev.TargetImage == "clear" {
			continue
		}
		assetPath := filepath.Join(conf.AssetsDir, ev.TargetImage+".png")
		err := engine.LoadAsset(ev.TargetImage, assetPath)
		if err != nil {
			log.Printf("Warning: Could not load asset %s: %v", ev.TargetImage, err)
		}
	}

	// 6. Pipe (FFmpeg or ffplay)
	var pipe *ffmpeg.Pipe
	if *preview {
		pipe, err = ffmpeg.NewPreviewPipe(conf.Width, conf.Height, conf.FPS, audioTmp, exactDuration)
	} else {
		pipe, err = ffmpeg.NewPipe(conf.OutputPath, audioTmp, subsTmp, conf.Width, conf.Height, conf.FPS, exactDuration)
	}
	if err != nil {
		return fmt.Errorf("pipe: %w", err)
	}

	engine.StartWorkers()
	totalFrames := int(timeline.Duration * float64(conf.FPS))

	// Limit to at most 120 frames in-flight (uncompressed RGBA in memory)
	sem := make(chan struct{}, 120)

	// Feed jobs in a goroutine
	go func() {
		for f := 0; f < totalFrames; f++ {
			sem <- struct{}{}
			engine.Pool.Jobs <- render.FrameJob{
				Index:  f,
				Events: timeline.Events,
			}
		}
		close(engine.Pool.Jobs)
	}()

	fmt.Printf("Rendering %d frames (parallel)...\n", totalFrames)

	// Collect results in order
	resultsMap := make(map[int]*image.RGBA)
	nextFrame := 0

	for nextFrame < totalFrames {
		res := <-engine.Pool.Results
		resultsMap[res.Index] = res.Frame

		// Drain as many sequential frames as possible
		for {
			frame, available := resultsMap[nextFrame]
			if !available {
				break
			}

			err := pipe.WriteFrame(frame.Pix)
			if err != nil {
				return fmt.Errorf("pipe write: %w", err)
			}

			// Clean up and return to pool
			delete(resultsMap, nextFrame)
			engine.Pool.BufferPool.Put(frame)
			<-sem // Release in-flight frame slot

			if nextFrame%30 == 0 {
				fmt.Printf("\rProgress: %d/%d (%.1f%%)", nextFrame, totalFrames, float64(nextFrame)*100/float64(totalFrames))
			}
			nextFrame++
		}
	}
	fmt.Println("\nFinishing encoding...")
	pipe.Close()

	fmt.Printf("Done! Video saved to %s\n", conf.OutputPath)
	return nil
}
