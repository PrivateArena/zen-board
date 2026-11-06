package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zen-board/internal/envelope"
	"zen-board/internal/testutil"
)

func testApp(t *testing.T) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	app := New()
	app.Stdout = stdout
	app.Stderr = stderr
	return app, stdout, stderr
}

func writeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case.zen")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func repoAssets(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "assets"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skip("no assets directory in the repo")
	}
	return dir
}

// TestCheckGateIsFreeAndBlocked is the plan's CP-2 acceptance: an unknown asset
// exits 10 having issued zero HTTP requests. The counter lives in the mock server,
// so it counts what actually arrived rather than what the code intended.
func TestCheckGateIsFreeAndBlocked(t *testing.T) {
	mock := testutil.NewCountingMockTTSServer()
	defer mock.Close()

	scriptPath := writeScript(t, `This draws a thing that does not exist. [draw:totally_missing_asset]`)
	app, _, _ := testApp(t)
	code := app.Run([]string{
		"check", "-script", scriptPath,
		"-assets", repoAssets(t),
		"-tts", mock.URL(),
	})
	if code != envelope.ExitBlocked {
		t.Errorf("exit = %d, want %d (blocked)", code, envelope.ExitBlocked)
	}
	if got := mock.Requests(); got != 0 {
		t.Errorf("check issued %d TTS requests, want 0", got)
	}
}

// TestCheckPassesOnShippedExamples: every example must lint to zero errors against
// the real catalog, or the example is a trap for the next agent.
func TestCheckPassesOnShippedExamples(t *testing.T) {
	dir := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.zen"))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != len(entries) {
		t.Fatalf("example count mismatch: %d files, %d entries", len(names), len(entries))
	}
	for _, path := range names {
		t.Run(filepath.Base(path), func(t *testing.T) {
			mock := testutil.NewCountingMockTTSServer()
			defer mock.Close()
			app, stdout, _ := testApp(t)
			code := app.Run([]string{"check", "-script", path, "-assets", repoAssets(t), "-tts", mock.URL(), "-json"})
			if code != envelope.ExitOK {
				t.Errorf("check %s exit = %d, want 0. envelope:\n%s", filepath.Base(path), code, stdout.String())
			}
			if got := mock.Requests(); got != 0 {
				t.Errorf("%s: check issued %d TTS requests, want 0", filepath.Base(path), got)
			}
		})
	}
}

// TestJSONIsOneDocumentPerInvocation runs check twice in the same process: the
// stdout redirect must be undone after each command, so every invocation emits
// exactly one document and nothing else - and the second call is not polluted by
// the first.
func TestJSONIsOneDocumentPerInvocation(t *testing.T) {
	app, stdout, stderr := testApp(t)
	_ = stderr
	path := writeScript(t, `[draw:robot] fine`)
	for i := 0; i < 2; i++ {
		if code := app.Run([]string{"check", "-script", path, "-assets", repoAssets(t), "-json"}); code != envelope.ExitOK {
			t.Fatalf("invocation %d: exit %d", i, code)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	docs := 0
	for {
		var env envelope.Envelope
		if err := dec.Decode(&env); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("document %d is not valid JSON: %v\n%s", docs, err, stdout.String())
		}
		docs++
	}
	if docs != 2 {
		t.Errorf("got %d JSON documents from two invocations, want 2:\n%s", docs, stdout.String())
	}
	if !strings.Contains(stdout.String(), "\"ok\": true") {
		t.Errorf("documents do not report success:\n%s", stdout.String())
	}
}

// TestExitCodes: the table an agent or CI step branches on.
func TestExitCodes(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want int
	}{
		{"unknown verb", []string{"frobnicate"}, envelope.ExitUsage},
		{"check without script", []string{"check"}, envelope.ExitUsage},
		{"blocked script", []string{"check", "-script", writeScript(t, `[draw:not_an_asset]`), "-assets", repoAssets(t)}, envelope.ExitBlocked},
		{"clean script", []string{"check", "-script", writeScript(t, `[draw:robot] fine`), "-assets", repoAssets(t)}, envelope.ExitOK},
		{"dsl table", []string{"check", "-dsl", "-script", "x.zen"}, envelope.ExitOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := testutil.NewCountingMockTTSServer()
			defer mock.Close()
			app, stdout, _ := testApp(t)
			_ = stdout
			code := app.Run(append(tc.argv, "-tts", mock.URL()))
			if code != tc.want {
				t.Errorf("Run(%v) = %d, want %d\noutput:\n%s", tc.argv, code, tc.want, stdout.String())
			}
		})
	}
}

// TestCheckDSLTableIsGenerated asserts the documented vocabulary is the compiled
// one: a tag in the table must appear in the printed help.
func TestCheckDSLTableIsGenerated(t *testing.T) {
	app, stdout, _ := testApp(t)
	if code := app.Run([]string{"check", "-dsl", "-script", "x.zen"}); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	out := stdout.String()
	for _, tag := range []string{"draw", "text", "arrow", "highlight", "compare", "overlay", "counter", "watermark"} {
		if !strings.Contains(out, "["+tag) {
			t.Errorf("DSL table is missing %q:\n%s", tag, out[:min(200, len(out))])
		}
	}
	if !strings.Contains(out, "layout/camera presets") {
		t.Error("DSL table does not document the preset vocabulary")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
