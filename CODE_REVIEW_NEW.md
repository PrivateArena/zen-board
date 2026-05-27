## Zen-Board: Current State Analysis

First, good news — you've already fixed several things the old `CODE_REVIEW.md` flagged. The current code has `os.CreateTemp`, `GenerateASS` takes `width/height`, `HandRenderer` has `TipX/TipY`, and `StartWorkers()` actually launches goroutines. The pipeline works end-to-end. Here's what actually needs fixing for production:

---

### 🔴 Critical Bugs (will cause wrong output or crashes)

**1. `[draw:tag]` position is hardcoded to `(100, 100)` for everything**
In `main.go` around the timeline builder:
```go
x, y := action.X, action.Y
if x == 0 && y == 0 {
    x, y = 100, 100  // Every image stacks here
}
```
Your parser correctly reads `[draw:pyramids@200,300]` coordinates and stores them — but the fallback means any `[draw:tag]` without coords (which is the common case in your demo) renders all images on top of each other at `(100,100)`. You need an auto-layout algorithm: a simple grid or a configurable default zone per draw call.

**2. `FrameEvent.Width/Height` is never set → no image scaling**
The render engine uses raw asset pixel dimensions — a `200×200` hand-drawn PNG will appear postage-stamp sized on a 1920×1080 canvas. Width/Height from the draw tag (`[draw:tag@x,y,w,h]`) is parsed and stored in `DrawAction.W/H` but never copied into the `FrameEvent` in `main.go`'s timeline builder. Add:
```go
Width:  action.W,
Height: action.H,
```
And in `engine.go`, scale the image if `ev.Width > 0`.

**3. Word timing is character-count based but subtitle uses karaoke `\k` tags**
`EstimateWordTimings` distributes duration by character count. "AI" gets almost no time, "civilizations" gets a lot. This is fine for rough sync, but combined with ASS karaoke (`{\k30}word`) the subtitle highlighting will be visibly wrong. This isn't just cosmetic — it also affects when `[draw:]` events fire, since draw trigger times come from these estimated timings.

**4. `ffmpeg` stderr is silently swallowed**
`NewPipe` starts ffmpeg but never captures `Stderr`. If encoding fails (missing codec, bad format, disk full), `pipe.Close()` returns the exit error but you get zero diagnostic info. Add:
```go
cmd.Stderr = os.Stderr
```

---

### 🟠 Correctness Issues (will produce bad output in real scripts)

**5. Pure-wait lines lose their draw actions**
In `main.go`, when `line.Text == ""`, the code only handles `WAIT:` actions and then `continue`s — any `[draw:]` tags on the same line as a `[wait:]` (e.g. `[wait:1.0][draw:robot]`) are silently dropped. You need to process draw actions on wait-only lines too.

**6. `ConcatenateWAVs` hardcodes the data chunk at byte offset 40**
The fix in `wav.go` (walking chunks) was applied to `GetWAVDuration` but `ConcatenateWAVs` in `client.go` still writes the output data size at `final[40:44]`. If the output WAV ever has a non-standard header (which can happen if the first chunk is non-standard), this corrupts the file. The walking logic from `GetWAVDuration` should be shared.

**7. `resultsMap` in main can grow unboundedly**
If a slow worker stalls, frames pile up in `resultsMap` with no cap. For a 10-minute video at 30fps that's 18,000 frames potentially in RAM simultaneously (~37GB at 1080p). You need backpressure — only allow N frames ahead of `nextFrame` before blocking the job feeder.

---

### 🟡 Missing Features Needed for Production

**8. No `[clear]` tag** — you can't wipe the canvas mid-video. Once an image is drawn it stays forever. This is essential for any script longer than ~60 seconds or multi-section content.

**9. No asset validation at startup** — missing assets are `log.Printf` warnings that get buried. The render silently skips the draw event and the hand appears to draw... nothing. You should collect all missing assets upfront and fail early with a clear list.

**10. No `[draw:]` without x/y auto-positioning** — since most users won't think in pixel coordinates, you need a layout system. Even a simple "next available slot in a 3-column grid" would make the tool actually usable for real scripts.

**11. Preview mode has no audio** — `NewPreviewPipe` feeds ffplay but there's no audio path wired in preview mode. The preview is silent, which makes it hard to verify timing.

**12. TTS is called sequentially** — each `client.Synthesize()` call blocks on HTTP. For a 20-line script this could take 30+ seconds before rendering even starts. These calls are independent and should be parallelized (fan-out, preserve order).

---

### 🟢 Quick Wins (1-2 hour fixes each)

- **`getBinaryDir`** — `strings.Contains(exe, os.TempDir())` is already there but on Linux `os.TempDir()` returns `/tmp` and go-build temp dirs are `/tmp/go-buildXXX` — this currently works but is fragile. Use `strings.HasPrefix(filepath.ToSlash(exe), filepath.ToSlash(os.TempDir()))`.

- **`[draw:tag@x,y]` vs `[draw:tag@x,y,w,h]`** — parsing is correct but the docs/example don't mention the 4-param form. Add it to `examples/demo.zen`.

- **`script/parser_test.go`** — there's no test for `[wait:]` interleaved with text, or for the `@coords` form. These are exactly the edge cases that break.

- **Subtitle font size** — hardcoded `60` in `ass.go`. At 1920×1080 this is fine but at your test resolution of `100×100` it's comically oversized. Derive from canvas height: `fontSize := height / 18`.

---

### Summary Priority Order

| Priority | Issue | Effort |
|---|---|---|
| P0 | Images all render at `(100,100)` — add auto-layout | M |
| P0 | `FrameEvent.Width/Height` never set — images not scaled | S |
| P0 | ffmpeg stderr suppressed — silent failures | XS |
| P1 | Draw actions dropped on pure-wait lines | S |
| P1 | `resultsMap` unbounded memory growth | S |
| P1 | TTS calls serialized — parallelize them | S |
| P1 | No `[clear]` tag | M |
| P2 | Missing asset upfront validation | S |
| P2 | Preview mode has no audio | S |
| P2 | Subtitle font size hardcoded | XS |
| P3 | Word timing accuracy (syllable heuristic or TTS alignment API) | M |

The core pipeline is solid and the parallel rendering architecture is correctly structured. The P0 items are what will make the tool visually broken for any real script — fix those and you'll have something genuinely usable.