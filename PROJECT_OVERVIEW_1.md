# zen-board — Project Architecture Overview

## Purpose

zen-board is a Go-based whiteboard video renderer. It takes a script file describing slides,
drawing actions, transitions, and narration, then produces an MP4 video with animated hand-drawn
content, text-to-speech audio, and ASS-format subtitles. It supports both a CLI pipeline and an
interactive web GUI with live preview.

---

## Directory Tree

```
zen-board/
|
+-- main.go                          # Entry point — dispatches Run()
|
+-- internal/
    |
    +-- assets/                      # Asset-management subsystem
    |   +-- bg.go                    #   Background removal (rembg, chroma-key, zen-lights)
    |   +-- cli.go                   #   CLI subcommand dispatch for assets
    |   +-- gen.go                   #   Paint asset generation via zen-lights API
    |   +-- indexer.go               #   Asset index (scan, load, save)
    |   +-- server.go                #   Web GUI server (HTTP, WebSocket, live preview)
    |
    +-- builder/                     # Timeline compilation and render dispatch
    |   +-- renderer.go              #   RenderTimeline — drives per-frame rendering
    |   +-- timeline.go              #   CompileTimeline, PrepareAssets, PaintGenRequest
    |
    +-- ffmpeg/                      # FFmpeg integration (video/audio encoding)
    |   +-- pipe.go                  #   Pipe (stdin/stdout FFmpeg process), NewPreviewPipe
    |
    +-- model/                       # Core domain types
    |   +-- types.go                 #   Project, ScriptLine, DrawAction, WordTiming, FrameEvent, Timeline, ...
    |
    +-- render/                      # Frame rendering engine
    |   +-- annotate.go              #   Arrow, highlight, compare, overlay, transition, counter events
    |   +-- camera.go                #   Camera state, viewport presets, CropAndScale
    |   +-- engine.go                #   Engine (main render orchestrator), RenderFrame, slide/lower3rd handlers
    |   +-- hand.go                  #   HandRenderer — animated hand sprite with rotation + breathing
    |   +-- lower3rd.go              #   Lower-third panel rendering
    |   +-- mask.go                  #   Alpha mask generation (reveal/sweep styles)
    |   +-- pool.go                  #   RenderPool — concurrent frame job worker pool
    |   +-- text.go                  #   Embedded font loading and text rendering
    |
    +-- script/                      # Script parsing
    |   +-- parser.go                #   Parse, extractActions
    |   +-- preprocessor.go          #   SplitInlineWaits
    |
    +-- subtitle/                    # Subtitle generation
    |   +-- ass.go                   #   GenerateASS — produces ASS subtitle format
    |
    +-- tts/                         # Text-to-speech synthesis
    |   +-- client.go                #   TTSClient, SynthesizeWithTimings, ConcatenateWAVs
    |   +-- orchestrator.go          #   OrchestrateTTS — parallel TTS + word-timing assembly
    |   +-- timing.go                #   EstimateWordTimings (syllable-based fallback)
    |   +-- wav.go                   #   WAV parsing, header generation, silent WAV creation
    |
    +-- testutil/                    # Test helpers
        +-- mock_tts.go              #   NewMockTTSServer (minimal WAV mock server)
```

---

## Data Flow

```
Script (.txt)  --[parser]-->  ProcessedLine[]  --[preprocessor]-->  Expanded lines
       |
       v
  CompileTimeline  --[model.Timeline]-->  FrameEvent[]
       |
       v
  RenderPool  --[parallel jobs]-->  Engine.RenderFrame
       |                                  |
  TTSClient <--(word timings)----   Draw hand / slides / lower3rds
       |                            /  arrows / highlights / masks
  OrchestrateTTS ------------>  Concatenated WAV
       |
       v
  FFmpeg Pipe  <--(raw RGBA frames + WAV)-->  MP4
       |
  ASS Subtitle  --[embedded]-->  Subtitled output
```

---

## Entry point: main.go

| Export | Role |
|--------|------|
| `main()` | Parses flags, dispatches to `Run()` |
| `Run()` | Two modes: (1) **daemon** — starts `StartServer()` with Web GUI; (2) **CLI** — `RunCLI()` for asset subcommands or script rendering pipeline |

