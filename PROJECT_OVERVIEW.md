# zen-board — Go

## Purpose
Video generation engine that compiles `.zen` script files into MP4 videos. Parses a declarative whiteboard-style DSL, orchestrates TTS audio, renders each frame via Go's `image` library with hand-animation, annotations, transitions, camera moves, and lower-thirds, then pipes frames through FFmpeg. Stack: Go 1.25, FFmpeg (external), oksvg for SVG rasterization.

## Architecture
```
.zen script  →  Parser  →  Timeline Compiler  →  Render Engine  →  FFmpeg pipe  →  MP4
                           ↑                        ↓
                        TTS Orchestrator        Frame Pool (goroutines)
```

## File Tree
```
zen-board/
├── main.go                    ← Entry point: CLI dispatch, orchestration loop
├── config.json                ← Project configuration (resolution, FPS, paths)
├── go.mod                     ← Module: zen-board, Go 1.25
├── examples/                  ← Sample .zen scripts
├── internal/
│   ├── assets/                ← Asset server, indexing, background removal, CLI, gen
│   ├── builder/               ← Timeline compilation + frame rendering loop
│   ├── ffmpeg/                ← FFmpeg subprocess pipe (raw frames → video)
│   ├── model/                 ← Core types: Project, FrameEvent, Timeline, Layout
│   ├── render/                ← Frame rendering: engine, hand, annotations, camera, text, masks
│   ├── script/                ← .zen DSL parser + preprocessor
│   ├── subtitle/              ← ASS subtitle generation
│   ├── svg/                   ← SVG rasterization + editing (oksvg)
│   └── tts/                   ← TTS synthesis client + orchestrator + timing
└── assets/                    ← Static assets (images, SVGs, prompts)
```

## Component Roles

| File / Module | Role | Key Exports |
|---|---|---|
| `main.go` | Entry point: loads config, parses `.zen`, runs render or CLI | `main`, `Run`, `loadConfig` |
| `internal/script/parser.go` | Parses `.zen` DSL into structured actions | `Parse`, `extractActions` |
| `internal/script/preprocessor.go` | Inline wait-splitting before timeline compilation | `SplitInlineWaits` |
| `internal/model/types.go` | Core data types for project, events, timeline | `Project`, `ScriptLine`, `FrameEvent`, `Timeline`, `ProcessedLine`, `WordTiming`, `DrawAction`, `SubtitleEvent` |
| `internal/model/layout.go` | Preset layout regions for drawing/annotation | `GetPresetLayout` |
| `internal/builder/timeline.go` | Compiles parsed script into frame-granular timeline, generates paint assets | `CompileTimeline`, `PrepareAssets`, `GeneratePaintAsset`, `TimelineCompilation` |
| `internal/builder/renderer.go` | Renders all frames sequentially from compiled timeline | `RenderTimeline` |
| `internal/render/engine.go` | Core rendering engine: frame-by-frame image compositing | `Engine`, `NewEngine`, `RenderFrame`, `LoadAsset`, `RegisterAsset`, `StartWorkers`, `RenderStats` |
| `internal/render/annotate.go` | Drawing annotations: arrows, highlights, comparisons, overlays, counters | `handleArrowEvent`, `handleHighlightEvent`, `handleCompareEvent`, `handleOverlayEvent`, `handleTransitionEvent`, `handleCounterEvent` |
| `internal/render/hand.go` | Animated hand sprite rendering with rotation and breath jitter | `HandRenderer`, `NewHandRenderer`, `Draw` |
| `internal/render/camera.go` | Camera pan/zoom via crop-and-scale | `CameraState`, `LerpCamera`, `CropAndScale`, `GetPresetViewport` |
| `internal/render/text.go` | Embedded font text rendering | `RenderText`, `pickEmbeddedFont` |
| `internal/render/easing.go` | Easing functions for smooth progress interpolation | `CalcProgress`, `EaseInOut`, `EaseOutCubic`, `EaseInOutCubic` |
| `internal/render/mask.go` | Alpha mask generation for reveal/wipe effects | `GenerateMask`, `GetFrontierPoint`, `MaskConfig` |
| `internal/render/pool.go` | Goroutine worker pool for parallel frame rendering | `RenderPool`, `NewRenderPool`, `FrameJob`, `RenderResult` |
| `internal/render/lower3rd.go` | Lower-third overlay rendering | `RenderLower3rdPanel` |
| `internal/render/draw_utils.go` | Masked image compositing helper | `DrawWithMask` |
| `internal/render/bg_utils.go` | Background style resolution | `ResolveStyleBg`, `ResolveStyleTextColor`, `ResolveStyleBgColor` |
| `internal/render/mask_utils.go` | Progress-based alpha mask application | `ApplyAlpha`, `ApplyEasedProgressMask` |
| `internal/ffmpeg/pipe.go` | FFmpeg subprocess pipe: feeds raw frames, manages audio | `Pipe`, `NewPipe`, `NewPreviewPipe`, `WriteFrame`, `Close`, `buildAudioArgs` |
| `internal/tts/client.go` | TTS HTTP client with caching and WAV concatenation | `TTSClient`, `NewClient`, `Synthesize`, `SynthesizeWithTimings`, `ConcatenateWAVs` |
| `internal/tts/orchestrator.go` | Multi-line TTS orchestration, word-timing compilation | `OrchestrateTTS`, `SynthJob`, `SynthResult` |
| `internal/tts/timing.go` | Syllable-count-based word-timing estimation | `EstimateWordTimings`, `countSyllables` |
| `internal/tts/wav.go` | WAV parsing, header creation, silent WAV generation | `GetWAVDuration`, `ParseWAVParams`, `CreateWAVHeader`, `CreateSilentWAV` |
| `internal/subtitle/ass.go` | ASS subtitle file generation from word timings | `GenerateASS` |
| `internal/svg/render.go` | SVG-to-image rasterization via oksvg | `RasterizeSVG`, `RasterConfig` |
| `internal/svg/edit.go` | SVG color modification and preprocessing | `ModifySVG`, `PreprocessSVG`, `Variant` |
| `internal/assets/server.go` | Web GUI server for asset management | `StartServer` |
| `internal/assets/cli.go` | CLI subcommand handler for asset operations | `RunCLI` |
| `internal/assets/indexer.go` | Asset index loading, saving, auto-indexing | `AssetIndex`, `LoadIndex`, `SaveIndex`, `AutoIndex`, `AssetEntry` |
| `internal/assets/gen.go` | AI-prompt-based paint asset generation (zen-lights) | `BatchGenerate`, `GenerateSingleAsset`, `PaintGenRequest`, `PaintGenResponse` |
| `internal/assets/bg.go` | Background removal: rembg, chroma-key, brightness-based | `ProcessBackgrounds`, `removeBgRembg`, `removeBgChromaKey` |

