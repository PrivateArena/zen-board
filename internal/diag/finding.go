// Package diag carries structured diagnostics (findings) produced by every
// stage of the pipeline: parse, lint, compile, render.
//
// It is a LEAF package: it imports only the standard library. Any producer of
// findings (script, builder, cli) imports diag - never the reverse - so that no
// pipeline package needs a dependency on cli.
package diag

import (
	"fmt"
	"sort"
	"strings"
)

// Severity levels. Only SeverityError blocks a run (see Report.Blocked).
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Finding is a single diagnostic attached to a source location.
type Finding struct {
	Code      string   `json:"code"`
	Severity  Severity `json:"severity"`
	Message   string   `json:"message"`
	FixHint   string   `json:"fix_hint,omitempty"`
	File      string   `json:"file,omitempty"`
	Line      int      `json:"line,omitempty"`
	Col       int      `json:"col,omitempty"`
	Snippet   string   `json:"snippet,omitempty"`
	Stage     string   `json:"stage"`
	LegacyMsg string   `json:"-"` // original log.Printf wording, kept for parity
}

// Location renders "file:line:col" (truncated where unknown), the form an
// editor or an agent can jump on.
func (f Finding) Location() string {
	var sb strings.Builder
	if f.File != "" {
		sb.WriteString(f.File)
	} else {
		sb.WriteString("<script>")
	}
	if f.Line > 0 {
		fmt.Fprintf(&sb, ":%d", f.Line)
		if f.Col > 0 {
			fmt.Fprintf(&sb, ":%d", f.Col)
		}
	}
	return sb.String()
}

// LegacyOrString renders the finding, preferring the historic log.Printf wording
// while call sites migrate to the structured report.
func (f Finding) LegacyOrString() string {
	if f.LegacyMsg != "" {
		return f.LegacyMsg
	}
	return f.String()
}

func (f Finding) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s: %s[%s]: %s", f.Location(), f.Severity, f.Code, f.Message)
	if f.FixHint != "" {
		fmt.Fprintf(&sb, "\n  hint: %s", f.FixHint)
	}
	return sb.String()
}

// Report is an ordered collection of findings. All mutating methods take a
// pointer receiver: a zero Report is a valid empty accumulator, and value
// copies must never silently drop findings.
type Report struct {
	Findings []Finding `json:"findings"`
}

// Add appends one finding, normalising a missing severity to error.
func (r *Report) Add(f Finding) {
	if f.Severity == "" {
		f.Severity = SeverityError
	}
	r.Findings = append(r.Findings, f)
}

// Addf appends a formatted finding.
func (r *Report) Addf(sev Severity, code, format string, args ...any) {
	r.Add(Finding{Code: code, Severity: sev, Message: fmt.Sprintf(format, args...)})
}

func (r *Report) Extend(other *Report) {
	if other == nil || r == nil {
		return
	}
	r.Findings = append(r.Findings, other.Findings...)
}

// Blocked reports whether the run must stop: any error-tier finding blocks,
// warnings and info never do. A nil report blocks nothing.
func (r *Report) Blocked() bool {
	if r == nil {
		return false
	}
	for _, f := range r.Findings {
		if f.Severity == SeverityError {
			return true
		}
	}
	return false
}

func (r *Report) ByCode(code string) []Finding {
	if r == nil {
		return nil
	}
	var out []Finding
	for _, f := range r.Findings {
		if f.Code == code {
			out = append(out, f)
		}
	}
	return out
}

// Count returns the number of findings at the given severity.
func (r *Report) Count(sev Severity) int {
	if r == nil {
		return 0
	}
	n := 0
	for _, f := range r.Findings {
		if f.Severity == sev {
			n++
		}
	}
	return n
}

func (r *Report) Empty() bool { return r == nil || len(r.Findings) == 0 }

// Sorted returns findings ordered by file, line, col, code. The receiver stays
// untouched so compile-time ordering (which is semantic) is preserved.
func (r *Report) Sorted() []Finding {
	if r == nil {
		return nil
	}
	out := append([]Finding(nil), r.Findings...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Col != b.Col {
			return a.Col < b.Col
		}
		return a.Code < b.Code
	})
	return out
}

// String renders one finding per line in the canonical
// `demo.zen:14:22: error[E0008]: ...` shape, plus hint lines.
func (r *Report) String() string {
	if r == nil {
		return ""
	}
	var sb strings.Builder
	for _, f := range r.Sorted() {
		sb.WriteString(f.String())
		sb.WriteString("\n")
	}
	return sb.String()
}
