package cli

import (
	"fmt"
	"time"

	"zen-board/internal/envelope"
	"zen-board/internal/pipeline"
)

// check is the gate: parse, lint, resolve assets, stop. It never synthesises TTS,
// never opens a socket and never renders - so it is free to run before every edit,
// and its exit code is the whole answer.
func (a *App) check(opts *Options, _ []string) (*envelope.Envelope, error) {
	env := envelope.New("check")
	conf := opts.Project

	if opts.ShowDSL {
		fmt.Fprint(a.Stdout, DSLHelpTable())
		return env, nil
	}

	t0 := time.Now()
	src, err := pipeline.LoadScript(conf.ScriptPath)
	env.RecordStage("parse", durationMS(t0), len(src.Lines))
	if err != nil {
		return env, err
	}
	env.ScriptHash = src.Source.Hash()

	// Lint covers the parse findings plus the grammar and catalog rules, over the
	// raw bytes. Both are pure reads: zero TTS requests, zero renders.
	t1 := time.Now()
	report := LintScript(src.Source, conf.AssetsDir, opts.LintOnly)
	env.Findings = report
	env.RecordStage("lint", durationMS(t1), len(report.Findings))

	if env.Findings.Blocked() {
		return env, envelope.Blocked(env.Findings)
	}
	return env, nil
}
