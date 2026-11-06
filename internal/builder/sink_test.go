package builder

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"zen-board/internal/constant"
	"zen-board/internal/model"
	"zen-board/internal/render"
	"zen-board/internal/script"
	"zen-board/internal/testutil"
	"zen-board/internal/tts"
)

// rendered is one frame the sink kept, with the bytes as they were handed over.
type rendered struct {
	order int
	pix   []byte
}

// captureSink keeps every frame it is given, copying the pixels at write time
// because the contract says the collector owns the buffer.
type captureSink struct {
	frames    map[int]bool
	seen      map[int]bool
	got       []rendered
	writes    int64
	closeArgs int
	err       error
}

func (s *captureSink) Wants(frame int) bool {
	if s.frames == nil {
		return true
	}
	return s.frames[frame]
}

func (s *captureSink) WriteFrame(frameNumber int, frame *image.RGBA) error {
	atomic.AddInt64(&s.writes, 1)
	pix := make([]byte, len(frame.Pix))
	copy(pix, frame.Pix)
	s.got = append(s.got, rendered{order: frameNumber, pix: pix})
	if s.seen == nil {
		s.seen = map[int]bool{}
	}
	s.seen[frameNumber] = true
	return s.err
}

func (s *captureSink) Close() error {
	s.closeArgs++
	return nil
}

// fullRender runs the real pipeline for an example and returns the compiled
// timeline plus everything the sink saw.
func fullRender(t *testing.T, scriptName string, want map[int]bool) (*captureSink, int, *render.Engine) {
	t.Helper()
	conf := goldenConf(t, scriptName)
	prepped := prepareExample(t, conf)

	sink := &captureSink{frames: want}
	if err := RenderTimeline(conf, prepped.comp.Timeline, prepped.engine, sink, prepped.comp.StyleKeyframes, prepped.pLines, conf.CameraEnabled, nil); err != nil {
		t.Fatalf("RenderTimeline(%s): %v", scriptName, err)
	}
	if sink.closeArgs != 1 {
		t.Errorf("sink.Close called %d times, want exactly 1", sink.closeArgs)
	}
	return sink, pipelineFrameCount(conf, prepped.comp.Timeline), prepped.engine
}

func goldenConf(t *testing.T, scriptName string) *model.Project {
	t.Helper()
	conf := model.NewDefaultProject()
	conf.ScriptPath = filepath.Join("..", "..", "examples", scriptName)
	conf.AssetsDir = filepath.Join("..", "..", "assets")
	conf.Width, conf.Height, conf.FPS = 320, 180, 8
	conf.CameraEnabled = true
	conf.FreezeFrames = 8
	conf.DisableTranscript = true
	conf.Cursor = constant.CURSOR_HAND
	return conf
}

// compiled is the artifact set a frame render needs.
type compiled struct {
	comp     *TimelineCompilation
	pLines   []model.ProcessedLine
	duration float64
	engine   *render.Engine
}

