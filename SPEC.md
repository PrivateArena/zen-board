# zen-board Technical Specification

## 1. System Overview & Scope

### Problem Statement & Purpose
zen-board renders narrated whiteboard-explainer videos from a plain-text script. The Go pipeline parses the script into draw actions, synthesizes TTS narration with per-word timing against a remote zen-lights HTTP server, compiles an event timeline, rasterizes SVG assets into animated frames (cursor, hand, highlight/arrow/compare overlays, camera pans, reveal masks), and pipes frames plus mixed audio through an ffmpeg stdin pipe into an MP4. It also ships a Web GUI server and a CLI for asset generation, background removal, and indexing.

### System Architecture
```
script text
    │
    ▼
main.go:Run()
    │
    ├─► script.Parse ──► []model.ScriptLine ──► SplitInlineWaits
    │
    ├─► OrchestrateTTS ──HTTP POST──► zen-lights ──► WAV + []model.WordTiming + []model.ProcessedLine
    │
    ├─► CompileTimeline ──► TimelineCompilation (events, camera, style keyframes)
    │       │
    │       └─► PrepareAssets ──► ModifySVG/PreprocessSVG ──► RasterizeSVG ──► Engine.LoadAsset/RegisterAsset
    │
    ├─► RenderTimeline ──► Engine.RenderFrame (per frameNum) ──► *image.RGBA
    │       │
    │       └─► ffmpeg Pipe.WriteFrame (stdin) ◄──── mixed audio + ASS + metadata ──► MP4
    │
    ├─► (optional) assets CLI: RunCLI ──► bg removal / AI gen / indexer
    └─► (optional) Web GUI: StartServer
```

Frame pipeline is single-process Go; encode is external ffmpeg binary.

## 2. Directory & Module Topology

### Folder Structure
```
zen-board/
├── main.go                        # entry point; Run() orchestrates full pipeline
├── go.mod                         # go 1.25, deps: etree, oksvg, rasterx, x/image
├── review_svg.py                  # SVG review helper (Python)
└── internal/
    ├── model/                     # domain types, defaults, preset layouts
    │   ├── types.go               # core structs: Project, ScriptLine, DrawAction, WordTiming, FrameEvent, etc.
    │   └── layout.go              # GetPresetLayout(preset, canvasW, canvasH) → (x,y,w,h)
    ├── script/                    # script parser + inline-wait preprocessor
    │   ├── parser.go              # Parse(input) → []ScriptLine; extractActions(line) → (string, []DrawAction)
    │   └── preprocessor.go        # SplitInlineWaits(lines) → []ScriptLine
    ├── tts/                       # TTS client, orchestration, WAV + timing estimation
    │   ├── orchestrator.go        # OrchestrateTTS(client, lines, speed, voice) → ([]byte, []WordTiming, []ProcessedLine, error)
    │   ├── client.go              # TTSClient; Synthesize, SynthesizeWithTimings, ConcatenateWAVs, SaveWAV
    │   ├── timing.go              # EstimateWordTimings, countSyllables
    │   └── wav.go                 # GetWAVDuration, ParseWAVParams, CreateWAVHeader, CreateSilentWAV
    ├── builder/                   # CompileTimeline + RenderTimeline frame driver
    │   ├── timeline.go            # CompileTimeline, PrepareAssets, GeneratePaintAsset
    │   └── renderer.go            # RenderTimeline
    ├── render/                    # frame engine, overlay handlers, cursor/hand sprites, masks, camera
    │   ├── engine.go              # Engine; NewEngine, RenderFrame, LoadAsset, RegisterAsset, StartWorkers, PrintStats
    │   ├── annotate.go            # handle*Event dispatchers; parsePoint/Region/HexColor helpers
    │   ├── cursor.go              # CursorRegistry; NewCursorRegistry, CursorInterpolate, DrawCursorLayers
    │   ├── hand.go                # HandRenderer; NewHandRenderer, Draw
    │   ├── mask.go                # GenerateMask, GetFrontierPoint, MaskConfig
    │   ├── camera.go              # LerpCamera, CropAndScale, GetPresetViewport, CameraState
    │   ├── banner.go              # RenderBannerPanel
    │   ├── text.go                # RenderText, pickEmbeddedFont
    │   ├── pool.go                # NewRenderPool; FrameJob, RenderResult
    │   ├── eventlog.go            # NewEventLogger, LogTransition, Close
    │   ├── easing.go              # CalcProgress, EaseInOut, EaseOutCubic, EaseInOutCubic
    │   ├── bg_utils.go            # ResolveStyleBg, ResolveStyleTextColor, ResolveStyleBgColor, ResolveStr
    │   ├── draw_utils.go          # DrawWithMask
    │   └── mask_utils.go          # ApplyAlpha, ApplyEasedProgressMask
    ├── svg/                       # SVG varianting + rasterization
    │   ├── edit.go                # ModifySVG, PreprocessSVG, ResolveCurrentColor, Variant
    │   └── render.go              # RasterizeSVG, CacheKey, RasterConfig
    ├── ffmpeg/                    # ffmpeg stdin pipe, filter_complex audio mix
    │   └── pipe.go                # NewPipe, NewPreviewPipe, WriteFrame, Close; buildAudioArgs
    ├── subtitle/                  # ASS subtitle generation
    │   └── ass.go                 # GenerateASS(timings, width, height, events) → string
    ├── assets/                    # asset index, Web GUI server, bg-removal, AI generation
    │   ├── indexer.go             # LoadIndex, SaveIndex, AutoIndex
    │   ├── bg.go                  # ProcessBackgrounds, removeBgRembg/Lights/ChromaKey
    │   ├── gen.go                 # BatchGenerate, GenerateSingleAsset
    │   ├── server.go              # StartServer
    │   └── cli.go                 # RunCLI(args, defaultAssetsDir), PrintUsage
    └── testutil/                  # mock TTS HTTP server for tests
        └── mock_tts.go            # NewMockTTSServer → *httptest.Server
```

