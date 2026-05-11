package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"zen-board/internal/testutil"
)

func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// 1. Mock TTS Server
	ts := testutil.NewMockTTSServer()
	defer ts.Close()

	// 2. Setup temporary workspace
	tmpDir, err := os.MkdirTemp("", "zen-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "test.zen")
	err = os.WriteFile(scriptPath, []byte("[draw:test] Hello world"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(tmpDir, "output.mp4")
	
	// Create a dummy asset and hand sprite
	assetsDir := filepath.Join(tmpDir, "assets")
	os.Mkdir(assetsDir, 0755)
	
	createDummyPNG(filepath.Join(assetsDir, "test.png"))
	handPath := filepath.Join(tmpDir, "hand.png")
	createDummyPNG(handPath)

	// 3. Run the pipeline
	// Save original args
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{
		"zen-board",
		"-script", scriptPath,
		"-o", outputPath,
		"-tts", ts.URL,
		"-assets", assetsDir,
		"-hand", handPath,
		"-fps", "10", // Low FPS for speed
		"-w", "100",   // Small resolution
		"-h", "100",
	}

	err = Run()
	if err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	// 4. Verify output
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Errorf("Output file %s was not created", outputPath)
	}
}

func createDummyPNG(path string) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	f, _ := os.Create(path)
	defer f.Close()
	png.Encode(f, img)
}
