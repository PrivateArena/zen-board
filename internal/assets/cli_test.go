package assets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCLICommands(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "zen-cli-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 1. Run 'index' on empty directory
	err = RunCLI([]string{"index", "-dir", tempDir}, tempDir)
	if err != nil {
		t.Errorf("failed index command: %v", err)
	}

	// Verify index.json was created
	idxPath := filepath.Join(tempDir, "index.json")
	if _, err := os.Stat(idxPath); os.IsNotExist(err) {
		t.Errorf("expected index.json to be created, but it was not")
	}

	// 2. Run 'list'
	err = RunCLI([]string{"list", "-dir", tempDir}, tempDir)
	if err != nil {
		t.Errorf("failed list command: %v", err)
	}

	// 3. Run 'process-bg' with no background images
	err = RunCLI([]string{"process-bg", "-dir", tempDir}, tempDir)
	if err != nil {
		t.Errorf("failed process-bg command: %v", err)
	}

	// 4. Run 'generate' with empty prompts file (should fail/warn gracefully or exit)
	promptsFile := filepath.Join(tempDir, "prompts.txt")
	err = os.WriteFile(promptsFile, []byte("# Empty prompts file\n"), 0644)
	if err != nil {
		t.Fatalf("failed to write prompts file: %v", err)
	}

	err = RunCLI([]string{"generate", "-dir", tempDir, "-prompts", promptsFile}, tempDir)
	if err != nil {
		t.Errorf("failed generate command: %v", err)
	}
}