### Component Breakdown

| Component | File | Role | Scale |
|---|---|---|---|
| Entry / CLI | `main.go` | Top orchestrator; CLI dispatch | ~282 LOC |
| Domain types | `internal/model/types.go` | Core structs | ~117 LOC |
| Layout presets | `internal/model/layout.go` | Canvas geometry | ~26 LOC |
| Script parser | `internal/script/parser.go` | Text → ScriptLine[] | ~263 LOC |
| Script preprocessor | `internal/script/preprocessor.go` | Inline wait splitting | ~124 LOC |
| TTS orchestrator | `internal/tts/orchestrator.go` | TTS hub | ~162 LOC |
| TTS client | `internal/tts/client.go` | HTTP TTS + caching | ~233 LOC |
| TTS timing | `internal/tts/timing.go` | Syllable-based fallback | ~83 LOC |
| WAV utils | `internal/tts/wav.go` | WAV parse / header / silence | ~148 LOC |
| Timeline builder | `internal/builder/timeline.go` | Event compilation + asset prep | ~1193 LOC |
| Frame renderer | `internal/builder/renderer.go` | Frame driver → ffmpeg pipe | ~189 LOC |
| Render engine | `internal/render/engine.go` | Per-frame compositor, worker pool | ~734 LOC |
| Annotation handlers | `internal/render/annotate.go` | Overlay/arrow/highlight/compare/transition/counter | ~609 LOC |
| Cursor sprites | `internal/render/cursor.go` | Animated cursor, rotation cache | ~351 LOC |
| Hand sprite | `internal/render/hand.go` | Rotating hand/pen | ~170 LOC |
| Mask generation | `internal/render/mask.go` | Reveal masks + frontier point | ~183 LOC |
| Camera | `internal/render/camera.go` | Pan/zoom lerp + crop/scale | ~127 LOC |
| Banner | `internal/render/banner.go` | Title banner panel | ~125 LOC |
| Text rendering | `internal/render/text.go` | Embedded font text | ~94 LOC |
| Render pool | `internal/render/pool.go` | Parallel frame worker pool | ~41 LOC |
| Event logger | `internal/render/eventlog.go` | Frame-transition JSONL logging | ~71 LOC |
| Easing curves | `internal/render/easing.go` | Animation easing | ~29 LOC |
| Style utils | `internal/render/bg_utils.go` | Background/text color resolution | ~52 LOC |
| Draw/mask utils | `internal/render/draw_utils.go`, `mask_utils.go`, `hand_utils.go` | Helpers | ~77 LOC |
| SVG edit | `internal/svg/edit.go` | Variant fill/color + rgba→rgb | ~169 LOC |
| SVG render | `internal/svg/render.go` | Rasterization + cache key | ~46 LOC |
| ffmpeg pipe | `internal/ffmpeg/pipe.go` | Encode sink, audio/subtitle/meta args | ~222 LOC |
| ASS subtitles | `internal/subtitle/ass.go` | Subtitle generation | ~93 LOC |
| Asset indexer | `internal/assets/indexer.go` | index.json CRUD | ~199 LOC |
| Background removal | `internal/assets/bg.go` | rembg/lights/chroma backends | ~197 LOC |
| AI asset generation | `internal/assets/gen.go` | zen-lights paint gen | ~189 LOC |
| Web GUI server | `internal/assets/server.go` | HTTP asset server | ~265 LOC |
| Asset CLI | `internal/assets/cli.go` | `zen-board assets` subcommands | ~128 LOC |
| Mock TTS | `internal/testutil/mock_tts.go` | Test fixture | ~34 LOC |

