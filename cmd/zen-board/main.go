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
	if strings.Contains(exe, "go-build") || strings.Contains(exe, os.TempDir()) {
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
	preview := fs.Bool("preview", false, "Preview render in real-time via ffplay")
	
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

	fmt.Println("Synthesizing TTS...")
	for _, line := range lines {
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
				pLines = append(pLines, processedLine{
					startTime: currentTime,
					duration:  waitDuration,
				})
				currentTime += waitDuration
			}
			continue
		}

		chunk, err := client.Synthesize(line.Text, *speed)
		if err != nil {
			return fmt.Errorf("TTS Error: %w", err)
		}
		audioChunks = append(audioChunks, chunk)

		duration, _ := tts.GetWAVDuration(chunk)
		wordOffset := len(allWordTimings)
		wordTimings := tts.EstimateWordTimings(line.Text, duration, currentTime)
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
		Duration:  currentTime,
	}

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

			x, y := action.X, action.Y
			if x == 0 && y == 0 {
				x, y = 100, 100
			}

			timeline.Events = append(timeline.Events, model.FrameEvent{
				TargetImage: action.Tag,
				StartFrame:  startFrame,
				EndFrame:    endFrame,
				X:           x,
				Y:           y,
				Width:       action.W,
				Height:      action.H,
			})
		}
	}

	// 4. Subtitles
	assData := subtitle.GenerateASS(timeline.Words, conf.Width, conf.Height)
	sf, err := os.CreateTemp("", "zen-subs-*.ass")
	if err != nil {
		return fmt.Errorf("temp subs: %w", err)
	}
	sf.Write([]byte(assData))
	sf.Close()
	subsTmp := sf.Name()
	defer os.Remove(subsTmp)

	// 5. Rendering Engine
	engine, err := render.NewEngine(conf.Width, conf.Height, conf.FPS, *handPath, conf.HandTipX, conf.HandTipY)
	if err != nil {
		return fmt.Errorf("engine: %w", err)
	}

	// Load Assets
	fmt.Println("Loading assets...")
	for _, ev := range timeline.Events {
		assetPath := filepath.Join(conf.AssetsDir, ev.TargetImage+".png")
		err := engine.LoadAsset(ev.TargetImage, assetPath)
		if err != nil {
			log.Printf("Warning: Could not load asset %s: %v", ev.TargetImage, err)
		}
	}

	// 6. Pipe (FFmpeg or ffplay)
	var pipe *ffmpeg.Pipe
	if *preview {
		pipe, err = ffmpeg.NewPreviewPipe(conf.Width, conf.Height, conf.FPS)
	} else {
		pipe, err = ffmpeg.NewPipe(conf.OutputPath, audioTmp, subsTmp, conf.Width, conf.Height, conf.FPS)
	}
	if err != nil {
		return fmt.Errorf("pipe: %w", err)
	}

	engine.StartWorkers()
	totalFrames := int(timeline.Duration * float64(conf.FPS))

	// Feed jobs in a goroutine
	go func() {
		for f := 0; f < totalFrames; f++ {
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
