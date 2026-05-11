package script

import (
	"testing"
)

func TestParse(t *testing.T) {
	input := `The king was [draw:king_death] killed in battle.
[wait:2.0]
His son [draw:prince] took the throne.`

	lines := Parse(input)

	if len(lines) != 3 {
		t.Errorf("Expected 3 lines, got %d", len(lines))
	}

	// Test Line 1
	if lines[0].Text != "The king was killed in battle." {
		t.Errorf("Line 1 text mismatch: %s", lines[0].Text)
	}
	if len(lines[0].Actions) != 1 {
		t.Errorf("Line 1 actions mismatch: %d", len(lines[0].Actions))
	} else {
		if lines[0].Actions[0].Tag != "king_death" {
			t.Errorf("Action tag mismatch: %s", lines[0].Actions[0].Tag)
		}
		if lines[0].Actions[0].WordIndex != 3 { // "The king was" -> 3 words
			t.Errorf("Action WordIndex mismatch: %d", lines[0].Actions[0].WordIndex)
		}
	}

	// Test Line 2 (Wait)
	if lines[1].Text != "" {
		t.Errorf("Line 2 text should be empty, got %s", lines[1].Text)
	}
	if len(lines[1].Actions) != 1 || lines[1].Actions[0].Tag != "WAIT:2" {
		t.Errorf("Line 2 wait action mismatch")
	}

	// Test Line 3
	if lines[2].Text != "His son took the throne." {
		t.Errorf("Line 3 text mismatch: %s", lines[2].Text)
	}
	if len(lines[2].Actions) != 1 || lines[2].Actions[0].Tag != "prince" || lines[2].Actions[0].WordIndex != 2 {
		t.Errorf("Line 3 action mismatch: %+v", lines[2].Actions[0])
	}
}