## 3. Component Design & Core Contracts

### Interfaces & Type Definitions

#### Domain Model (`internal/model/types.go`)
```go
type Project struct { ... }                // Top-level config container (canvas, style, assets, voice, etc.)
type ScriptLine struct { ... }             // Parsed line: text, wait, draw actions, voice/speed overrides
type DrawAction struct { ... }             // Inline draw directives (arrow, highlight, overlay, transition, etc.)
type WordTiming struct { ... }             // Per-word TTS timing: Word, Start, End
type FrameEvent struct { ... }             // Atomic frame-level event (type, x/y/width/height, text, color, etc.)
type SubtitleEvent struct { ... }          // ASS subtitle entry
type Timeline struct { ... }               // Ordered frame events + camera keyframes + style keyframes
type ProcessedLine struct { ... }          // Post-TTS line with audio boundaries
```

#### Builder Types (`internal/builder/timeline.go`)
```go
type TextRenderJob struct { ... }
type GenRenderJob struct { ... }
type StyleKeyframe struct { ... }
type ChapterMarker struct { ... }
type TimelineCompilation struct { ... }    // Events, camera keyframes, style keyframes, chapter markers
type PaintGenRequest struct { ... }        // Prompt, style for zen-lights paint gen
type PaintGenResponse struct { ... }       // Image bytes from zen-lights
```

#### Render Engine (`internal/render/engine.go`)
```go
type RenderStats struct { ... }            // Frame count, time, FPS, cache hits/misses
type Engine struct { ... }                 // Canvas size, asset registry, cursor registry, worker pool, event buffer
```

#### Render Primitives
```go
// cursor.go
type CursorRegistry struct { ... }
type CursorPreset struct { ... }
type CursorPreset struct { ... }
type TipOffset struct { ... }
type SpriteLayer struct { ... }
type AnimationKeyframe struct { ... }
type AnimationConfig struct { ... }

// hand.go
type HandRenderer struct { ... }

// mask.go
type MaskConfig struct { ... }

// camera.go
type CameraState struct { ... }           // X, Y, Width, Height (viewport rect)

// pool.go
type FrameJob struct { ... }
type RenderResult struct { ... }
type RenderPool struct { ... }

// eventlog.go
type EventLogger struct { ... }

// svg/edit.go
type Variant map[string]string             // Fill/color attribute overrides

// svg/render.go
type RasterConfig struct { ... }
```

