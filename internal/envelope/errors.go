package envelope

import (
	"errors"
	"fmt"

	"zen-board/internal/diag"
)

// Exit codes. The table is the contract an agent or CI step branches on; 0 is
// success, 1 stays the generic failure so scripts written against the current
// single-exit-code binary keep working.
const (
	ExitOK      = 0
	ExitError   = 1
	ExitUsage   = 2
	ExitEnv     = 3
	ExitBlocked = 10
)

// Error is the machine-readable failure carried by an Envelope.
type Error struct {
	Class   string `json:"class"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	FixHint string `json:"fix_hint,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// ExitCode maps the error class to the process exit code.
func (e *Error) ExitCode() int {
	switch e.Class {
	case ClassUsage:
		return ExitUsage
	case ClassEnv:
		return ExitEnv
	case ClassBlocked:
		return ExitBlocked
	default:
		return ExitError
	}
}

// Error classes.
const (
	ClassUsage   = "usage"
	ClassEnv     = "environment"
	ClassBlocked = "blocked"
	ClassRun     = "run"
)

// UsageError: bad flags, unknown verb, missing argument. Exit 2.
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

// EnvError: a precondition of the environment is missing - no ffmpeg, a TTS
// server that will not answer, a cold cache where only a warm one works. Exit 3.
type EnvError struct {
	Err  error
	Hint string
}

func (e *EnvError) Error() string { return e.Err.Error() }
func (e *EnvError) Unwrap() error { return e.Err }

// BlockedError: the script is not renderable as written. Exit 10. The findings
// travel with it so the report and the exit code can never disagree.
type BlockedError struct {
	Err    error
	Report *diag.Report
}

func (e *BlockedError) Error() string { return e.Err.Error() }
func (e *BlockedError) Unwrap() error { return e.Err }

// Usage builds a usage error.
func Usage(format string, args ...any) error { return &UsageError{Err: fmt.Errorf(format, args...)} }

// Env builds an environment error with an actionable hint.
func Env(hint string, err error) error { return &EnvError{Err: err, Hint: hint} }

// Blocked builds the error that makes `check` exit 10.
func Blocked(report *diag.Report) error {
	n := report.Count(diag.SeverityError)
	return &BlockedError{
		Err:    fmt.Errorf("%d error(s) in the script; nothing was rendered", n),
		Report: report,
	}
}

// Wrap converts any error into the wire form.
func Wrap(err error) *Error {
	if err == nil {
		return nil
	}
	out := &Error{Class: ClassRun, Message: err.Error()}
	var env *EnvError
	if errors.As(err, &env) {
		out.Class = ClassEnv
		out.FixHint = env.Hint
		return out
	}
	var usage *UsageError
	if errors.As(err, &usage) {
		out.Class = ClassUsage
		return out
	}
	var blocked *BlockedError
	if errors.As(err, &blocked) {
		out.Class = ClassBlocked
		return out
	}
	return out
}

// ExitCode is the process exit code for an arbitrary error.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	return Wrap(err).ExitCode()
}
