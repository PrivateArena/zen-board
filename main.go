package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"zen-board/internal/assets"
	"zen-board/internal/builder"
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

	if len(os.Args) > 1 && os.Args[1] == "assets" {
		return assets.RunCLI(os.Args[2:], conf.AssetsDir)
	}

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
	lines = script.SplitInlineWaits(lines)

	// 2. TTS & Timing
	client := tts.NewClient(conf.TTSAddr)
	finalAudio, allWordTimings, pLines, err := tts.OrchestrateTTS(client, lines, *speed, conf.Voice)
	if err != nil {
		return err
	}

	// Derive authoritative duration from the actual concatenated WAV
	exactDuration, err := tts.GetWAVDuration(finalAudio)
	if err != nil {
		return fmt.Errorf("getting exact WAV duration: %w", err)
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
	comp, err := builder.CompileTimeline(conf, allWordTimings, pLines, exactDuration, audioTmp)
	if err != nil {
		return err
	}

	// 4. Subtitles
	var subsTmp string
	if !conf.DisableTranscript {
		assData := subtitle.GenerateASS(comp.Timeline.Words, conf.Width, conf.Height, comp.SubtitleEvents)
		sf, err := os.CreateTemp("", "zen-subs-*.ass")
		if err != nil {
			return fmt.Errorf("temp subs: %w", err)
		}
		sf.Write([]byte(assData))
		sf.Close()
		subsTmp = sf.Name()
		defer os.Remove(subsTmp)
	}

	// 5. Metadata (Chapters)
	extendedDuration := exactDuration + float64(conf.FreezeFrames)/float64(conf.FPS)
	var metadataTmp string
	if len(comp.Chapters) > 0 {
		var sb strings.Builder
		sb.WriteString(";FFMETADATA1\n")
		for i, ch := range comp.Chapters {
			startMs := int64(ch.StartTime * 1000)
			endMs := int64(extendedDuration * 1000)
			if i+1 < len(comp.Chapters) {
				endMs = int64(comp.Chapters[i+1].StartTime * 1000)
			}
			sb.WriteString("[CHAPTER]\n")
			sb.WriteString("TIMEBASE=1/1000\n")
			sb.WriteString(fmt.Sprintf("START=%d\n", startMs))
			sb.WriteString(fmt.Sprintf("END=%d\n", endMs))
			sb.WriteString(fmt.Sprintf("title=%s\n", ch.Title))
		}
		mf, err := os.CreateTemp("", "zen-metadata-*.txt")
		if err == nil {
			mf.Write([]byte(sb.String()))
			mf.Close()
			metadataTmp = mf.Name()
			defer os.Remove(metadataTmp)
		}
	}

	// 6. Preparing Engine & Assets
	engine, err := render.NewEngine(conf.Width, conf.Height, conf.FPS, *handPath, conf.HandTipX, conf.HandTipY)
	if err != nil {
		return fmt.Errorf("engine: %w", err)
	}

	err = builder.PrepareAssets(conf, engine, comp.Timeline, comp.TextJobs, comp.GenJobs)
	if err != nil {
		return err
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

	// 8. Render & encode
	return builder.RenderTimeline(conf, comp.Timeline, engine, pipe, comp.StyleKeyframes, pLines, *cameraEnabled)
}