#### TTS Layer (`internal/tts/client.go`, `orchestrator.go`, `wav.go`)
```go
type TTSClient struct { ... }             // addr, cacheDir, http.Client
type TTSResult struct { ... }             // Audio []byte, Duration float64
type CachedMetadata struct { ... }        // Cache entry metadata
type SynthJob struct { ... }
type SynthResult struct { ... }
type WAVParams struct { ... }             // SampleRate, NumChannels, BitsPerSample
```

#### Asset Layer (`internal/assets/indexer.go`, `bg.go`, `gen.go`)
```go
type AssetEntry struct { ... }            // Path, name, tags, hasBg, width, height
type AssetIndex struct { ... }            // Entries map, LastIndexed timestamp
type PaintGenRequest struct { ... }
type PaintGenResponse struct { ... }
```

### Subsystem Dispatch & IPC

#### Entry Point (`main.go`)
- `Run() error` — orchestrates full pipeline
- `loadConfig() *model.Project` — loads project config
- `getBinaryDir() string` — resolves binary-relative paths
- CLI dispatch: direct invocation of `RunCLI`, `StartServer`, or `Run()`

#### Script Pipeline
- `script.Parse(input string) []model.ScriptLine` — tokenizes plain-text script into `ScriptLine[]`
- `extractActions(line string) (string, []model.DrawAction)` — parses inline draw directives
- `SplitInlineWaits(lines []model.ScriptLine) []model.ScriptLine` — splits `wait` periods into separate audio durations

#### TTS Pipeline
- `OrchestrateTTS(client *TTSClient, lines []model.ScriptLine, speed float64, voice string) ([]byte, []model.WordTiming, []model.ProcessedLine, error)` — synthesizes all lines, concatenates WAVs, compiles absolute word timings
- HTTP protocol to zen-lights server (`--lights-addr`):
  - Request: `POST /tts` (JSON: `{"text", "speed", "voice"}`)
  - Response: `audio/wav` or JSON with audio bytes + timings
  - Caching: file-based, keyed by `(text, speed, voice)` in `cacheDir`

#### Timeline Compilation (`internal/builder/timeline.go`)
- `CompileTimeline(conf *model.Project, allWordTimings []model.WordTiming, pLines []model.ProcessedLine, exactDuration float64, audioTmp string) (*TimelineCompilation, error)` — builds event timeline
- `PrepareAssets(conf *model.Project, engine *render.Engine, timeline *model.Timeline, textJobs []TextRenderJob, genJobs []GenRenderJob) error` — pre-renders assets into engine
- `GeneratePaintAsset(prompt string) (image.Image, error)` — zen-lights paint generation

#### Frame Rendering (`internal/builder/renderer.go`, `internal/render/engine.go`)
- `RenderTimeline(conf *model.Project, timeline *model.Timeline, engine *render.Engine, pipe *ffmpeg.Pipe, styleKeyframes []StyleKeyframe, pLines []model.ProcessedLine, cameraEnabled bool, eventLog *render.EventLogger) error`
- `Engine.RenderFrame(frameNum int, events []model.FrameEvent, cam CameraState, style string) *image.RGBA` — pure function; compositor draws background, assets, overlays, cursor, hand, banner
- Event dispatch in `annotate.go`:
  - `handleArrowEvent`, `handleHighlightEvent`, `handleCompareEvent`, `handleOverlayEvent`, `handleTransitionEvent`, `handleCounterEvent`

#### ffmpeg Encode (`internal/ffmpeg/pipe.go`)
- `NewPipe(outputPath, audioPath, assPath, bgmPath string, bgmVolume float64, width, height, fps int, duration float64, metadataPath string, fastMode bool) (*Pipe, error)` — constructs ffmpeg args
- `Pipe.WriteFrame(data []byte) error` — streams RGBA frame to ffmpeg stdin
- `Pipe.Close() error` — finalizes encode
- `NewPreviewPipe(...)` — fast preview path
- `buildAudioArgs(...)` — assembles `-i` inputs and `-filter_complex` for audio mix + BGM + ASS subtitles + metadata

