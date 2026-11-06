package tts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"zen-board/internal/model"
)

// slowMockServer returns a TTS server that takes `delay` per request and whose
// audio duration encodes the text, so a mis-ordered concatenation is detectable.
func slowMockServer(delay time.Duration, requests *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(requests, 1)
		var req struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		time.Sleep(delay)
		// One second of audio per rune, so a mis-ordered concatenation is visible
		// in both the byte stream and the compiled line start times.
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(wavWithSamples(uint32(len(req.Text) * 24000)))
	}))
}

func wavWithSamples(samples uint32) []byte {
	params := WAVParams{Channels: 1, SampleRate: 24000, BitsPerSample: 16}
	dataSize := samples * uint32(params.BitsPerSample/8)
	return append(CreateWAVHeader(params, dataSize), make([]byte, dataSize)...)
}

func lines(n int) []model.ScriptLine {
	out := make([]model.ScriptLine, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, model.ScriptLine{Text: fmt.Sprintf("line %d has some words", i)})
	}
	return out
}

// TestOrchestrateTTSConcurrencyBoundedAndOrdered is the W0 acceptance test: the
// cold TTS stage must stop serialising on one request, and the concatenation
// order must not depend on which goroutine finished first.
func TestOrchestrateTTSConcurrencyBoundedAndOrdered(t *testing.T) {
	const nLines = 12
	var requests int32
	ts := slowMockServer(60*time.Millisecond, &requests)
	defer ts.Close()

	client := NewClient(ts.URL, t.TempDir())
	in := lines(nLines)

	// One at a time: nLines * delay.
	t0 := time.Now()
	serial, _, _, err := OrchestrateTTSConcurrent(client, in, 1.0, "v", 1)
	if err != nil {
		t.Fatalf("serial synthesis: %v", err)
	}
	serialDur := time.Since(t0)

	// Bounded concurrency: same bytes, materially faster.
	client2 := NewClient(ts.URL, t.TempDir())
	t1 := time.Now()
	parallel, _, pLines, err := OrchestrateTTSConcurrent(client2, in, 1.0, "v", 6)
	if err != nil {
		t.Fatalf("parallel synthesis: %v", err)
	}
	parallelDur := time.Since(t1)

	if len(serial) != len(parallel) {
		t.Fatalf("audio length changed with concurrency: %d vs %d", len(serial), len(parallel))
	}
	for i := range serial {
		if serial[i] != parallel[i] {
			t.Fatalf("audio bytes differ at %d with concurrency", i)
		}
	}
	if len(pLines) != nLines {
		t.Fatalf("got %d processed lines, want %d", len(pLines), nLines)
	}
	wantStart := 0.0
	for i, pl := range pLines {
		// The mock returns one second of audio per rune, so line i starts at the
		// summed length of every preceding line.
		if diff := pl.StartTime - wantStart; diff > 0.01 || diff < -0.01 {
			t.Errorf("line %d StartTime = %.3f, want %.3f (ordering broken)", i, pl.StartTime, wantStart)
		}
		wantStart += float64(len(in[i].Text))
	}
	if parallelDur*2 >= serialDur {
		t.Errorf("concurrency did not help: serial %v, 6-wide %v", serialDur, parallelDur)
	}
	t.Logf("serial=%v concurrent=%v requests=%d", serialDur, parallelDur, requests)
}

// TestOrchestrateTTSConcurrencyZeroFallsBackToDefault guards the flag plumbing:
// a zero or negative setting must not deadlock the semaphore.
func TestOrchestrateTTSConcurrencyZeroFallsBackToDefault(t *testing.T) {
	var requests int32
	ts := slowMockServer(0, &requests)
	defer ts.Close()
	client := NewClient(ts.URL, t.TempDir())
	if _, _, _, err := OrchestrateTTSConcurrent(client, lines(3), 1.0, "v", 0); err != nil {
		t.Fatalf("concurrency 0 must fall back to the default: %v", err)
	}
}
