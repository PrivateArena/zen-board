package cli

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"testing"

	"zen-board/internal/envelope"
	"zen-board/internal/inspect"
)

// The timeline verb is what an agent reads instead of watching a video, so its asset
// column must show the id from the script - never a rasterisation cache key, which
// would send the agent hunting for a file that does not exist.
func TestTimelineReportsAuthoredAssetIDs(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "examples", "advanced.zen")
	cache := warmCache(t, scriptPath)

	app, stdout, stderr := testApp(t)
	code := app.Run([]string{
		"timeline", "-script", scriptPath, "-assets", repoAssets(t), "-tts-cache", cache,
		"-w", "320", "-h", "180", "-fps", "8", "-freeze", "6", "-json",
	})
	if code != envelope.ExitOK {
		t.Fatalf("timeline exit = %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("bad envelope: %v\n%s", err, stdout.String())
	}
	data, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatal(err)
	}
	var doc inspect.TimelineDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("timeline data: %v", err)
	}
	if len(doc.Events) == 0 {
		t.Fatal("timeline reports no events")
	}
	if doc.FPS != 8 || doc.Width != 320 {
		t.Errorf("timeline lost config: fps=%d width=%d", doc.FPS, doc.Width)
	}
	if len(doc.Words) == 0 {
		t.Error("timeline reports no word timings")
	}
	if len(doc.ScriptHash) != 64 {
		t.Errorf("script_hash = %q", doc.ScriptHash)
	}
	rasterKey := regexp.MustCompile(`\.r\d+\.[0-9a-f]{8}`)
	for _, ev := range doc.Events {
		if rasterKey.MatchString(ev.Asset) {
			t.Errorf("event %s exposes a rasterisation key: %q", ev.Type, ev.Asset)
		}
	}
	var sawRobot, sawID bool
	for _, ev := range doc.Events {
		if ev.Asset == "robot" {
			sawRobot = true
		}
		if ev.ID != "" {
			sawID = true
		}
	}
	if !sawRobot {
		t.Error("advanced.zen draws the robot asset; no event reports it by id")
	}
	if !sawID {
		t.Error("events carry no stable id, so @ref cannot be verified")
	}
}

// A reveal that lasts forever must be reported as lasting to the end of the video, not
// to the internal sentinel frame.
func TestTimelineClampsForeverEvents(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "examples", "genre_mascot.zen")
	cache := warmCache(t, scriptPath)
	app, stdout, stderr := testApp(t)
	code := app.Run([]string{
		"timeline", "-script", scriptPath, "-assets", repoAssets(t), "-tts-cache", cache,
		"-w", "320", "-h", "180", "-fps", "8", "-freeze", "6", "-json",
	})
	if code != envelope.ExitOK {
		t.Fatalf("timeline exit = %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(env.Data)
	var doc inspect.TimelineDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, ev := range doc.Events {
		if ev.EndFrame > doc.TotalFrames {
			t.Errorf("event %s ends at frame %d, beyond the %d frames of the video", ev.Type, ev.EndFrame, doc.TotalFrames)
		}
	}
}