#### Asset Management (`internal/assets/`)
- `RunCLI(args []string, defaultAssetsDir string) error` — subcommands: `index`, `bg`, `gen`
- `StartServer(assetsDir string, port int, lightsAddr string) error` — HTTP GUI for asset browsing + generation

### API & Tool Schemas

#### zen-lights HTTP API (TTS)
```
POST /tts
Content-Type: application/json
Body: {"text": "...", "speed": 1.0, "voice": "en-US-A"}

Response 200: audio/wav
  or JSON: {"audio": "<base64>", "timings": [{"word":"...", "start":0.0, "end":0.5}, ...]}
```

#### zen-lights HTTP API (Asset Generation)
```
POST /generate
Content-Type: application/json
Body: {"prompt": "...", "style": "..."}

Response 200: image/png
```

#### Asset CLI (`internal/assets/cli.go`)
- `zen-board assets index [--dir]` — scan + update `assets/index.json`
- `zen-board assets bg --backend rembg|lights|chroma [--dir]` — remove backgrounds
- `zen-board assets gen --prompts prompts.txt --style <style> [--dir]` — batch generate

#### Web GUI (`internal/assets/server.go`)
- `GET /` — asset browser
- `POST /api/generate` — trigger AI generation (proxies to zen-lights)

## 4. State Management & Data Pipeline

### State Architecture
- **In-memory pipeline state**: `model.Project` loaded once at startup; `TimelineCompilation` holds all events; `Engine` holds loaded assets in `map[string]image.Image`
- **No persistent workspace state**: rendering is stateless per-frame; `RenderFrame(frameNum, events, cam, style)` is a pure function
- **Session/TTL**: no long-running session store; ffmpeg process lifetime is the run boundary

### Storage Mechanics
- **Asset index**: `assets/index.json` — JSON file mapping asset names to paths + metadata (width, height, hasBg, tags)
- **WAV cache**: file-based cache in `cacheDir/`, keyed by `(text, speed, voice)` hash; stores concatenated WAV + JSON metadata
- **Frame event log**: `eventlog.go` writes v2 JSONL format to disk for debug/transition logging
- **External storage**: ffmpeg output file; no database or ORM layer

### Data Flow Diagram
```
script.txt
    │
    ▼
[Parse] script/parser.go
    │  ScriptLine[] (text, wait, draw actions, overrides)
    ▼
[SplitInlineWaits] script/preprocessor.go
    │  ScriptLine[] with explicit wait boundaries
    ▼
[OrchestrateTTS] tts/orchestrator.go
    │  ├─► HTTP zen-lights → WAV chunks
    │  ├─► ConcatenateWAVs → single WAV
    │  └─► SynthesizeWithTimings / EstimateWordTimings → []WordTiming
    ▼
[CompileTimeline] builder/timeline.go
    │  ├─► PrepareAssets → RasterizeSVG → Engine.LoadAsset/RegisterAsset
    │  └─► TimelineCompilation (FrameEvent[], CameraKeyframes[], StyleKeyframes[])
    ▼
[RenderTimeline] builder/renderer.go
    │  per frameNum:
    │    Engine.RenderFrame(frameNum, events, cam, style) → *image.RGBA
    │    handle*Event → draw overlays/cursor/hand/banner/mask
    │    ffmpeg Pipe.WriteFrame(RGBA bytes)
    ▼
[ffmpeg] ffmpeg/pipe.go
    ├─► rawvideo stdin (RGBA frames)
    ├─► audio mix + BGM + ASS subtitles + metadata
    ▼
output.mp4
```

## 5. Execution, Security & Performance Boundaries

