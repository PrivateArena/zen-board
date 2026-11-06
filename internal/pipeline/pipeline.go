// Package pipeline holds the staged pipeline the verbs compose. Each stage takes
// typed artifacts and returns typed artifacts, so `render`, `check` and `snapshot`
// can share the stages they have in common without either of them owning the
// others' ordering.
//
// Stage boundaries mirror the architecture: script -> TTS -> compile -> prepare ->
// frame sink. Nothing here knows about flags, exit codes or output formats.
package pipeline

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"zen-board/internal/builder"
	"zen-board/internal/constant"
	"zen-board/internal/diag"
	"zen-board/internal/model"
	"zen-board/internal/render"
	"zen-board/internal/script"
	"zen-board/internal/tts"
)

// Script is a loaded, parsed script.
type Script struct {
	Source   *script.Source
	Lines    []model.ScriptLine
	Findings *diag.Report
}

// LoadScript reads and parses a .zen file. It never touches the network, so it is
// the whole of what `check` needs before linting.
func LoadScript(path string) (*Script, error) {
	src, err := script.Load(path)
	if err != nil {
		return nil, fmt.Errorf("reading script: %w", err)
	}
	lines, findings := script.ParseChecked(string(src.Raw), src.Path)
	return &Script{
		Source:   src,
		Lines:    script.SplitInlineWaits(lines),
		Findings: &diag.Report{Findings: findings},
	}, nil
}

// Audio is the narration track plus the timings the timeline is built from.
type Audio struct {
	WAVPath     string
	WAV         []byte
	WordTimings []model.WordTiming
	Processed   []model.ProcessedLine
	Duration    float64
	// Elapsed is how long the synthesis stage took, reported by the render verb.
	Elapsed time.Duration
}

// Synthesize runs TTS for every line and writes the concatenated audio to a temp
// file. The caller owns the file: pass the returned cleanup to defer.
func Synthesize(conf *model.Project, lines []model.ScriptLine) (*Audio, func(), error) {
	start := time.Now()
	client := tts.NewClient(conf.TTSAddr, conf.TTSCacheDir)

	finalAudio, wordTimings, pLines, err := tts.OrchestrateTTSConcurrent(client, lines, conf.Speed, conf.Voice, conf.TTSConcurrency)
	if err != nil {
		return nil, func() {}, err
	}
	duration, err := tts.GetWAVDuration(finalAudio)
	if err != nil {
		return nil, func() {}, fmt.Errorf("getting exact WAV duration: %w", err)
	}
	path, err := writeTemp("zen-audio-*.wav", finalAudio)
	if err != nil {
		return nil, func() {}, err
	}
	return &Audio{
		WAVPath:     path,
		WAV:         finalAudio,
		WordTimings: wordTimings,
		Processed:   pLines,
		Duration:    duration,
		Elapsed:     time.Since(start),
	}, func() { os.Remove(path) }, nil
}

// ErrCacheMiss names a line the TTS cache cannot serve.
type ErrCacheMiss struct {
	Line    int
	Text    string
	Cache   string
	Servers string
}

func (e *ErrCacheMiss) Error() string {
	return fmt.Sprintf("line %d is not in the TTS cache (%s)", e.Line, truncate(e.Text, 60))
}

// SynthesizeFromCache builds the narration track using only what is already cached.
// It is the snapshot path: frame addressing must not pay for, or be slowed by, a
// synthesis round trip - and it must not silently issue one either.
func SynthesizeFromCache(conf *model.Project, lines []model.ScriptLine) (*Audio, func(), error) {
	client := tts.NewClient(conf.TTSAddr, conf.TTSCacheDir)
	type entry struct {
		res  *tts.TTSResult
		text string
	}
	entries := make([]*entry, len(lines))
	for i, line := range lines {
		if line.Text == "" {
			continue
		}
		res, ok := client.Cached(line.Text, conf.Speed, conf.Voice)
		if !ok {
			return nil, func() {}, &ErrCacheMiss{Line: i + 1, Text: line.Text, Cache: conf.TTSCacheDir, Servers: conf.TTSAddr}
		}
		entries[i] = &entry{res: res, text: line.Text}
	}

	wavParams := WAVParamsOf(nil)
	var chunks [][]byte
	var all []model.WordTiming
	var pLines []model.ProcessedLine
	current := 0.0
	for i, line := range lines {
		e := entries[i]
		if e == nil {
			wait := 0.0
			for _, action := range line.Actions {
				if strings.HasPrefix(action.Tag, constant.TAG_WAIT) {
					var v float64
					fmt.Sscanf(strings.TrimPrefix(action.Tag, constant.TAG_WAIT), "%f", &v)
					wait += v
				}
			}
			if wait > 0 {
				chunks = append(chunks, tts.CreateSilentWAV(wavParams, wait))
			}
			pLines = append(pLines, model.ProcessedLine{StartTime: current, Duration: wait, Actions: line.Actions})
			current += wait
			continue
		}
		chunks = append(chunks, e.res.Audio)
		offset := len(all)
		dur := e.res.Duration
		if d, err := tts.GetWAVDuration(e.res.Audio); err == nil {
			dur = d
		}
		timings := e.res.Timings
		if timings == nil {
			timings = tts.EstimateWordTimings(line.Text, dur, current)
		}
		shifted := make([]model.WordTiming, 0, len(timings))
		for _, tm := range timings {
			shifted = append(shifted, model.WordTiming{Word: tm.Word, Start: tm.Start + current, End: tm.End + current})
		}
		all = append(all, shifted...)
		pLines = append(pLines, model.ProcessedLine{StartTime: current, Duration: dur, WordOffset: offset, Actions: line.Actions})
		current += dur
	}
	finalAudio, err := tts.ConcatenateWAVs(chunks)
	if err != nil {
		return nil, func() {}, fmt.Errorf("concat cached audio: %w", err)
	}
	duration, err := tts.GetWAVDuration(finalAudio)
	if err != nil {
		return nil, func() {}, err
	}
	path, err := writeTemp("zen-audio-*.wav", finalAudio)
	if err != nil {
		return nil, func() {}, err
	}
	return &Audio{WAVPath: path, WAV: finalAudio, WordTimings: all, Processed: pLines, Duration: duration},
		func() { os.Remove(path) }, nil
}