Calls into: `internal/assets/cli.go`, `internal/assets/server.go`, `internal/builder/renderer.go`,
`internal/ffmpeg/pipe.go`, `internal/model/types.go`, `internal/script/parser.go`,
`internal/subtitle/ass.go`, `internal/tts/client.go`, `internal/tts/orchestrator.go`

---

## Component Breakdown

### internal/model/ — Core Types

| Type | Fields / Role |
|------|---------------|
| `Project` | Config: Dimensions (1920x1080), FPS (30), TTS URL, ColorMode, DefaultBgColor, VTTFonts, hand asset config |
| `ScriptLine` | Raw script line: text, delay, image file ref |
| `DrawAction` | Type (draw/highlight/arrow/erase...), points, color, size, duration |
| `WordTiming` | Word string, start/end time in seconds |
| `FrameEvent` | Union-like struct: holds slide background, draw actions, word timings, camera state, overlay/transition/counter config |
| `SubtitleEvent` | Text slice with start/end time |
| `Timeline` | Compiled timeline: events, subtitle events, total duration, WAV data |
| `ProcessedLine` | Parsed script line with resolved actions and timing |

Connects to: all other packages via these types.

### internal/assets/ — Asset Management

| File | Key Export | Role |
|------|------------|------|
| `server.go` | `StartServer(port, readyChan)` | HTTP/WebSocket server; serves Web GUI, accepts asset uploads, broadcasts render progress |
| `cli.go` | `RunCLI(args)` | Processes `assets` subcommands: index, generate, background-remove |
| `gen.go` | `BatchGenerate()`, `GenerateSingleAsset()` | Calls zen-lights API to generate PNG paint assets from text prompts; updates index |
| `indexer.go` | `LoadIndex()`, `SaveIndex()`, `AutoIndex()` | Manages `assetsindex.json`: scans directory, caches dimensions + background flag |
| `bg.go` | `ProcessBackgrounds()` | Background removal pipeline with three strategies (rembg, zen-lights, chroma-key) |

### internal/builder/ — Timeline Builder

| Export | Role |
|--------|------|
| `CompileTimeline(project, lines, ttsClient, ctx)` | ~965 lines. Builds full `Timeline` from parsed script: allocates FrameEvents, resolves background/foreground layers, inserts draw actions, handles slide changes, lower3rds, overlays, transitions, counters, word-timing alignment, camera animation, paint asset generation |
| `PrepareAssets()` | Pre-loads and caches image assets |
| `RenderTimeline(project, timeline, outputPath)` | Drives per-frame rendering: initializes `Engine`, creates FFmpeg `Pipe`, submits `FrameJob`s to `RenderPool`, writes RGBA frames, encodes WAV → MP4 |
| `TextRenderJob`, `GenRenderJob`, `StyleKeyframe`, `ChapterMarker` | Interim compilation types |

### internal/render/ — Rendering Engine

| File | Key Export | Role |
|------|------------|------|
| `engine.go` | `Engine`, `NewEngine()`, `RenderFrame()` | Central render orchestrator. Manages asset cache, composits layers: background → slides → hands → draw actions → lower3rds → camera crop |
| `hand.go` | `HandRenderer`, `Draw()` | Animated hand sprite renderer. Pre-computes rotated sprites at 5° increments (±30°). Applies breathing jitter per frame |
| `annotate.go` | `handleArrowEvent`, `handleHighlightEvent`, `handleCompareEvent`, `handleOverlayEvent`, `handleTransitionEvent`, `handleCounterEvent` | Drawing annotation primitives: bezier arrows with arrowheads, rounded-rect highlights, side-by-side comparison with labels, image overlays, wipe transitions, counter overlays with comma formatting |
| `camera.go` | `GetPresetViewport()`, `LerpCamera()`, `CropAndScale()` | Camera animation: preset viewports, smooth interpolation, output frame cropping |
| `mask.go` | `GenerateMask()`, `GetFrontierPoint()` | Alpha mask generation for reveal/sweep progress animations; frontier tracking for pencil-tip effects |
| `lower3rd.go` | `RenderLower3rdPanel()` | Lower-third title/subtitle panel with rounded rect, easing animations |
| `text.go` | `RenderText()` | Text rendering with embedded font selection |
| `pool.go` | `RenderPool`, `FrameJob`, `RenderResult`, `NewRenderPool()` | Concurrent frame rendering worker pool |

### internal/ffmpeg/ — FFmpeg Pipe