### Gatekeeping & Safety
- **Path safety**: `getBinaryDir()` resolves paths relative to the executable; no user-controlled absolute path injection in default config
- **No command sandbox**: ffmpeg is spawned as an external process; command args are constructed from internal config, not raw user input
- **No confirmation workflow**: single-shot pipeline; no interactive prompts during render
- **Asset CLI**: operates on designated `assetsDir`; no filesystem traversal outside that scope

### Token & Resource Optimization
- **WAV caching**: `SynthesizeWithTimings` caches by `(text, speed, voice)` to avoid redundant HTTP calls
- **SVG rasterization cache**: `RasterizeSVG` keyed by `CacheKey(rawXML, variants, w, h)` — assets rasterized once per variant/size
- **Cursor/hand rotation caches**: precomputed at load time in 5° buckets (±30°); per-frame draws are direct blits
- **Parallel frame rendering**: `Engine.StartWorkers()` + `RenderPool` processes frame jobs concurrently; `RenderFrame` is pure and deterministic
- **ffmpeg streaming**: frames piped via stdin to avoid intermediate files
- **Preview mode**: `NewPreviewPipe` provides lower-latency preview path

## 6. Architectural Decisions & Constraints

### Key Decisions (ADR style)

1. **Staged pipeline orchestration**
   - *Decision*: `main.go Run()` sequences script parse → TTS synth → CompileTimeline → RenderTimeline → ffmpeg encode
   - *Rationale*: Each stage emits typed artifacts (`ProcessedLine`, `TimelineCompilation`, `*image.RGBA`) that decouple stages and enable independent testing

2. **Event-driven frame compositing**
   - *Decision*: `model.FrameEvent` drives `Engine.RenderFrame`; `annotate.go` dispatches per-event handlers
   - *Rationale*: New overlay types are added as handler functions without touching the engine core

3. **Deterministic parallel frame rendering**
   - *Decision*: `RenderFrame` is a pure function of `(frameNum, events, cam, style)`; parallelized via `RenderPool`
   - *Rationale*: Enables reproducible renders and scales with CPU cores

4. **Sprite rotation caches**
   - *Decision*: `CursorRegistry`/`HandRenderer` precompute per-style rotation buckets at load time (5° steps)
   - *Rationale*: Eliminates per-frame rotation math for sprites; trades ~50-100ms load time for constant-time draws

5. **SVG varianting + rasterization with cache keys**
   - *Decision*: `svg/edit.go ModifySVG` applies fill/color variants; `svg/render.go RasterizeSVG` + `CacheKey` rasterize once per variant
   - *Rationale*: oksvg rasterization is expensive; caching avoids re-rasterizing identical SVGs

6. **TTS with cached per-word timing**
   - *Decision*: `SynthesizeWithTimings` caches by `(text, speed, voice)`; `timing.go` provides syllable-based fallback
   - *Rationale*: Reduces HTTP load and enables deterministic offline preview

7. **ffmpeg raw-video stdin pipe**
   - *Decision*: `Pipe.WriteFrame` streams RGBA frames; `buildAudioArgs` assembles `filter_complex` for audio + BGM + ASS subtitles + metadata
   - *Rationale*: Avoids intermediate video files; single-pass encode with all tracks

8. **Preset-driven geometry**
   - *Decision*: `model/layout.go GetPresetLayout` and `render/camera.go GetPresetViewport` centralize canvas rect and camera framing
   - *Rationale*: Annotations and render stages share one geometry source; eliminates layout drift

### Non-Functional Requirements & Exclusions

- **Performance target**: real-time or faster frame rendering; parallel frame pool bounded by CPU cores
- **Stateless guarantee**: frame rendering is pure; no mutable global state in render path
- **Stateful boundary**: TTS cache on disk; asset index persisted as JSON
- **Non-goals**: real-time collaborative editing, GPU acceleration, streaming output, multi-language UI, cloud sync
- **Dependency constraints**: single external binary (ffmpeg); remote zen-lights server required for TTS/AI generation; rembg is optional
