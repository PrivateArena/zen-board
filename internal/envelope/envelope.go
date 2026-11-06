// Package envelope is the machine-readable result contract of every CLI verb.
//
// It is a leaf package: it depends on diag for findings and nothing else in the
// pipeline, so any package may build one without importing the CLI.
package envelope

import (
	"encoding/json"
	"io"

	"zen-board/internal/diag"
)

// Version is the envelope schema revision. Agents may pin behaviour to it.
const Version = "1"

// Artifact is a file the command produced.
type Artifact struct {
	Path    string  `json:"path"`
	Frame   int     `json:"frame,omitempty"`
	TimeSec float64 `json:"time_sec,omitempty"`
	Kind    string  `json:"kind"`
	Label   string  `json:"label,omitempty"`
}

// Stage is one pipeline step, timed. A skipped stage is reported as such rather
// than omitted, so "0 events" cannot masquerade as success.
type Stage struct {
	Name       string `json:"name"`
	MS         int64  `json:"ms"`
	Items      int    `json:"items,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
	SkipReason string `json:"skip_reason,omitempty"`
}

// Envelope is the single JSON document a command writes to stdout under -json.
type Envelope struct {
	OK         bool         `json:"ok"`
	Command    string       `json:"command"`
	Version    string       `json:"version"`
	ScriptHash string       `json:"script_hash,omitempty"`
	Findings   *diag.Report `json:"findings,omitempty"`
	Stages     []Stage      `json:"stages,omitempty"`
	Artifacts  []Artifact   `json:"artifacts,omitempty"`
	Data       any          `json:"data,omitempty"`
	Error      *Error       `json:"error,omitempty"`
}

// New starts an envelope for a command.
func New(command string) *Envelope {
	return &Envelope{OK: true, Command: command, Version: Version, Findings: &diag.Report{}}
}

// Stage records a completed pipeline step.
func (e *Envelope) RecordStage(name string, ms int64, items int) {
	e.Stages = append(e.Stages, Stage{Name: name, MS: ms, Items: items})
}

// Skip records a pipeline step that was deliberately not run.
func (e *Envelope) Skip(name, reason string) {
	e.Stages = append(e.Stages, Stage{Name: name, Skipped: true, SkipReason: reason})
}

// AddArtifact records an output file.
func (e *Envelope) AddArtifact(a Artifact) {
	e.Artifacts = append(e.Artifacts, a)
}

// Fail marks the envelope as failed. The exit code comes from the error class.
func (e *Envelope) Fail(err error) {
	e.OK = false
	e.Error = Wrap(err)
	// The blocked error carries the very report this envelope already holds; extending
	// with it would duplicate every finding in the JSON output.
	if blocked, ok := err.(*BlockedError); ok && blocked.Report != e.Findings && e.Findings != nil {
		e.Findings.Extend(blocked.Report)
	}
}

// ExitCode is the process exit code this envelope demands.
func (e *Envelope) ExitCode() int {
	if e.Error != nil {
		return e.Error.ExitCode()
	}
	if e.Findings.Blocked() {
		return ExitBlocked
	}
	return 0
}

// Write renders the envelope as one JSON document.
func (e *Envelope) Write(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if e.Findings != nil && e.Findings.Empty() {
		e.Findings = nil
	}
	return enc.Encode(e)
}
