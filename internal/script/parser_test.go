package script

import (
	"testing"
)

func TestParse(t *testing.T) {
	input := `The king was [draw:king_death@100,200,400,300] killed in battle.
[wait:2.0]
[clear]
His son [draw:prince] took the throne. [wait:1.5][draw:robot]`

	lines := Parse(input)

	if len(lines) != 4 {
		t.Fatalf("Expected 4 lines, got %d", len(lines))
	}

	// Test Line 1 (Draw with coords/dimensions)
	if lines[0].Text != "The king was killed in battle." {
		t.Errorf("Line 1 text mismatch: %s", lines[0].Text)
	}
	if len(lines[0].Actions) != 1 {
		t.Errorf("Line 1 actions mismatch: %d", len(lines[0].Actions))
	} else {
		act := lines[0].Actions[0]
		if act.Tag != "king_death" {
			t.Errorf("Action tag mismatch: %s", act.Tag)
		}
		if act.WordIndex != 3 { // "The king was" -> 3 words
			t.Errorf("Action WordIndex mismatch: %d", act.WordIndex)
		}
		if act.X != 100 || act.Y != 200 || act.W != 400 || act.H != 300 {
			t.Errorf("Action coords mismatch: %+v", act)
		}
	}

	// Test Line 2 (Wait)
	if lines[1].Text != "" {
		t.Errorf("Line 2 text should be empty, got %s", lines[1].Text)
	}
	if len(lines[1].Actions) != 1 || lines[1].Actions[0].Tag != "WAIT:2" {
		t.Errorf("Line 2 wait action mismatch: %+v", lines[1].Actions)
	}

	// Test Line 3 (Clear)
	if lines[2].Text != "" {
		t.Errorf("Line 3 text should be empty, got %s", lines[2].Text)
	}
	if len(lines[2].Actions) != 1 || lines[2].Actions[0].Tag != "clear" {
		t.Errorf("Line 3 clear action mismatch: %+v", lines[2].Actions)
	}

	// Test Line 4 (Interleaved wait and draw)
	if lines[3].Text != "His son took the throne." {
		t.Errorf("Line 4 text mismatch: %s", lines[3].Text)
	}
	if len(lines[3].Actions) != 3 {
		t.Errorf("Line 4 actions count mismatch: %d", len(lines[3].Actions))
	} else {
		// prince
		if lines[3].Actions[0].Tag != "prince" || lines[3].Actions[0].WordIndex != 2 {
			t.Errorf("Line 4 action 0 mismatch: %+v", lines[3].Actions[0])
		}
		// WAIT:1.5
		if lines[3].Actions[1].Tag != "WAIT:1.5" || lines[3].Actions[1].WordIndex != 5 {
			t.Errorf("Line 4 action 1 mismatch: %+v", lines[3].Actions[1])
		}
		// robot
		if lines[3].Actions[2].Tag != "robot" || lines[3].Actions[2].WordIndex != 5 {
			t.Errorf("Line 4 action 2 mismatch: %+v", lines[3].Actions[2])
		}
	}
}