// WAVParamsOf reports the parameters cached audio is written with.
func WAVParamsOf(client *tts.TTSClient) tts.WAVParams { return tts.CachedWAVParams() }

// Timeline is a compiled timeline plus the artifacts the compile produced.
type Timeline struct {
	Compilation *builder.TimelineCompilation
	TotalFrames int
}

// Compile turns narration timings into a frame-event timeline. EndFrame sentinels
// are clamped here, so every downstream stage sees real frame numbers.
func Compile(conf *model.Project, audio *Audio) (*Timeline, error) {
	comp, err := builder.CompileTimeline(conf, audio.WordTimings, audio.Processed, audio.Duration, audio.WAVPath)
	if err != nil {
		return nil, err
	}
	total := TotalFrames(conf, audio.Duration)
	for i := range comp.Timeline.Events {
		if comp.Timeline.Events[i].EndFrame >= total {
			comp.Timeline.Events[i].EndFrame = total - 1
		}
	}
	return &Timeline{Compilation: comp, TotalFrames: total}, nil
}

// TotalFrames is the frame count of the finished video, including the freeze tail.
// Frame addressing must use this number: the freeze tail is real frames.
func TotalFrames(conf *model.Project, duration float64) int {
	return int(duration*float64(conf.FPS)) + conf.FreezeFrames
}

// EngineOptions are the per-run knobs the frame engine needs.
type EngineOptions struct {
	// HandPath is the sprite used when a cursor preset is not installed. Empty
	// means the shipped default.
	HandPath string
	// HandRequired makes a missing sprite an error instead of a warning.
	HandRequired bool
	FastMode     bool
}

// Prepare builds the frame engine and loads every asset the timeline references.
func Prepare(conf *model.Project, tl *Timeline, opts EngineOptions, findings *diag.Report) (*render.Engine, error) {
	cursorRegistry := render.NewCursorRegistry(filepath.Join(conf.AssetsDir, "cursors"))
	_ = cursorRegistry.Get(constant.CURSOR_HAND) // warm the hand cache during init

	defaultCursor := conf.Cursor
	if defaultCursor == "" {
		defaultCursor = constant.CURSOR_HAND
	}
	engine, err := render.NewEngine(conf.Width, conf.Height, conf.FPS, cursorRegistry, defaultCursor)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}

	handPath := opts.HandPath
	if handPath == "" {
		handPath = DefaultHandSprite
	}
	// The hand sprite is the fallback for any cursor preset that is not installed
	// (cursor=magic_wand on a repo that only ships `hand`). It used to be loaded
	// only when -hand differed from the literal default, so the default render had
	// no fallback at all and silently drew no hand.
	if hr, handErr := render.NewHandRenderer(handPath, conf.HandTipX, conf.HandTipY); handErr == nil {
		engine.Hand = hr
	} else if opts.HandRequired {
		return nil, fmt.Errorf("hand renderer: %w", handErr)
	} else {
		log.Printf("[Cursor] hand sprite %s unavailable (%v); falling back to installed cursor presets", handPath, handErr)
	}
	engine.FastMode = opts.FastMode

	if err := builder.PrepareAssets(conf, engine, tl.Compilation.Timeline, tl.Compilation.TextJobs, tl.Compilation.GenJobs, findings); err != nil {
		return nil, err
	}
	return engine, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "..."
}

// DefaultHandSprite is the shipped hand bitmap, resolved relative to the working
// directory like every other asset path.
const DefaultHandSprite = "./assets/hand.png"

// WriteTextTemp stages a text artifact (ASS subtitles, ffmetadata) for ffmpeg.
func WriteTextTemp(pattern, data string) (string, error) {
	return writeTemp(pattern, []byte(data))
}

func writeTemp(pattern string, data []byte) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("temp %s: %w", pattern, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", fmt.Errorf("temp %s: %w", pattern, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("temp %s: %w", pattern, err)
	}
	return f.Name(), nil
}