| Export | Role |
|--------|------|
| `Pipe` struct | Wraps stdin of a spawned FFmpeg process |
| `NewPipe(width, height, fps, audioPath, outputPath)` | Spawns FFmpeg with video + audio encoding args; returns pipe for raw RGBA frames |
| `WriteFrame(data)` | Writes raw RGBA pixel data to pipe stdin |
| `Close()` | Closes pipe and waits for FFmpeg to finalize |
| `NewPreviewPipe(...)` | Spawns a lower-latency FFmpeg for WebSocket preview streaming |

### internal/script/ — Script Parser

| Export | Role |
|--------|------|
| `Parse(text)` | Parses multi-line script into `[]ProcessedLine`. Handles `@asset`, `@wait`, `@delay`, `@image`, `@lower3rd`, `@counter` directives |
| `extractActions(lines)` | Extracts `DrawAction` per line from inline drawing directives |
| `SplitInlineWaits(lines)` | Pre-processor: splits lines at inline `{wait=N}` markers so timeline builder treats waits as separate audio segments |

### internal/tts/ — Text-to-Speech

| Export | Role |
|--------|------|
| `TTSClient`, `NewClient(url)` | HTTP client to TTS server (piper/OpenAI-compatible API) |
| `SynthesizeWithTimings(text, voice)` | Cached synthesis: returns `TTSResult` with audio + word timings |
| `OrchestrateTTS(lines, client)` | Runs synthesis for all script lines in parallel, concatenates WAVs, computes absolute word timings |
| `EstimateWordTimings(text)` | Syllable-based timing estimation fallback when server returns no word-level timestamps |
| `GetWAVDuration(data)`, `ConcatenateWAVs(fragments)` | WAV utility: duration extraction, multi-segment concatenation |
| `CreateSilentWAV(durationMs)` | Generates silent WAV for wait/delay periods |

### internal/subtitle/ — Subtitle Generation

| Export | Role |
|--------|------|
| `GenerateASS(events, width, height, color)` | Produces ASS subtitle track from `[]SubtitleEvent` for embedding in output MP4 |

---

## Architectural Patterns

### 1. Pipeline Pattern

The entire render path is a sequential pipeline:
```
Script → Parse → CompileTimeline → RenderPool.RenderFrame → Pipe.WriteFrame → FFmpeg
```
Each stage consumes the previous stage's output via well-defined types (`ProcessedLine`, `Timeline`, `FrameEvent`, raw RGBA).

### 2. Worker Pool (Concurrent Frame Rendering)

`RenderPool` in `internal/render/pool.go` dispatches frame rendering across goroutines. Each job is a
struct-bound closure that calls `Engine.RenderFrame()`. Results (RGBA byte slices) are collected
in order and fed to the FFmpeg pipe sequentially.

### 3. Event-Based Frame Composition

`Engine.RenderFrame(frameNumber, events, ...)` evaluates a `[]FrameEvent` slice active at the given
frame, composites layers in order:
1. Background (solid color or image)
2. Slide background image with pan/zoom camera
3. Hand sprite (animated, rotated, breathing)
4. Drawing annotations (arrows, highlights, comparisons)
5. Lower-third panels
6. Overlays and transitions (wipe, counter)

### 4. Asset Index + Cache

`internal/assets/indexer.go` maintains `assetsindex.json` as a metadata cache (dimensions,
background-presence flag). `Engine.LoadAsset()` caches decoded images in a map, lazy-loaded at
render time.

### 5. TTS Orchestration with Fallback

`OrchestrateTTS` parallelizes synthesis calls per script line. If the TTS server returns audio
without word-level timings, `EstimateWordTimings` provides syllable-count-based estimates.
Caching via file-based TTS result cache (`SynthesizeWithTimings`) avoids re-synthesis.

### 6. Dual-Mode Entry (CLI + Server)

`Run()` in `main.go` branches on CLI flags:
- `--daemon` or no subcommand → `StartServer()` (Web GUI + WebSocket preview)
- `assets <subcommand>` → `RunCLI()` (index, generate, background-remove)
- `render` or direct script path → full pipeline (parse → compile → render → encode)

### 7. Background Removal Strategies

`ProcessBackgrounds` in `internal/assets/bg.go` uses three strategies with fallback:
- **rembg** — external Python service (highest quality, slowest)
- **zen-lights** — local generative model (balance of speed/quality)
- **chroma-key** — simple near-white pixel detection with alpha blending (fastest)
