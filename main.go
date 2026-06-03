package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	bgmPath := fs.String("bgm", conf.BGMPath, "Path to background music file")
	bgmVolume := fs.Float64("bgm-vol", conf.BGMVolume, "Background music volume multiplier")
	cameraEnabled := fs.Bool("camera", conf.CameraEnabled, "Enable camera zoom effects")
	freezeFrames := fs.Int("freeze", conf.FreezeFrames, "Number of freeze frames at the end of the video")
	
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
	conf.BGMPath = *bgmPath
	conf.BGMVolume = *bgmVolume
	conf.CameraEnabled = *cameraEnabled
	conf.FreezeFrames = *freezeFrames

	if conf.ScriptPath == "" {
		return fmt.Errorf("-script is required")
	}

	// 1. Parse Script
	scriptData, err := os.ReadFile(conf.ScriptPath)
	if err != nil {
		return fmt.Errorf("reading script: %w", err)
	}
	lines := script.Parse(string(scriptData))
	lines = splitLinesWithInlineWaits(lines)

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

	type textRenderJob struct {
		AssetID    string
		Content    string
		FontFamily string
		FontSize   float64
		IsBold     bool
		Style      string
	}
	var textJobs []textRenderJob
	textAssetCount := 0

	type genRenderJob struct {
		AssetID string
		Prompt  string
	}
	var genJobs []genRenderJob
	genAssetCount := 0

	type styleKeyframe struct {
		frame int
		style string
	}
	var styleKeyframes []styleKeyframe

	type chapterMarker struct {
		startTime float64
		title     string
	}
	var chapters []chapterMarker

	var subtitleEvents []model.SubtitleEvent

	currentStyle := "whiteboard"

	gridIndex := 0
	marginX := int(float64(conf.Width) * 0.05)
	marginY := int(float64(conf.Height) * 0.05)
	colWidth := (conf.Width - 2*marginX) / 3
	rowHeight := (conf.Height - 2*marginY) / 2

	for _, pl := range pLines {
		for _, action := range pl.actions {
			// Find trigger time
			triggerTime := pl.startTime
			if action.WordIndex > 0 {
				triggerWordIdx := pl.wordOffset + action.WordIndex - 1
				if triggerWordIdx >= 0 && triggerWordIdx < len(allWordTimings) {
					if action.TriggerAfterWord {
						triggerTime = allWordTimings[triggerWordIdx].End
					} else {
						triggerTime = allWordTimings[triggerWordIdx].Start
					}
				} else {
					log.Printf("Warning: WordIndex %d OOB for line starting at %.2fs", action.WordIndex, pl.startTime)
				}
			}

			startFrame := int(triggerTime * float64(conf.FPS))
			
			// Handle custom duration parameters or defaults
			revealDuration := 2.0
			actionTag := action.Tag
			preset := ""
			
			isSpecialPrefix := false
			specialPrefixes := []string{"WAIT:", "zoom:", "style:", "chapter:", "sfx:", "subtitle:", "text:", "erase:", "move:", "gen:"}
			for _, prefix := range specialPrefixes {
				if strings.HasPrefix(actionTag, prefix) {
					isSpecialPrefix = true
					break
				}
			}
			
			if !isSpecialPrefix {
				parts := strings.Split(actionTag, ":")
				if len(parts) > 1 {
					if dur, err := strconv.ParseFloat(parts[len(parts)-1], 64); err == nil {
						revealDuration = dur
						parts = parts[:len(parts)-1]
					}
				}
				if len(parts) > 0 {
					actionTag = parts[0]
				}
				if len(parts) > 1 {
					preset = parts[1]
				}
			}

			endFrame := startFrame + int(revealDuration*float64(conf.FPS))

			if strings.HasPrefix(action.Tag, "WAIT:") {
				continue
			}

			if strings.HasPrefix(action.Tag, "zoom:") {
				continue
			}

			if strings.HasPrefix(action.Tag, "style:") {
				styleName := strings.TrimPrefix(action.Tag, "style:")
				currentStyle = styleName
				styleKeyframes = append(styleKeyframes, styleKeyframe{
					frame: startFrame,
					style: styleName,
				})
				continue
			}

			if strings.HasPrefix(action.Tag, "chapter:") {
				title := strings.TrimPrefix(action.Tag, "chapter:")
				title = strings.Trim(title, "\"")
				chapters = append(chapters, chapterMarker{
					startTime: triggerTime,
					title:     title,
				})
				continue
			}

			if strings.HasPrefix(action.Tag, "subtitle:") {
				state := strings.TrimPrefix(action.Tag, "subtitle:")
				subtitleEvents = append(subtitleEvents, model.SubtitleEvent{
					Time:  triggerTime,
					State: state,
				})
				continue
			}

			if strings.HasPrefix(action.Tag, "sfx:") {
				continue
			}

			if strings.HasPrefix(action.Tag, "text:") {
				rest := strings.TrimPrefix(action.Tag, "text:")
				firstQuote := strings.Index(rest, "\"")
				lastQuote := strings.LastIndex(rest, "\"")
				if firstQuote != -1 && lastQuote != -1 && lastQuote > firstQuote {
					content := rest[firstQuote+1 : lastQuote]
					remainder := rest[lastQuote+1:]
					
					preset := ""
					fontFamily := "sans"
					fontSize := 48.0
					fontWeight := "regular"
					
					parts := strings.Split(remainder, ":")
					if len(parts) > 1 {
						preset = parts[1]
					}
					if len(parts) > 2 {
						fontFamily = parts[2]
					}
					if len(parts) > 3 {
						if sz, err := strconv.ParseFloat(parts[3], 64); err == nil {
							fontSize = sz
						}
					}
					if len(parts) > 4 {
						fontWeight = parts[4]
					}

					textAssetID := fmt.Sprintf("__text_%d", textAssetCount)
					textAssetCount++

					textJobs = append(textJobs, textRenderJob{
						AssetID:    textAssetID,
						Content:    content,
						FontFamily: fontFamily,
						FontSize:   fontSize,
						IsBold:     fontWeight == "bold",
						Style:      currentStyle,
					})

					tx, ty, tw, th := action.X, action.Y, action.W, action.H
					if preset != "" && tx == 0 && ty == 0 && tw == 0 && th == 0 {
						px, py, pw, ph := getPresetLayout(preset, conf.Width, conf.Height)
						padW := int(float64(pw) * 0.1)
						padH := int(float64(ph) * 0.1)
						tx = px + padW
						ty = py + padH
						tw = pw - 2*padW
						th = ph - 2*padH
					}

					event := model.FrameEvent{
						TargetImage: textAssetID,
						StartFrame:  startFrame,
						EndFrame:    endFrame,
						X:           tx,
						Y:           ty,
						Width:       tw,
						Height:      th,
						EventType:   "text",
						MaskStyle:   "ltr",    // LTR for text sweeps
						HandStyle:   "marker", // marker cursor
					}
					timeline.Events = append(timeline.Events, event)
				}
				continue
			}

			if action.Tag == "erase:*" {
				clearFrame := startFrame
				for i := range timeline.Events {
					if timeline.Events[i].EndFrame > clearFrame {
						timeline.Events[i].EndFrame = clearFrame
					}
				}
				gridIndex = 0
				continue
			}

			if strings.HasPrefix(action.Tag, "erase:") {
				targetAsset := strings.TrimPrefix(action.Tag, "erase:")
				eraseEvent := model.FrameEvent{
					TargetImage: targetAsset,
					StartFrame:  startFrame,
					EndFrame:    endFrame,
					EventType:   "erase",
					HandStyle:   "eraser",
					MaskStyle:   "ttb",
				}
				
				for i := len(timeline.Events) - 1; i >= 0; i-- {
					if timeline.Events[i].TargetImage == targetAsset && timeline.Events[i].EventType == "draw" {
						eraseEvent.X = timeline.Events[i].X
						eraseEvent.Y = timeline.Events[i].Y
						eraseEvent.Width = timeline.Events[i].Width
						eraseEvent.Height = timeline.Events[i].Height
						if timeline.Events[i].EndFrame > startFrame {
							timeline.Events[i].EndFrame = startFrame
						}
						break
					}
				}
				timeline.Events = append(timeline.Events, eraseEvent)
				continue
			}

			if strings.HasPrefix(action.Tag, "move:") {
				parts := strings.Split(strings.TrimPrefix(action.Tag, "move:"), ":")
				targetAsset := parts[0]
				destPreset := ""
				if len(parts) > 1 {
					destPreset = parts[1]
				}

				var startX, startY int
				var startW, startH int
				found := false
				for i := len(timeline.Events) - 1; i >= 0; i-- {
					if timeline.Events[i].TargetImage == targetAsset {
						if timeline.Events[i].EventType == "move" {
							startX = timeline.Events[i].DestX
							startY = timeline.Events[i].DestY
						} else {
							startX = timeline.Events[i].X
							startY = timeline.Events[i].Y
						}
						startW = timeline.Events[i].Width
						startH = timeline.Events[i].Height
						found = true
						if timeline.Events[i].EndFrame > startFrame {
							timeline.Events[i].EndFrame = startFrame
						}
						break
					}
				}

				if found {
					destX, destY := startX, startY
					if destPreset != "" {
						px, py, pw, ph := getPresetLayout(destPreset, conf.Width, conf.Height)
						padW := int(float64(pw) * 0.1)
						padH := int(float64(ph) * 0.1)
						destX = px + padW
						destY = py + padH
						startW = pw - 2*padW
						startH = ph - 2*padH
					} else if action.X != 0 || action.Y != 0 {
						destX = action.X
						destY = action.Y
						if action.W != 0 && action.H != 0 {
							startW = action.W
							startH = action.H
						}
					}

					moveEvent := model.FrameEvent{
						TargetImage: targetAsset,
						StartFrame:  startFrame,
						EndFrame:    endFrame,
						EventType:   "move",
						X:           startX,
						Y:           startY,
						Width:       startW,
						Height:      startH,
						DestX:       destX,
						DestY:       destY,
						HandStyle:   "pencil",
					}
					timeline.Events = append(timeline.Events, moveEvent)

					staticDrawEvent := model.FrameEvent{
						TargetImage: targetAsset,
						StartFrame:  endFrame,
						EndFrame:    999999,
						X:           destX,
						Y:           destY,
						Width:       startW,
						Height:      startH,
						EventType:   "draw",
					}
					timeline.Events = append(timeline.Events, staticDrawEvent)
				}
				continue
			}

			if strings.HasPrefix(action.Tag, "gen:") {
				rest := strings.TrimPrefix(action.Tag, "gen:")
				parts := strings.Split(rest, ":")
				prompt := parts[0]
				preset := ""
				if len(parts) > 1 {
					preset = parts[1]
				}

				genAssetID := fmt.Sprintf("__gen_%d", genAssetCount)
				genAssetCount++

				genJobs = append(genJobs, genRenderJob{
					AssetID: genAssetID,
					Prompt:  prompt,
				})

				tx, ty, tw, th := action.X, action.Y, action.W, action.H
				if preset != "" && tx == 0 && ty == 0 && tw == 0 && th == 0 {
					px, py, pw, ph := getPresetLayout(preset, conf.Width, conf.Height)
					padW := int(float64(pw) * 0.1)
					padH := int(float64(ph) * 0.1)
					tx = px + padW
					ty = py + padH
					tw = pw - 2*padW
					th = ph - 2*padH
				}

				event := model.FrameEvent{
					TargetImage: genAssetID,
					StartFrame:  startFrame,
					EndFrame:    endFrame,
					X:           tx,
					Y:           ty,
					Width:       tw,
					Height:      th,
					EventType:   "draw",
					MaskStyle:   "diagonal",
					HandStyle:   "pencil",
				}
				timeline.Events = append(timeline.Events, event)
				continue
			}

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

			if preset != "" && x == 0 && y == 0 && w == 0 && h == 0 {
				px, py, pw, ph := getPresetLayout(preset, conf.Width, conf.Height)
				padW := int(float64(pw) * 0.1)
				padH := int(float64(ph) * 0.1)
				w = pw - 2*padW
				h = ph - 2*padH
				x = px + padW
				y = py + padH
			} else if x == 0 && y == 0 {
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
				TargetImage: actionTag,
				StartFrame:  startFrame,
				EndFrame:    endFrame,
				X:           x,
				Y:           y,
				Width:       w,
				Height:      h,
				EventType:   "draw",
				MaskStyle:   "ttb",
				HandStyle:   "pencil",
			})
		}
	}

	// Validate Assets exist upfront
	var missingAssets []string
	seenAssets := make(map[string]bool)
	for _, ev := range timeline.Events {
		if ev.TargetImage == "clear" || strings.HasPrefix(ev.TargetImage, "__text_") || strings.HasPrefix(ev.TargetImage, "__gen_") {
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
		assData := subtitle.GenerateASS(timeline.Words, conf.Width, conf.Height, subtitleEvents)
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

	// Load standard assets
	fmt.Println("Loading assets...")
	seenAssets = make(map[string]bool)
	for _, ev := range timeline.Events {
		if ev.TargetImage == "clear" || strings.HasPrefix(ev.TargetImage, "__text_") || strings.HasPrefix(ev.TargetImage, "__gen_") {
			continue
		}
		if seenAssets[ev.TargetImage] {
			continue
		}
		seenAssets[ev.TargetImage] = true
		assetPath := filepath.Join(conf.AssetsDir, ev.TargetImage+".png")
		err := engine.LoadAsset(ev.TargetImage, assetPath)
		if err != nil {
			log.Printf("Warning: Could not load asset %s: %v", ev.TargetImage, err)
		}
	}

	// Render and load all text assets
	for _, job := range textJobs {
		textColor := color.RGBA{0, 0, 0, 255}
		if job.Style == "blackboard" {
			textColor = color.RGBA{255, 255, 255, 255}
		}
		img, err := render.RenderText(job.Content, job.FontFamily, job.FontSize, job.IsBold, textColor)
		if err != nil {
			log.Printf("Warning: failed to render text %q: %v", job.Content, err)
			continue
		}
		engine.RegisterAsset(job.AssetID, img)
	}

	// Generate and load all neural paint assets
	for _, job := range genJobs {
		fmt.Printf("Generating paint asset for prompt %q...\n", job.Prompt)
		img, err := GeneratePaintAsset(job.Prompt)
		if err != nil {
			log.Printf("Warning: Paint generation failed for %q: %v. Using fallback.", job.Prompt, err)
			fallbackPath := filepath.Join(conf.AssetsDir, "robot.png")
			if f, err := os.Open(fallbackPath); err == nil {
				if fallbackImg, _, err := image.Decode(f); err == nil {
					engine.RegisterAsset(job.AssetID, fallbackImg)
				}
				f.Close()
			}
		} else {
			engine.RegisterAsset(job.AssetID, img)
		}
	}

	// 6. Metadata (Chapters)
	extendedDuration := exactDuration + float64(conf.FreezeFrames)/float64(conf.FPS)
	var metadataTmp string
	if len(chapters) > 0 {
		var sb strings.Builder
		sb.WriteString(";FFMETADATA1\n")
		for i, ch := range chapters {
			startMs := int64(ch.startTime * 1000)
			endMs := int64(extendedDuration * 1000)
			if i+1 < len(chapters) {
				endMs = int64(chapters[i+1].startTime * 1000)
			}
			sb.WriteString("[CHAPTER]\n")
			sb.WriteString("TIMEBASE=1/1000\n")
			sb.WriteString(fmt.Sprintf("START=%d\n", startMs))
			sb.WriteString(fmt.Sprintf("END=%d\n", endMs))
			sb.WriteString(fmt.Sprintf("title=%s\n", ch.title))
		}
		mf, err := os.CreateTemp("", "zen-metadata-*.txt")
		if err == nil {
			mf.Write([]byte(sb.String()))
			mf.Close()
			metadataTmp = mf.Name()
			defer os.Remove(metadataTmp)
		}
	}

	// 7. Pipe (FFmpeg or ffplay)
	var pipe *ffmpeg.Pipe
	if *preview {
		pipe, err = ffmpeg.NewPreviewPipe(conf.Width, conf.Height, conf.FPS, audioTmp, conf.BGMPath, conf.BGMVolume, extendedDuration, metadataTmp)
	} else {
		pipe, err = ffmpeg.NewPipe(conf.OutputPath, audioTmp, subsTmp, conf.BGMPath, conf.BGMVolume, conf.Width, conf.Height, conf.FPS, extendedDuration, metadataTmp)
	}
	if err != nil {
		return fmt.Errorf("pipe: %w", err)
	}

	totalFrames := int(timeline.Duration*float64(conf.FPS)) + conf.FreezeFrames

	// Generate Camera States
	type zoomKeyframe struct {
		frame  int
		target string
	}
	var zoomKeyframes []zoomKeyframe
	for _, pl := range pLines {
		for _, action := range pl.actions {
			if strings.HasPrefix(action.Tag, "zoom:") {
				triggerTime := pl.startTime
				if action.WordIndex > 0 {
					triggerWordIdx := pl.wordOffset + action.WordIndex - 1
					if triggerWordIdx >= 0 && triggerWordIdx < len(allWordTimings) {
						triggerTime = allWordTimings[triggerWordIdx].Start
					}
				}
				frame := int(triggerTime * float64(conf.FPS))
				target := strings.TrimPrefix(action.Tag, "zoom:")
				zoomKeyframes = append(zoomKeyframes, zoomKeyframe{
					frame:  frame,
					target: target,
				})
			}
		}
	}

	sort.Slice(zoomKeyframes, func(i, j int) bool {
		return zoomKeyframes[i].frame < zoomKeyframes[j].frame
	})

	cameraStates := make([]render.CameraState, totalFrames)
	defaultCam := render.GetPresetViewport("reset", conf.Width, conf.Height)

	if !*cameraEnabled {
		for f := 0; f < totalFrames; f++ {
			cameraStates[f] = defaultCam
		}
	} else {
		currentCam := defaultCam
		targetCam := defaultCam
		startTransitionFrame := -1
		transitionDuration := int(1.0 * float64(conf.FPS)) // 1 second transition
		transitionStartCam := defaultCam
		nextZoomIdx := 0

		for f := 0; f < totalFrames; f++ {
			if nextZoomIdx < len(zoomKeyframes) && f >= zoomKeyframes[nextZoomIdx].frame {
				preset := zoomKeyframes[nextZoomIdx].target
				targetCam = render.GetPresetViewport(preset, conf.Width, conf.Height)
				transitionStartCam = currentCam
				startTransitionFrame = f
				nextZoomIdx++
			}

			if startTransitionFrame != -1 && f < startTransitionFrame+transitionDuration {
				t := float64(f-startTransitionFrame) / float64(transitionDuration)
				currentCam = render.LerpCamera(transitionStartCam, targetCam, t)
			} else {
				currentCam = targetCam
			}
			cameraStates[f] = currentCam
		}
	}

	styleStates := make([]string, totalFrames)
	currentStyleState := "whiteboard"
	sort.Slice(styleKeyframes, func(i, j int) bool {
		return styleKeyframes[i].frame < styleKeyframes[j].frame
	})
	nextStyleIdx := 0
	for f := 0; f < totalFrames; f++ {
		if nextStyleIdx < len(styleKeyframes) && f >= styleKeyframes[nextStyleIdx].frame {
			currentStyleState = styleKeyframes[nextStyleIdx].style
			nextStyleIdx++
		}
		styleStates[f] = currentStyleState
	}

	engine.StartWorkers()

	// Limit to at most 120 frames in-flight (uncompressed RGBA in memory)
	sem := make(chan struct{}, 120)

	// Feed jobs in a goroutine
	go func() {
		for f := 0; f < totalFrames; f++ {
			sem <- struct{}{}
			engine.Pool.Jobs <- render.FrameJob{
				Index:  f,
				Events: timeline.Events,
				Cam:    cameraStates[f],
				Style:  styleStates[f],
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

func getPresetLayout(preset string, canvasW, canvasH int) (x, y, w, h int) {
	halfW := canvasW / 2
	halfH := canvasH / 2
	switch preset {
	case "TL":
		return 0, 0, halfW, halfH
	case "TR":
		return halfW, 0, halfW, halfH
	case "BL":
		return 0, halfH, halfW, halfH
	case "BR":
		return halfW, halfH, halfW, halfH
	case "HT":
		return 0, 0, canvasW, halfH
	case "HB":
		return 0, halfH, canvasW, halfH
	case "LH":
		return 0, 0, halfW, canvasH
	case "RH":
		return halfW, 0, halfW, canvasH
	default:
		return 0, 0, canvasW, canvasH
	}
}

func splitLinesWithInlineWaits(lines []model.ScriptLine) []model.ScriptLine {
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

type PaintGenRequest struct {
	Prompt string `json:"prompt"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Steps  int    `json:"steps,omitempty"`
}

type PaintGenResponse struct {
	Path string `json:"path"`
}

func GeneratePaintAsset(prompt string) (image.Image, error) {
	reqBody, err := json.Marshal(PaintGenRequest{
		Prompt: prompt,
		Width:  512,
		Height: 512,
		Steps:  4,
	})
	if err != nil {
		return nil, err
	}

	resp, err := http.Post("http://localhost:8765/paint/generate", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var genResp PaintGenResponse
	if err := json.Unmarshal(body, &genResp); err != nil {
		return nil, err
	}

	f, err := os.Open(genResp.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	return img, err
}
