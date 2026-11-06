package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zen-board/internal/envelope"
	"zen-board/internal/inspect"
	"zen-board/internal/testutil"
)

// warmCache renders the script once with the mock TTS server so the cache holds
// every line's audio and word timings.
func warmCache(t *testing.T, scriptPath string) string {
	t.Helper()
	mock := testutil.NewCountingMockTTSServer()
	t.Cleanup(mock.Close)
	cache := t.TempDir()
	app, stdout, stderr := testApp(t)
	_ = stdout
	out := filepath.Join(t.TempDir(), "warm.mp4")
	code := app.Run([]string{
		"render", "-script", scriptPath, "-o", out,
		"-assets", repoAssets(t), "-tts", mock.URL(), "-tts-cache", cache,
		"-w", "160", "-h", "90", "-fps", "5", "-freeze", "5", "-disable-transcript",
	})
	if code != envelope.ExitOK {
		t.Fatalf("warm render failed: exit %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	if mock.Requests() == 0 {
		t.Fatal("warm render issued no TTS requests; the cache is not warm")
	}
	return cache
}

func TestSnapshotWritesPNGsManifestAndSheet(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "examples", "v3_test.zen")
	cache := warmCache(t, scriptPath)

	outDir := filepath.Join(t.TempDir(), "frames")
	// An ffmpeg that is not there: if the snapshot path spawns one, this fails.
	t.Setenv("PATH", t.TempDir())

	app, stdout, stderr := testApp(t)
	mock := testutil.NewCountingMockTTSServer()
	defer mock.Close()

	code := app.Run([]string{
		"snapshot", "-script", scriptPath, "-assets", repoAssets(t),
		"-tts-cache", cache, "-tts", mock.URL(),
		"-w", "160", "-h", "90", "-fps", "5", "-freeze", "5",
		"-events", "end", "-max", "24", "-outdir", outDir, "-json",
	})
	if code != envelope.ExitOK {
		t.Fatalf("snapshot exit = %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	if got := mock.Requests(); got != 0 {
		t.Errorf("snapshot issued %d TTS requests, want 0", got)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("no output dir: %v", err)
	}
	var pngs, manifest, sheet int
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), ".png"):
			pngs++
		case e.Name() == "manifest.json":
			manifest++
		case e.Name() == "sheet.jpg":
			sheet++
		}
	}
	if pngs == 0 {
		t.Error("no PNGs written")
	}
	if pngs > 24 {
		t.Errorf("wrote %d PNGs, -max 24 must cap the run", pngs)
	}
	if manifest != 1 {
		t.Errorf("manifest.json written %d times, want 1", manifest)
	}
	if sheet != 1 {
		t.Errorf("contact sheet written %d times, want 1", sheet)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		ScriptHash string `json:"script_hash"`
		Frames     []struct {
			Path    string  `json:"path"`
			Frame   int     `json:"frame"`
			TimeSec float64 `json:"time_sec"`
			Kind    string  `json:"kind"`
			Label   string  `json:"label"`
		} `json:"frames"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	if len(doc.Frames) != pngs {
		t.Errorf("manifest lists %d frames, directory has %d PNGs", len(doc.Frames), pngs)
	}
	if len(doc.ScriptHash) != 64 {
		t.Errorf("manifest script_hash = %q, want the script content hash", doc.ScriptHash)
	}
	for _, f := range doc.Frames {
		if !strings.HasSuffix(f.Path, ".png") {
			t.Errorf("artifact path %q is not a PNG", f.Path)
		}
		if f.Kind != "engine" {
			t.Errorf("artifact %q kind = %q, want engine (these are encoder-identical pixels)", f.Path, f.Kind)
		}
	}

	// The envelope must report the same artifacts.
	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v\n%s", err, stdout.String())
	}
	t.Logf("dir pngs=%d artifacts=%d", pngs, len(env.Artifacts))
	for _, a := range env.Artifacts {
		t.Logf("  %s %s", a.Kind, a.Path)
	}
	if len(env.Artifacts) != pngs+2 { // frames + sheet + manifest
		t.Errorf("envelope reports %d artifacts, want %d frames + sheet + manifest", len(env.Artifacts), pngs)
	}
	var sawSheet, sawManifest bool
	for _, art := range env.Artifacts {
		switch art.Kind {
		case "sheet":
			sawSheet = true
		case "manifest":
			sawManifest = true
			if art.Label == "" {
				t.Error("the manifest artifact must say how many frames were kept and dropped")
			}
		}
	}
	if !sawSheet || !sawManifest {
		t.Errorf("envelope artifacts missing sheet or manifest: %+v", env.Artifacts)
	}
}

// TestSnapshotRefusesColdCache: frame numbering comes from real narration timings, so
// a cold cache must be an actionable error - never eight silent HTTP round trips.
func TestSnapshotRefusesColdCache(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "examples", "demo.zen")
	app, _, _ := testApp(t)
	mock := testutil.NewCountingMockTTSServer()
	defer mock.Close()

	code := app.Run([]string{
		"snapshot", "-script", scriptPath, "-assets", repoAssets(t),
		"-tts-cache", t.TempDir(), "-tts", mock.URL(),
		"-w", "160", "-h", "90", "-fps", "5", "-outdir", filepath.Join(t.TempDir(), "frames"),
	})
	if code != envelope.ExitEnv {
		t.Errorf("exit = %d, want %d (environment precondition unmet)", code, envelope.ExitEnv)
	}
	if got := mock.Requests(); got != 0 {
		t.Errorf("a cold cache must not be filled over the network: %d requests issued", got)
	}
}

// TestSnapshotClearsStaleFrames: a PNG from an older script must not survive a run.
func TestSnapshotClearsStaleFrames(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "examples", "short_test.zen")
	cache := warmCache(t, scriptPath)
	outDir := filepath.Join(t.TempDir(), "frames")
	stale := filepath.Join(outDir, "f000000_000.00s.png")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("from an older script"), 0o644); err != nil {
		t.Fatal(err)
	}

	app, stdout, stderr := testApp(t)
	code := app.Run([]string{
		"snapshot", "-script", scriptPath, "-assets", repoAssets(t), "-tts-cache", cache,
		"-w", "160", "-h", "90", "-fps", "5", "-frame", "1", "-outdir", outDir,
	})
	if code != envelope.ExitOK {
		t.Fatalf("exit = %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(stale)
	if err == nil && string(data) == "from an older script" {
		t.Error("a stale PNG from an older script survived the run")
	}
}

// TestSnapshotRefusesFast: -fast halves the canvas before compile, so every absolute
// @x,y in the script lands somewhere else. A snapshot under it would lie.
func TestSnapshotRefusesFast(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "examples", "short_test.zen")
	cache := warmCache(t, scriptPath)
	app, _, _ := testApp(t)
	code := app.Run([]string{
		"snapshot", "-script", scriptPath, "-assets", repoAssets(t), "-tts-cache", cache,
		"-fast", "-outdir", filepath.Join(t.TempDir(), "frames"),
	})
	if code != envelope.ExitUsage {
		t.Errorf("exit = %d, want %d (usage error explaining the refusal)", code, envelope.ExitUsage)
	}
}

// TestSnapshotSheetFormats: PNG sheets are available but the default is JPEG, because
// the sheet is the artifact a vision model reads most.
func TestSnapshotSheetFormats(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "examples", "short_test.zen")
	cache := warmCache(t, scriptPath)
	for _, tc := range []struct{ flag, want string }{{"png", "sheet.png"}, {"none", ""}} {
		outDir := filepath.Join(t.TempDir(), "frames")
		app, stdout, stderr := testApp(t)
		code := app.Run([]string{
			"snapshot", "-script", scriptPath, "-assets", repoAssets(t), "-tts-cache", cache,
			"-w", "160", "-h", "90", "-fps", "5", "-frame", "0", "-outdir", outDir, "-sheet", tc.flag,
		})
		if code != envelope.ExitOK {
			t.Fatalf("-sheet %s: exit %d\n%s\n%s", tc.flag, code, stdout.String(), stderr.String())
		}
		if tc.want == "" {
			for _, name := range []string{"sheet.png", "sheet.jpg"} {
				if _, err := os.Stat(filepath.Join(outDir, name)); err == nil {
					t.Errorf("-sheet none still wrote %s", name)
				}
			}
			continue
		}
		if _, err := os.Stat(filepath.Join(outDir, tc.want)); err != nil {
			t.Errorf("-sheet %s: %v", tc.flag, err)
		}
	}
}

func TestSnapshotSheetFormatValidation(t *testing.T) {
	if _, err := inspect.SheetFormatOf("tiff"); err == nil {
		t.Error("an unknown sheet format must be rejected")
	}
	if got, err := inspect.SheetFormatOf(""); err != nil || got != inspect.SheetJPEG {
		t.Errorf("the default sheet format must be jpg, got %q (%v)", got, err)
	}
}
