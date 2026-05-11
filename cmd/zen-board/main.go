package main

import (
	"encoding/json"
	"flag"
	"fmt"
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
	if strings.Contains(exe, "go-build") || strings.Contains(dir, "Temp") {
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
	conf := loadConfig()

	scriptPath := flag.String("script", "", "Path to .zen script file")
	assetsDir := flag.String("assets", conf.AssetsDir, "Directory containing image assets")
	handPath := flag.String("hand", "./assets/hand.png", "Path to hand.png sprite")
	outputPath := flag.String("o", conf.OutputPath, "Output video path")
	fps := flag.Int("fps", conf.FPS, "Frames per second")
	width := flag.Int("w", conf.Width, "Canvas width")
	height := flag.Int("h", conf.Height, "Canvas height")
	ttsAddr := flag.String("tts", conf.TTSAddr, "zen-tts server address")
	speed := flag.Float64("speed", conf.Speed, "TTS speed multiplier")
	flag.Parse()

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
		log.Fatal("Error: -script is required")
	}

	// 1. Parse Script
	scriptData, err := os.ReadFile(conf.ScriptPath)
	if err != nil {
		log.Fatalf("Error reading script: %v", err)
	}
	lines := script.Parse(string(scriptData))

	// 2. TTS & Timing
	client := tts.NewClient(conf.TTSAddr)
	var audioChunks [][]byte
	var allWordTimings []model.WordTiming
	currentTime := 0.0

	fmt.Println("Synthesizing TTS...")
	for _, line := range lines {
		if line.Text == "" {
			// Handle WAIT tags in line.Actions
			for _, action := range line.Actions {
				if strings.HasPrefix(action.Tag, "WAIT:") {
					waitStr := strings.TrimPrefix(action.Tag, "WAIT:")
					var waitVal float64
					fmt.Sscanf(waitStr, "%f", &waitVal)
					currentTime += waitVal
				}
			}
			continue
		}

		chunk, err := client.Synthesize(line.Text, *speed)
		if err != nil {
			log.Fatalf("TTS Error: %v", err)
		}
		audioChunks = append(audioChunks, chunk)
		
		duration, _ := tts.GetWAVDuration(chunk)
		wordTimings := tts.EstimateWordTimings(line.Text, duration, currentTime)
		allWordTimings = append(allWordTimings, wordTimings...)
		
		// Map DrawActions to actual timestamps based on WordIndex
		// (This will be used in Timeline building)
		
		currentTime += duration
	}

	finalAudio, err := tts.ConcatenateWAVs(audioChunks)
	if err != nil {
		log.Fatalf("WAV Concat Error: %v", err)
	}
	err = tts.SaveWAV("temp_audio.wav", finalAudio)
	if err != nil {
		log.Fatalf("WAV Save Error: %v", err)
	}
	defer os.Remove("temp_audio.wav")

	// 3. Build Timeline
	var events []model.FrameEvent
	totalFrames := int(currentTime * float64(conf.FPS))
	
	// Track which word index in allWordTimings corresponds to each line
	// This is a bit tricky due to wait actions.
	// For now, let's just find the draw actions and map them.
	
	wordOffset := 0
	lineTimeOffset := 0.0
	for _, line := range lines {
		if line.Text == "" {
			for _, action := range line.Actions {
				if strings.HasPrefix(action.Tag, "WAIT:") {
					var waitVal float64
					fmt.Sscanf(strings.TrimPrefix(action.Tag, "WAIT:"), "%f", &waitVal)
					lineTimeOffset += waitVal
				}
			}
			continue
		}
		
		lineDuration := 0.0
		// We need to know this line's duration to update lineTimeOffset
		// Actually it's easier if we just track word indices globally.
		
		for _, action := range line.Actions {
			if strings.HasPrefix(action.Tag, "WAIT:") { continue }
			
			// Find the word that triggers this action
			triggerWordIdx := wordOffset + action.WordIndex - 1
			if triggerWordIdx < 0 { triggerWordIdx = 0 }
			if triggerWordIdx >= len(allWordTimings) { triggerWordIdx = len(allWordTimings) - 1 }
			
			triggerTime := allWordTimings[triggerWordIdx].Start
			startFrame := int(triggerTime * float64(conf.FPS))
			
			// Default 2 second reveal
			revealDuration := 2.0
			endFrame := startFrame + int(revealDuration * float64(conf.FPS))
			
			events = append(events, model.FrameEvent{
				TargetImage: action.Tag,
				StartFrame:  startFrame,
				EndFrame:    endFrame,
				X:           100, // TODO: Smart positioning or from tag
				Y:           100,
			})
		}
		
		// Update offsets for next line
		wordsInLine := len(strings.Fields(line.Text))
		// Get duration of this line's chunk (we should have stored it)
		// Let's re-calculate or store it better. 
		// For now, assume it's the duration of the words.
		if wordsInLine > 0 {
			lineDuration = allWordTimings[wordOffset+wordsInLine-1].End - allWordTimings[wordOffset].Start
		}
		
		wordOffset += wordsInLine
		lineTimeOffset += lineDuration
	}

	// 4. Subtitles
	assData := subtitle.GenerateASS(allWordTimings)
	err = os.WriteFile("temp_subs.ass", []byte(assData), 0644)
	if err != nil {
		log.Fatalf("ASS Save Error: %v", err)
	}
	defer os.Remove("temp_subs.ass")

	// 5. Rendering Engine
	engine, err := render.NewEngine(conf.Width, conf.Height, conf.FPS, *handPath)
	if err != nil {
		log.Fatalf("Engine Error: %v", err)
	}

	// Load Assets
	fmt.Println("Loading assets...")
	for _, ev := range events {
		assetPath := filepath.Join(conf.AssetsDir, ev.TargetImage+".png")
		err := engine.LoadAsset(ev.TargetImage, assetPath)
		if err != nil {
			log.Printf("Warning: Could not load asset %s: %v", ev.TargetImage, err)
		}
	}

	// 6. FFmpeg Pipe
	pipe, err := ffmpeg.NewPipe(conf.OutputPath, "temp_audio.wav", "temp_subs.ass", conf.Width, conf.Height, conf.FPS)
	if err != nil {
		log.Fatalf("FFmpeg Error: %v", err)
	}

	fmt.Printf("Rendering %d frames...\n", totalFrames)
	for f := 0; f < totalFrames; f++ {
		frame := engine.RenderFrame(f, events)
		err := pipe.WriteFrame(frame.Pix)
		if err != nil {
			log.Fatalf("Pipe Write Error: %v", err)
		}
		// Return buffer to pool
		engine.Pool.BufferPool.Put(frame)
		
		if f%30 == 0 {
			fmt.Printf("\rProgress: %d/%d (%.1f%%)", f, totalFrames, float64(f)*100/float64(totalFrames))
		}
	}
	fmt.Println("\nFinishing encoding...")
	pipe.Close()

	fmt.Printf("Done! Video saved to %s\n", conf.OutputPath)
}
