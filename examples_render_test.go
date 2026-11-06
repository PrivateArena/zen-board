package main

// L3 gate for the authoring surface: every shipped example must compile and
// render end to end against the mock TTS server. The plan's genre scripts are
// the contract - if one of them cannot render, the DSL cannot express the genre.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zen-board/internal/testutil"
)

func TestExamplesRenderEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping end-to-end example renders in short mode")
	}

	ts := testutil.NewMockTTSServer()
	defer ts.Close()

	tmpDir := t.TempDir()
	entries, err := os.ReadDir("examples")
	if err != nil {
		t.Fatalf("read examples: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".zen") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(tmpDir, strings.TrimSuffix(name, ".zen")+".mp4")
			runRender(t, ts.URL, filepath.Join("examples", name), out,
				"-fps", "8", "-w", "320", "-h", "180", "-freeze", "8")

			st, err := os.Stat(out)
			if err != nil {
				t.Fatalf("no output for %s: %v", name, err)
			}
			if st.Size() < 1024 {
				t.Errorf("%s produced a suspiciously small video (%d bytes)", name, st.Size())
			}
		})
	}
}

// runRender invokes the real pipeline in-process, the way the CLI would.
func runRender(t *testing.T, ttsURL, scriptPath, outPath string, extra ...string) {
	t.Helper()
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	args := []string{
		"zen-board",
		"-script", scriptPath,
		"-o", outPath,
		"-tts", ttsURL,
		"-assets", filepath.Join(".", "assets"),
		"-hand", filepath.Join(".", "assets", "hand.png"),
		"-disable-transcript",
		"-tts-cache", filepath.Join(t.TempDir(), "tts-cache"),
	}
	os.Args = append(args, extra...)

	if err := Run(); err != nil {
		t.Fatalf("Run() failed for %s: %v", scriptPath, err)
	}
}
