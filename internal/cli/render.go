package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"zen-board/internal/builder"
	"zen-board/internal/envelope"
	"zen-board/internal/ffmpeg"
	"zen-board/internal/pipeline"
	"zen-board/internal/render"
	"zen-board/internal/subtitle"
)

// render is the full pipeline: script -> TTS -> timeline -> assets -> encode.
// It is the same sequence the single-verb CLI has always run, now expressed as
// named stages so each one can be timed, skipped, or reused by another verb.
func (a *App) render(opts *Options, _ []string) (*envelope.Envelope, error) {
	env := envelope.New("render")
	conf := opts.Project

	// 1. Parse
	t0 := time.Now()
	src, err := pipeline.LoadScript(conf.ScriptPath)
	env.RecordStage("parse", durationMS(t0), len(src.Lines))
	if err != nil {
		return env, err
	}
	env.Findings.Extend(src.Findings)
	env.ScriptHash = src.Source.Hash()

	// 2. TTS and word timings
	t0 = time.Now()
	audio, cleanup, err := pipeline.Synthesize(conf, src.Lines)
	if err != nil {
		return env, err
	}
	defer cleanup()
	env.RecordStage("tts", durationMS(t0), len(src.Lines))

	// 3. Compile the timeline
	t0 = time.Now()
	tl, err := pipeline.Compile(conf, audio)
	if err != nil {
		return env, err
	}
	env.RecordStage("compile", durationMS(t0), len(tl.Compilation.Timeline.Events))
	env.Findings.Extend(tl.Compilation.Findings)

	// 4. Subtitles
	var subsTmp string
	if !conf.DisableTranscript {
		assData := subtitle.GenerateASS(tl.Compilation.Timeline.Words, conf.Width, conf.Height, tl.Compilation.SubtitleEvents)
		path, err := pipeline.WriteTextTemp("zen-subs-*.ass", assData)
		if err != nil {
			return env, err
		}
		defer removeTemp(path)
		subsTmp = path
	}

	// 5. Chapter metadata
	extendedDuration := audio.Duration + float64(conf.FreezeFrames)/float64(conf.FPS)
	var metadataTmp string
	if data, ok := chapterMetadata(tl.Compilation.Chapters, extendedDuration); ok {
		path, err := pipeline.WriteTextTemp("zen-metadata-*.txt", data)
		if err == nil {
			defer removeTemp(path)
			metadataTmp = path
		}
	}

	// 6. Engine and assets
	t0 = time.Now()
	engine, err := pipeline.Prepare(conf, tl, pipeline.EngineOptions{
		HandPath:     opts.HandPath,
		HandRequired: opts.HandExplicit,
		FastMode:     opts.Fast || opts.Preview,
	}, env.Findings)
	if err != nil {
		return env, err
	}
	env.RecordStage("prepare", durationMS(t0), len(engine.Assets))

	// 7. Encode sink
	pipe, err := a.newPipe(opts, subsTmp, metadataTmp, extendedDuration, audio.WAVPath, env)
	if err != nil {
		return env, err
	}

	// 8. Event log
	var el *render.EventLogger
	if opts.EventLogPath != "" {
		el, err = render.NewEventLogger(opts.EventLogPath)
		if err != nil {
			return env, fmt.Errorf("eventlog: %w", err)
		}
		defer el.Close()
		env.AddArtifact(envelope.Artifact{Path: opts.EventLogPath, Kind: "eventlog"})
	}

	// 9. Frames
	t0 = time.Now()
	sink := builder.NewPipeSink(pipe)
	if err := builder.RenderTimeline(conf, tl.Compilation.Timeline, engine, sink, tl.Compilation.StyleKeyframes, audio.Processed, conf.CameraEnabled, el); err != nil {
		return env, err
	}
	env.RecordStage("encode", durationMS(t0), sink.Wrote())
	env.AddArtifact(envelope.Artifact{Path: conf.OutputPath, Kind: "video"})
	fmt.Printf("Total TTS & Timing synthesis: %v\n", audio.Elapsed)
	return env, nil
}

// newPipe starts ffmpeg (or ffplay in preview mode) for the render verb.
func (a *App) newPipe(opts *Options, subsTmp, metadataTmp string, duration float64, audioPath string, env *envelope.Envelope) (*ffmpeg.Pipe, error) {
	conf := opts.Project
	var pipe *ffmpeg.Pipe
	var err error
	if opts.Preview {
		pipe, err = ffmpeg.NewPreviewPipe(conf.Width, conf.Height, conf.FPS, audioPath, conf.BGMPath, conf.BGMVolume, duration, metadataTmp)
	} else {
		pipe, err = ffmpeg.NewPipe(conf.OutputPath, audioPath, subsTmp, conf.BGMPath, conf.BGMVolume, conf.Width, conf.Height, conf.FPS, duration, metadataTmp, opts.Fast)
	}
	if err != nil {
		return nil, envelope.Env("is ffmpeg installed and on PATH?", fmt.Errorf("pipe: %w", err))
	}
	env.AddArtifact(envelope.Artifact{Path: conf.OutputPath, Kind: "video"})
	return pipe, nil
}

// chapterMetadata renders the ffmetadata chapter file ffmpeg consumes.
func chapterMetadata(chapters []builder.ChapterMarker, extendedDuration float64) (string, bool) {
	if len(chapters) == 0 {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString(";FFMETADATA1\n")
	for i, ch := range chapters {
		startMs := int64(ch.StartTime * 1000)
		endMs := int64(extendedDuration * 1000)
		if i+1 < len(chapters) {
			endMs = int64(chapters[i+1].StartTime * 1000)
		}
		sb.WriteString("[CHAPTER]\n")
		sb.WriteString("TIMEBASE=1/1000\n")
		sb.WriteString(fmt.Sprintf("START=%d\n", startMs))
		sb.WriteString(fmt.Sprintf("END=%d\n", endMs))
		sb.WriteString(fmt.Sprintf("title=%s\n", ch.Title))
	}
	return sb.String(), true
}

// removeTemp deletes a staged temp file, ignoring the error: the process is
// finishing either way, and os.TempDir is not a resource contract.
func removeTemp(path string) { _ = os.Remove(path) }

// durationMS is the envelope's stage timing unit.
func durationMS(t0 time.Time) int64 { return int64(time.Since(t0) / time.Millisecond) }
