package cli

import (
	"os"
	"time"

	"zen-board/internal/envelope"
	"zen-board/internal/inspect"
	"zen-board/internal/pipeline"
)

// timeline reports what the compiler decided, as data. It is the cheapest way for an
// agent to see the consequence of an edit: it needs the TTS cache for timing, but no
// encoder and no renderer.
func (a *App) timeline(opts *Options, _ []string) (*envelope.Envelope, error) {
	env := envelope.New("timeline")
	conf := opts.Project

	t0 := time.Now()
	src, err := pipeline.LoadScript(conf.ScriptPath)
	env.RecordStage("parse", durationMS(t0), len(src.Lines))
	if err != nil {
		return env, err
	}
	env.ScriptHash = src.Source.Hash()

	report := LintScript(src.Source, conf.AssetsDir, false)
	if report.Blocked() {
		env.Findings = report
		return env, envelope.Blocked(report)
	}

	t0 = time.Now()
	audio, cleanup, err := pipeline.SynthesizeFromCache(conf, src.Lines)
	if err != nil {
		return env, a.cacheMiss(err, conf)
	}
	defer cleanup()
	env.RecordStage("tts-cache", durationMS(t0), len(src.Lines))

	t0 = time.Now()
	tl, err := pipeline.Compile(conf, audio)
	if err != nil {
		return env, err
	}
	env.RecordStage("compile", durationMS(t0), len(tl.Compilation.Timeline.Events))

	doc := inspect.BuildTimeline(tl.Compilation, audio.Duration, tl.TotalFrames, conf, src.Source)
	data, err := doc.JSON()
	if err != nil {
		return env, err
	}
	if opts.OutDir != "" {
		if err := os.WriteFile(opts.OutDir, data, 0o644); err != nil {
			return env, err
		}
		env.AddArtifact(envelope.Artifact{Path: opts.OutDir, Kind: "timeline"})
		return env, nil
	}
	env.Data = doc
	return env, nil
}