// prepareExample runs parse -> TTS -> compile -> prepare against the mock TTS
// server: the same stages the render verb runs, minus the encode.
func prepareExample(t *testing.T, conf *model.Project) *compiled {
	t.Helper()
	mock := testutil.NewMockTTSServer()
	t.Cleanup(mock.Close)
	conf.TTSAddr = mock.URL
	conf.TTSCacheDir = t.TempDir()

	raw, err := os.ReadFile(conf.ScriptPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := script.SplitInlineWaits(script.Parse(string(raw)))

	client := tts.NewClient(conf.TTSAddr, conf.TTSCacheDir)
	wav, words, pLines, err := tts.OrchestrateTTS(client, lines, conf.Speed, conf.Voice)
	if err != nil {
		t.Fatalf("tts: %v", err)
	}
	duration, err := tts.GetWAVDuration(wav)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(t.TempDir(), "audio-*.wav")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(wav); err != nil {
		t.Fatal(err)
	}
	f.Close()

	comp, err := CompileTimeline(conf, words, pLines, duration, f.Name())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, finding := range comp.Findings.Sorted() {
		if finding.Severity == "error" {
			t.Errorf("compile finding: %s", finding)
		}
	}
	engine := newTestEngine(t, conf)
	if err := PrepareAssets(conf, engine, comp.Timeline, comp.TextJobs, comp.GenJobs, nil); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	return &compiled{comp: comp, pLines: pLines, duration: duration, engine: engine}
}

func newTestEngine(t *testing.T, conf *model.Project) *render.Engine {
	t.Helper()
	registry := render.NewCursorRegistry(filepath.Join(conf.AssetsDir, "cursors"))
	engine, err := render.NewEngine(conf.Width, conf.Height, conf.FPS, registry, constant.CURSOR_HAND)
	if err != nil {
		t.Fatal(err)
	}
	if hr, err := render.NewHandRenderer(filepath.Join(conf.AssetsDir, "hand.png"), conf.HandTipX, conf.HandTipY); err == nil {
		engine.Hand = hr
	}
	return engine
}

// TestSinkByteExactAgainstPreRefactorFrames is the regression gate for the sink
// refactor: the PNG bytes of frame 0 / mid / last of every example, captured from
// the pre-refactor renderer, must be reproduced exactly. image/png is
// byte-deterministic across processes, so any pixel change - including a frame
// buffer reused out from under a sink - shows up here instead of as a subtly
// different video.
func TestSinkByteExactAgainstPreRefactorFrames(t *testing.T) {
	goldenDir := filepath.Join("testdata", "frames")
	entries, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatalf("no golden frames (%v); see _scratch/goldencapture", err)
	}
	// golden name = <example>.zen.f<frame:06d>.png
	type golden struct {
		path  string
		frame int
	}
	byExample := map[string][]golden{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".png") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".png")
		idx := strings.LastIndex(base, ".f")
		if idx < 0 {
			t.Fatalf("unexpected golden name %q", e.Name())
		}
		frame, err := strconv.Atoi(base[idx+2:])
		if err != nil {
			t.Fatalf("bad frame number in %q: %v", e.Name(), err)
		}
		example := base[:idx]
		byExample[example] = append(byExample[example], golden{path: filepath.Join(goldenDir, e.Name()), frame: frame})
	}
	if len(byExample) == 0 {
		t.Fatal("no golden frames present")
	}

	for example, goldens := range byExample {
		t.Run(example, func(t *testing.T) {
			sort.Slice(goldens, func(i, j int) bool { return goldens[i].frame < goldens[j].frame })
			want := map[int]bool{}
			for _, g := range goldens {
				want[g.frame] = true
			}
			sink, total, _ := fullRender(t, example, want)
			if len(sink.got) != len(goldens) {
				t.Fatalf("sink received %d frames, golden set has %d", len(sink.got), len(goldens))
			}
			for i, g := range goldens {
				if g.frame >= total {
					t.Fatalf("%s: golden frame %d is past the end of a %d frame render", example, g.frame, total)
				}
				wantBytes, err := os.ReadFile(g.path)
				if err != nil {
					t.Fatal(err)
				}
				frame := image.NewRGBA(image.Rect(0, 0, 320, 180))
				copy(frame.Pix, sink.got[i].pix)
				var got bytes.Buffer
				if err := png.Encode(&got, frame); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got.Bytes(), wantBytes) {
					t.Errorf("%s: frame %d differs from the pre-refactor render (%d vs %d PNG bytes)",
						example, g.frame, got.Len(), len(wantBytes))
				}
			}
		})
	}
}

// TestSinkWhitelistSkipsRender: a sink that wants three frames must cause exactly
// three frames of pixel work - the whole point of frame addressing.
func TestSinkWhitelistSkipsRender(t *testing.T) {
	sink, total, engine := fullRender(t, "demo.zen", map[int]bool{3: true, 40: true})
	if total <= 0 {
		t.Fatalf("frame count = %d, want a positive total", total)
	}
	// Exactly the two requested frames may reach the pixel pipeline.
	if got := atomic.LoadInt64(&engine.Stats.TotalFrames); got != 2 {
		t.Errorf("engine rendered %d frames, want exactly 2: a skipped frame must cost nothing", got)
	}
	if len(sink.got) != 2 {
		t.Errorf("sink received %d frames, want 2", len(sink.got))
	}
}

// TestSinkOwnershipFramesSurviveWrite: the collector must not recycle a frame buffer
// while the sink is still using it, and must not hand the same buffer to two frames
// before the first write returns. Frames are compared against a second render of the
// same timeline, so a double-Put aliasing bug shows up as a pixel difference.
func TestSinkOwnershipFramesSurviveWrite(t *testing.T) {
	example := "v3_test.zen"
	want := map[int]bool{}
	for f := 0; f < 12; f++ {
		want[f] = true
	}
	first, _, _ := fullRender(t, example, want)
	if len(first.got) != len(want) {
		t.Fatalf("sink received %d frames, want %d", len(first.got), len(want))
	}
	second, _, _ := fullRender(t, example, want)
	for i := range first.got {
		if !bytes.Equal(first.got[i].pix, second.got[i].pix) {
			t.Errorf("frame %d differs between two renders of the same script: the render is not a pure function of the frame number", i)
		}
	}
}

// TestSinkWriteErrorStopsTheLoop: a failing sink must surface, not be logged and
// ignored.
func TestSinkWriteErrorStopsTheLoop(t *testing.T) {
	conf := goldenConf(t, "short_test.zen")
	prepped := prepareExample(t, conf)
	sink := &captureSink{frames: map[int]bool{0: true}, err: fmt.Errorf("disk full")}
	err := RenderTimeline(conf, prepped.comp.Timeline, prepped.engine, sink, prepped.comp.StyleKeyframes, prepped.pLines, conf.CameraEnabled, nil)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("RenderTimeline error = %v, want the sink error", err)
	}
	if sink.closeArgs != 0 {
		t.Errorf("a failed render must not close the sink as if it succeeded (closes=%d)", sink.closeArgs)
	}
}
