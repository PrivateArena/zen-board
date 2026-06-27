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
		"-tts-cache", filepath.Join(tmpDir, "tts-cache"),
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

func TestV3Features(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping v3 integration test in short mode")
	}

	ts := testutil.NewMockTTSServer()
	defer ts.Close()

	tmpDir, err := os.MkdirTemp("", "zen-v3-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "v3_test.zen")
	inputPath := filepath.Join("examples", "v3_test.zen")
	data, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatalf("reading examples/v3_test.zen: %v", err)
	}
	if err := os.WriteFile(scriptPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(tmpDir, "output.mp4")
	assetsDir := filepath.Join(".", "assets")
	handPath := filepath.Join(assetsDir, "hand.png")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{
		"zen-board",
		"-script", scriptPath,
		"-o", outputPath,
		"-tts", ts.URL,
		"-assets", assetsDir,
		"-hand", handPath,
		"-fps", "10",
		"-w", "320",
		"-h", "180",
		"-disable-transcript",
		"-freeze", "15",
		"-tts-cache", filepath.Join(tmpDir, "tts-cache"),
	}

	err = Run()
	if err != nil {
		t.Fatalf("Run() failed for v3_test.zen: %v", err)
	}

	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Errorf("v3 output file %s was not created", outputPath)
	}
}

func TestIntegrationDisableTranscript(t *testing.T) {
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

	// 3. Run the pipeline with -disable-transcript
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{
		"zen-board",
		"-script", scriptPath,
		"-o", outputPath,
		"-tts", ts.URL,
		"-assets", assetsDir,
		"-hand", handPath,
		"-fps", "10",
		"-w", "100",
		"-h", "100",
		"-disable-transcript",
		"-tts-cache", filepath.Join(tmpDir, "tts-cache"),
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

func TestIntegrationAdvancedDSL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// 1. Mock TTS Server
	ts := testutil.NewMockTTSServer()
	defer ts.Close()

	// 2. Setup temporary workspace
	tmpDir, err := os.MkdirTemp("", "zen-test-advanced-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "test.zen")
	dslScript := `[chapter:"Intro"][style:whiteboard][draw:test:TL] Welcome to whiteboard mode.
[wait:1.0]
[subtitle:top][style:blackboard][text:"Hello Dynamic Text":BL:sans:48:bold] Welcome to blackboard mode.
[wait:1.0]
[subtitle:off][move:test:BR] Moving asset to bottom right.
[wait:1.0]
[erase:test] Erasing the asset.
[wait:1.0]
All done.`
	
	err = os.WriteFile(scriptPath, []byte(dslScript), 0644)
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

	// Create dummy hand variant files to make sure HandRenderer can load them
	createDummyPNG(filepath.Join(tmpDir, "hand_pencil.png"))
	createDummyPNG(filepath.Join(tmpDir, "hand_chalk.png"))
	createDummyPNG(filepath.Join(tmpDir, "hand_eraser.png"))
	createDummyPNG(filepath.Join(tmpDir, "hand_marker.png"))

	// 3. Run the pipeline
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{
		"zen-board",
		"-script", scriptPath,
		"-o", outputPath,
		"-tts", ts.URL,
		"-assets", assetsDir,
		"-hand", handPath,
		"-fps", "10",
		"-w", "100",
		"-h", "100",
		"-tts-cache", filepath.Join(tmpDir, "tts-cache"),
	}

	err = Run()
	if err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	// 4. Verify output exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Errorf("Output file %s was not created", outputPath)
	}
}

func TestIntegrationFastMode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// 1. Mock TTS Server
	ts := testutil.NewMockTTSServer()
	defer ts.Close()

	// 2. Setup temporary workspace
	tmpDir, err := os.MkdirTemp("", "zen-test-fast-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "test.zen")
	dslScript := `[draw:test] Fast mode test.`
	err = os.WriteFile(scriptPath, []byte(dslScript), 0644)
	if err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(tmpDir, "output.mp4")
	assetsDir := filepath.Join(tmpDir, "assets")
	os.Mkdir(assetsDir, 0755)
	createDummyPNG(filepath.Join(assetsDir, "test.png"))
	handPath := filepath.Join(tmpDir, "hand.png")
	createDummyPNG(handPath)

	// 3. Run the pipeline with -fast
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{
		"zen-board",
		"-script", scriptPath,
		"-o", outputPath,
		"-tts", ts.URL,
		"-assets", assetsDir,
		"-hand", handPath,
		"-fps", "10",
		"-w", "100",
		"-h", "100",
		"-fast",
		"-tts-cache", filepath.Join(tmpDir, "tts-cache"),
	}

	err = Run()
	if err != nil {
		t.Fatalf("Run() failed in Fast Mode: %v", err)
	}

	// 4. Verify output exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Errorf("Output file %s was not created in Fast Mode", outputPath)
	}
}

