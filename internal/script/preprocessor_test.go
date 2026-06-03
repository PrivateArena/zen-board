package script

import (
	"testing"
	"zen-board/internal/model"
)

func TestSplitInlineWaits(t *testing.T) {
	// Setup input script line
	lines := []model.ScriptLine{
		{
			Text: "Hello world this is a test.",
			Actions: []model.DrawAction{
				{Tag: "WAIT:2.5", WordIndex: 2}, // After "world"
				{Tag: "robot", WordIndex: 1},    // At "Hello"
				{Tag: "pencil", WordIndex: 4},   // At "a" (which is word index 4 of segment after split if adjusted, or 5 in total)
			},
		},
	}

	result := SplitInlineWaits(lines)

	// We expect 3 lines:
	// 1. Text: "Hello world" with action "robot" at WordIndex: 1
	// 2. Text: "" with action "WAIT:2.5"
	// 3. Text: "this is a test." with action "pencil" at WordIndex: 2 (since lastWordIdx was 2, 4 - 2 = 2)
	if len(result) != 3 {
		t.Fatalf("Expected 3 lines, got %d", len(result))
	}

	if result[0].Text != "Hello world" {
		t.Errorf("Expected first line 'Hello world', got '%s'", result[0].Text)
	}
	if len(result[0].Actions) != 1 || result[0].Actions[0].Tag != "robot" {
		t.Errorf("Expected action 'robot' on first line, got %+v", result[0].Actions)
	}

	if result[1].Text != "" {
		t.Errorf("Expected second line to be empty, got '%s'", result[1].Text)
	}
	if len(result[1].Actions) != 1 || result[1].Actions[0].Tag != "WAIT:2.5" {
		t.Errorf("Expected WAIT:2.5 on second line, got %+v", result[1].Actions)
	}

	if result[2].Text != "this is a test." {
		t.Errorf("Expected third line 'this is a test.', got '%s'", result[2].Text)
	}
	if len(result[2].Actions) != 1 || result[2].Actions[0].Tag != "pencil" || result[2].Actions[0].WordIndex != 2 {
		t.Errorf("Expected action 'pencil' with WordIndex 2, got %+v", result[2].Actions)
	}
}