## Key Architectural Patterns

1. **Pipeline decomposition**: `.zen` → Parser → Timeline Compiler → Render Engine → FFmpeg pipe. Each stage produces a well-defined intermediate representation (actions → timeline → frames → video), enabling parallel development and testing of each stage independently.

2. **Frame-granular timeline**: `CompileTimeline` produces a flat array of `FrameEvent` structs indexed by frame number. The render engine iterates linearly, compositing events active at each frame. This avoids a scene graph while keeping per-frame composition O(active events).

3. **Goroutine worker pool**: `RenderPool` dispatches `FrameJob` structs across N workers. Jobs are independent per frame (no inter-frame dependencies), making the workload embarrassingly parallel with near-linear speedup on multi-core CPUs.

4. **TTS caching layer**: `SynthesizeWithTimings` wraps the raw synthesis call with a file-based cache keyed by text hash. Cache hits skip TTS API calls entirely, critical for iteration during development.

5. **Background removal strategies**: Three implementations (`removeBgRembg` external process, `removeBgLights` brightness-based, `removeBgChromaKey` near-white alpha) are selected by config, allowing trade-offs between quality and speed without changing the processing pipeline.

6. **Camera as post-process crop**: Instead of transforming render coordinates, `CropAndScale` pans/zooms by cropping from a higher-resolution render buffer and scaling down. This avoids re-rendering and simplifies coordinate math.

7. **SVG editing via XML manipulation**: `ModifySVG` uses the `etree` XML library to traverse SVG DOM and replace fill colors / add CSS classes, rather than re-rendering. `PreprocessSVG` normalizes `rgba()` to `rgb()` for oksvg compatibility.

## Dependencies

### Direct
| Package | Role |
|---|---|
| `golang.org/x/image` | Extended image processing (fonts, draw, freetype) |

### Indirect (transitive)
| Package | Role |
|---|---|
| `github.com/srwiley/oksvg` + `rasterx` | SVG rasterization to `image.Image` |
| `github.com/beevik/etree` | XML DOM manipulation for SVG editing |
| `golang.org/x/net` + `text` | HTTP, charset, text encoding |

### External runtime
| Dependency | Role |
|---|---|
| FFmpeg | Raw frame → H.264/ACC video muxing |
| Python + rembg (optional) | AI background removal |
| zen-lights (optional) | AI paint asset generation |
