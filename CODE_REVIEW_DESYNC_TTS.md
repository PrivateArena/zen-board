## Root cause diagnosis

There are actually **two separate sync bugs** working against each other:

**Bug 1 — word timings are guesses.** `EstimateWordTimings()` in `tts/estimate.go` distributes the total WAV duration across words using a syllable-counting heuristic. Kokoro is a neural TTS — it doesn't speak at a uniform syllable rate, so these estimates drift badly, especially on longer lines, and the subtitle karaoke highlight falls out of sync.

**Bug 2 — audio/video length mismatch.** FFmpeg is invoked with `-shortest`, so it cuts at whichever stream ends first. If the estimated timing produces a `timeline.Duration` that's slightly shorter than the actual concatenated WAV, the video gets trimmed. If it's longer, there's trailing silence with no audio. Both cause perceived A/V desync.

**The fix:** Kokoro's ONNX model already produces audio samples sequentially per phoneme/token. You can capture the **per-token sample counts** right after `session.Run()` in `kokoro.go` and convert them to real word-level timestamps. zen-board then replaces `EstimateWordTimings` with those ground-truth timings entirely.

---

## Implementation plan

### Step 1 — zen-tts: expose per-token durations

In `internal/kokoro/kokoro.go`, after `session.Run()`, the output tensor `outputs[0]` contains the PCM samples. Kokoro internally processes one phoneme group per token. The trick is to get the **duration tensor** — Kokoro-82M exposes a second output `"durations"` (a float32 tensor of shape `[1, num_tokens]` in fractional seconds) when you add it to the output list.

Change `session` to also request `"durations"`:

```go
// in kokoro.go Synthesize()
session, err = ort.NewDynamicAdvancedSession(e.modelPath,
    []string{"tokens", "style", "speed"},
    []string{"audio", "durations"},  // add "durations"
    nil)

outputs := []ort.Value{nil, nil}  // second slot for durations
```

Then collect the word spans:

```go
durTensor := outputs[1].(*ort.Tensor[float32])
tokenDurs := durTensor.GetData()  // seconds per token

// Map back to words using the same tokenizer
wordTimings := shared.TokenDurationsToWordTimings(text, matchedVoice, tokenDurs, sampleRate)
```

Add `TokenDurationsToWordTimings` to `internal/shared` — it zips the token durations with the word boundaries already produced during tokenisation.

Return a new struct from `Synthesize()`:

```go
type SynthResult struct {
    Samples     []float32
    SampleRate  int
    WordTimings []WordTiming  // {Word, StartSec, EndSec}
}
```

### Step 2 — zen-tts: new API endpoint

In `server.go`, add a query-param branch:

```go
if r.URL.Query().Get("timestamps") == "1" {
    // return JSON envelope
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]any{
        "audio":    base64.StdEncoding.EncodeToString(wavBytes),
        "timings":  result.WordTimings,
        "duration": float64(len(samples)) / float64(sampleRate),
    })
    return
}
// existing path: raw WAV
```

This keeps the existing `/tts` endpoint fully backward-compatible.

### Step 3 — zen-board: new `TTSResult` and updated client

In `internal/tts/client.go`, add:

```go
type TTSResult struct {
    Audio      []byte
    Timings    []model.WordTiming
    Duration   float64
}

func (c *TTSClient) SynthesizeWithTimings(text string, speed float64, voice string) (*TTSResult, error) {
    // POST to /tts?timestamps=1
    // decode JSON envelope, base64-decode audio
    // return TTSResult
}
```

Graceful fallback: if the server returns non-JSON (old zen-tts without the feature), fall back to `Synthesize()` + `EstimateWordTimings`.

### Step 4 — zen-board: remove EstimateWordTimings from the hot path

In `cmd/zen-board/main.go`, replace:

```go
// OLD
duration, _ := tts.GetWAVDuration(chunk)
wordTimings := tts.EstimateWordTimings(line.Text, duration, currentTime)

// NEW
result, err := client.SynthesizeWithTimings(j.text, *speed, conf.Voice)
// result.Timings already have absolute timestamps baked in by zen-tts
// shift by currentTime if needed (or have zen-tts return relative times)
```

Keep `EstimateWordTimings` as a fallback, but it should no longer be the primary path.

### Step 5 — fix the audio/video length mismatch

Replace `-shortest` in `ffmpeg.go` with `-t <duration>` derived from the actual WAV length:

```go
// After concatenating WAVs, get exact duration
exactDuration, _ := tts.GetWAVDuration(finalAudio)

// Pass it to ffmpeg
args = append(args, "-t", fmt.Sprintf("%.6f", exactDuration))
```

This guarantees the video is exactly as long as the audio, independent of any timing estimates.

---

## What you don't need to change

- The WAV concatenation logic is fine
- The ASS subtitle generator is fine — it already uses `{\\k<centiseconds>}` karaoke tags, it just needs accurate input timings
- The render pipeline, FFmpeg pipe, and frame ordering are all correct
- The ONNX model doesn't need to be retrained or swapped

The `"durations"` output is available in Kokoro-82M's published ONNX model. If your specific model file doesn't expose it (older export), there's a good fallback: post-process the PCM output by running a simple energy envelope and locating silence boundaries between words — less accurate than phoneme-level timing but far better than the syllable heuristic.

Want me to write the actual Go code for any of these steps?