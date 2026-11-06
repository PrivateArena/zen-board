package cli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"zen-board/internal/model"

	"zen-board/internal/builder"
	"zen-board/internal/diag"
	"zen-board/internal/envelope"
	"zen-board/internal/inspect"
	"zen-board/internal/pipeline"
)

// snapshot writes PNGs of selected frames. It is the eyes of the loop: an agent can
// see what the screen looks like without paying for an encode.
//
// Three properties make it usable as a feedback signal, and each is enforced here:
//   - it copies the encoder's pixels exactly (the same RenderFrame path, no
//     nearest-neighbour preview mode), so a PNG is what the video will contain;
//   - it never spawns ffmpeg;
//   - it never issues a TTS request: frame numbering comes from the real narration
//     timings, which must therefore already be cached.
func (a *App) snapshot(opts *Options, _ []string) (*envelope.Envelope, error) {
	env := envelope.New("snapshot")
	conf := opts.Project

	// -fast halves the canvas before compile, which moves every absolute @x,y in the
	// script. A frame addressed under it would not be the frame that gets encoded.
	if opts.Fast {
		return env, envelope.Usage("-fast is refused by snapshot: it rescales the canvas before compile, so absolute @x,y coordinates would not match the real render")
	}

	// 1. Gate first: a script that does not lint has no frames worth looking at.
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

	// 2. Narration timings, from the cache only.
	t0 = time.Now()
	audio, cleanup, err := pipeline.SynthesizeFromCache(conf, src.Lines)
	if err != nil {
		return env, a.cacheMiss(err, conf)
	}
	defer cleanup()
	env.RecordStage("tts-cache", durationMS(t0), len(src.Lines))

	// 3. Compile and prepare, exactly as the encode would.
	t0 = time.Now()
	tl, err := pipeline.Compile(conf, audio)
	if err != nil {
		return env, err
	}
	env.Findings.Extend(tl.Compilation.Findings)
	env.RecordStage("compile", durationMS(t0), len(tl.Compilation.Timeline.Events))

	engine, err := pipeline.Prepare(conf, tl, pipeline.EngineOptions{
		HandPath: opts.HandPath,
	}, env.Findings)
	if err != nil {
		return env, err
	}

	// 4. Resolve the frame plan before any pixel work, and cap it there.
	selector, err := inspect.SelectorOf(opts.Events)
	if err != nil {
		return env, envelope.Usage("%v", err)
	}
	sheetFmt, err := inspect.SheetFormatOf(opts.Sheet)
	if err != nil {
		return env, envelope.Usage("%v", err)
	}
	at, err := splitList(opts.At)
	if err != nil {
		return env, envelope.Usage("%v", err)
	}
	every, err := parseInterval(opts.Every)
	if err != nil {
		return env, envelope.Usage("%v", err)
	}
	plan, err := inspect.Plan(conf.FPS, tl.TotalFrames, tl.Compilation.Timeline.Events, inspect.Request{
		Selector:     selector,
		At:           at,
		EverySeconds: every,
		Frame:        opts.Frame,
		Max:          opts.Max,
	})
	if err != nil {
		return env, envelope.Usage("%v", err)
	}
	if plan.Dropped > 0 {
		env.Findings.Add(diag.Finding{
			Code:      diag.I0001,
			Severity:  diag.SeverityInfo,
			Stage:     "snapshot",
			Message:   fmt.Sprintf("%d selected frames were dropped beyond -max %d", plan.Dropped, opts.Max),
			FixHint:   "raise -max or narrow -events / -every",
			LegacyMsg: fmt.Sprintf("Info: snapshot truncated to %d frames, %d dropped", opts.Max, plan.Dropped),
		})
	}
	if len(plan.Frames) == 0 {
		return env, envelope.Usage("the frame plan selected nothing: -max=%d", opts.Max)
	}

	// 5. Render the planned frames into PNGs.
	sink, err := inspect.NewPNGSink(opts.OutDir, conf.FPS, opts.MaxWidth, sheetFmt, plan, inspect.Manifest{
		ScriptHash: env.ScriptHash,
		Timing: &inspect.Timing{
			DurationSec: audio.Duration,
			TotalFrames: tl.TotalFrames,
			Source:      "tts-cache",
		},
	})
	if err != nil {
		return env, err
	}
	t0 = time.Now()
	if err := builder.RenderTimeline(conf, tl.Compilation.Timeline, engine, sink, tl.Compilation.StyleKeyframes, audio.Processed, conf.CameraEnabled, nil); err != nil {
		return env, err
	}
	env.RecordStage("snapshot", durationMS(t0), len(sink.Frames()))
	// The frame loop closed the sink, so manifest.json and the sheet are on disk
	// already; the envelope reports what is there.
	for _, art := range sink.Frames() {
		env.AddArtifact(art)
	}
	for _, art := range sink.SheetArtifacts() {
		env.AddArtifact(art)
	}
	env.AddArtifact(envelope.Artifact{
		Path:  filepath.Join(opts.OutDir, "manifest.json"),
		Kind:  "manifest",
		Label: fmt.Sprintf("%d of %d frames (%d dropped)", len(plan.Frames), tl.TotalFrames, plan.Dropped),
	})
	return env, nil
}

// cacheMiss turns "this script has never been synthesised" into something an agent
// can act on, instead of silently issuing a cold round of requests.
func (a *App) cacheMiss(err error, conf *model.Project) error {
	miss, ok := err.(*pipeline.ErrCacheMiss)
	if !ok {
		return err
	}
	return envelope.Env(
		fmt.Sprintf("run `zen-board -script %s -o /tmp/zen-warm.mp4` once to fill the cache, or pass -tts-cache pointing at the cache that already has it (looked in %s)", conf.ScriptPath, miss.Cache),
		fmt.Errorf("%w: snapshot never issues TTS requests", miss),
	)
}

// splitList parses a comma-separated flag value into trimmed entries.
func splitList(v string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out, nil
}

// parseInterval reads an -every value such as "0.5s" or "2".
func parseInterval(v string) (float64, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, nil
	}
	if strings.HasSuffix(v, "s") {
		return strconv.ParseFloat(strings.TrimSuffix(v, "s"), 64)
	}
	return strconv.ParseFloat(v, 64)
}
