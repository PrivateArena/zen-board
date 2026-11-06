package diag

import (
	"strings"
	"testing"
)

func TestReportBlocksOnlyOnError(t *testing.T) {
	var r Report
	if r.Blocked() || !r.Empty() {
		t.Fatal("zero Report must be empty and unblocked")
	}
	r.Add(Finding{Code: W0001, Severity: SeverityWarning, Message: "literal bracket"})
	if r.Blocked() {
		t.Error("a warning must never block")
	}
	r.Add(Finding{Code: E0008, Severity: SeverityOf(E0008), Message: "no events"})
	if !r.Blocked() {
		t.Error("an error must block")
	}
	if got := r.Count(SeverityWarning); got != 1 {
		t.Errorf("warning count = %d, want 1", got)
	}
	if got := len(r.ByCode(E0008)); got != 1 {
		t.Errorf("ByCode(E0008) = %d, want 1", got)
	}
}

// A value-typed accumulator would silently drop findings added after the copy.
func TestReportPointerReceiverAccumulates(t *testing.T) {
	r := &Report{}
	r.Add(Finding{Code: E0001, Message: "a"})
	r.Extend(&Report{Findings: []Finding{{Code: E0009, Message: "b"}}})
	r.Extend(nil)
	if len(r.Findings) != 2 {
		t.Fatalf("Extend lost findings: %+v", r.Findings)
	}
}

func TestFindingRenderingAndOrdering(t *testing.T) {
	r := &Report{}
	r.Add(Finding{Code: E0002, Severity: SeverityError, Message: "second", File: "demo.zen", Line: 14, Col: 22, Stage: "lint", FixHint: "spell it right"})
	r.Add(Finding{Code: E0001, Severity: SeverityError, Message: "first", File: "demo.zen", Line: 3, Col: 1, Stage: "lint"})

	got := strings.Split(strings.TrimRight(r.String(), "\n"), "\n")
	if len(got) != 3 { // two findings + one hint line
		t.Fatalf("rendered %d lines, want 3:\n%s", len(got), r.String())
	}
	if !strings.HasPrefix(got[0], "demo.zen:3:1: error[E0001]: first") {
		t.Errorf("findings are not sorted by position: %q", got[0])
	}
	if !strings.HasPrefix(got[1], "demo.zen:14:22: error[E0008]") && !strings.HasPrefix(got[1], "demo.zen:14:22: error[E0002]: second") {
		t.Errorf("unexpected second line %q", got[1])
	}
	if !strings.Contains(got[2], "hint: spell it right") {
		t.Errorf("hint line missing: %q", got[2])
	}
}

func TestSeverityOfUnknownCodeIsError(t *testing.T) {
	if SeverityOf("E9999") != SeverityError {
		t.Error("an unknown code must default to error so a new rule cannot downgrade itself")
	}
	if SeverityOf(W0002) != SeverityWarning {
		t.Error("W0002 must be a warning")
	}
	if SeverityOf(I0001) != SeverityInfo {
		t.Error("I0001 must be info")
	}
}
