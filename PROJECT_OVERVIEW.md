<!-- codegraph-file-count: 38, last-commit: da95a77550e8c535e276cd3dc32b4226244ed340 -->

# zen-board — Whiteboard Video Generation Engine — Go 1.25

## Purpose
zen-board renders narrated whiteboard-explainer videos from a plain-text script. The Go pipeline parses the script into draw actions, synthesizes TTS narration with per-word timing against a remote zen-lights HTTP server, compiles an event timeline, rasterizes SVG assets into animated frames (cursor, hand, highlight/arrow/compare overlays, camera pans, reveal masks), and pipes frames plus mixed audio through an ffmpeg stdin pipe into an MP4. It also ships a Web GUI server and a CLI for asset generation, background removal, and indexing. Python's review_svg.py is a one-off SVG review helper.

## Architecture
```
script ─► script.Parse ─► OrchestrateTTS ─► CompileTimeline ─► Engine.RenderFrame ─► ffmpeg Pipe ─► MP4
          (ScriptLine)   (WAV+WordTiming)  (TimelineCompil.)    (per-frame RGBA)     (stdin)      (file)
```
TTS/asset HTTP → zen-lights server (external). Frame pipeline is single-process Go; encode is external ffmpeg.

## File Tree
```
zen-board/
├── main.go                  # entry point; Run() orchestrates full pipeline
├── go.mod                   # go 1.25, deps: etree, oksvg, rasterx, x/image
├── review_svg.py            # SVG review helper (Python)
└── internal/
    ├── assets/              # asset index (index.json), Web GUI server, bg-removal, AI generation
    ├── builder/             # CompileTimeline + RenderTimeline frame driver
    ├── ffmpeg/              # ffmpeg stdin pipe, filter_complex audio mix
    ├── model/               # domain types, defaults, preset layouts
    ├── render/              # frame engine, overlay handlers, cursor/hand sprites, masks, camera
    ├── script/              # script parser + inline-wait preprocessor
    ├── subtitle/            # ASS subtitle generation
    ├── svg/                 # SVG varianting + rasterization
    ├── tts/                 # TTS client, orchestration, WAV + timing estimation
    └── testutil/            # mock TTS HTTP server for tests
```

## Component Roles

### Backend (Go)

| File / Module | Role | LOC | Key Exports (with signatures) |
|---|---|---|---|
| main.go | Entry point; CLI dispatch and full pipeline orchestration | ~282 | `Run() error`; `loadConfig() *model.Project`; `getBinaryDir() string` |
| internal/model/types.go | Core domain types + default project | ~117 | `NewDefaultProject() *Project`; `Project`, `ScriptLine`, `DrawAction`, `WordTiming`, `FrameEvent`, `SubtitleEvent`, `Timeline`, `ProcessedLine` |
| internal/model/layout.go | Preset canvas geometries | ~26 | `GetPresetLayout(preset string, canvasW, canvasH int) (x, y, w, h int)` |
| internal/script/parser.go | Parse script text into lines + draw actions | ~263 | `Parse(input string) []model.ScriptLine`; `extractActions(line string) (string, []model.DrawAction)` |
| internal/script/preprocessor.go | Split inline wait periods into separate audio durations | ~124 | `SplitInlineWaits(lines []model.ScriptLine) []model.ScriptLine` |
| internal/tts/orchestrator.go | Synthesize all lines, concat audio, compile absolute word timings | ~162 | `OrchestrateTTS(client *TTSClient, lines []model.ScriptLine, speed float64, voice string) ([]byte, []model.WordTiming, []model.ProcessedLine, error)` |
| internal/tts/client.go | HTTP TTS client with file-based caching + WAV concat | ~233 | `NewClient(addr, cacheDir string) *TTSClient`; `Synthesize(text string, speed float64, voice string) ([]byte, error)`; `SynthesizeWithTimings(text string, speed float64, voice string) (*TTSResult, error)`; `ConcatenateWAVs(chunks [][]byte) ([]byte, error)`; `SaveWAV(path string, data []byte) error` |
| internal/tts/timing.go | Syllable-based word timing estimation (fallback) | ~83 | `EstimateWordTimings(text string, duration, startTime float64) []model.WordTiming`; `countSyllables(word string) int` |
| internal/tts/wav.go | WAV parse / header build / silence synth | ~148 | `GetWAVDuration(data []byte) (float64, error)`; `ParseWAVParams(data []byte) (WAVParams, error)`; `CreateWAVHeader(params WAVParams, dataSize uint32) []byte`; `CreateSilentWAV(params WAVParams, duration float64) []byte` |
| internal/builder/timeline.go | Compile event timeline; prepare/register assets | ~1193 | `CompileTimeline(conf *model.Project, allWordTimings []model.WordTiming, pLines []model.ProcessedLine, exactDuration float64, audioTmp string) (*TimelineCompilation, error)`; `PrepareAssets(conf *model.Project, engine *render.Engine, timeline *model.Timeline, textJobs []TextRenderJob, genJobs []GenRenderJob) error`; `GeneratePaintAsset(prompt string) (image.Image, error)` |
| internal/builder/renderer.go | Frame render driver; feeds engine output to ffmpeg pipe | ~189 | `RenderTimeline(conf *model.Project, timeline *model.Timeline, engine *render.Engine, pipe *ffmpeg.Pipe, styleKeyframes []StyleKeyframe, pLines []model.ProcessedLine, cameraEnabled bool, eventLog *render.EventLogger) error` |
| internal/render/engine.go | Per-frame compositor, worker pool, event dispatch | ~734 | `NewEngine(w, h, fps int, registry *CursorRegistry, defaultCursor string) (*Engine, error)`; `RenderFrame(frameNum int, events []model.FrameEvent, cam CameraState, style string) *image.RGBA`; `LoadAsset(name, path string) error`; `RegisterAsset(name string, img image.Image)`; `StartWorkers()`; `PrintStats()` |
| internal/render/annotate.go | Overlay/annotation event handlers (brush, arrows, highlight, compare, transition, counter) | ~609 | `handleArrowEvent(e *Engine, frameNum int, ev model.FrameEvent, buf *image.RGBA, visibility float64) (int, int, bool)`; `handleHighlightEvent / handleCompareEvent / handleOverlayEvent / handleTransitionEvent / handleCounterEvent(e *Engine, frameNum int, ev model.FrameEvent, buf *image.RGBA, visibility float64, ...)` |
| internal/render/cursor.go | Animated cursor sprites with rotation/scale keyframes | ~351 | `NewCursorRegistry(baseDir string) *CursorRegistry`; `Get(name string) *CursorPreset`; `CursorInterpolate(preset *CursorPreset, frameNum, fps int, baseX, baseY, angle int, handStyle string) []SpriteLayer`; `DrawCursorLayers(dst draw.Image, layers []SpriteLayer)` |
| internal/render/hand.go | Rotating hand/pen sprite with 5° rotation cache + breathing jitter | ~170 | `NewHandRenderer(path string, origTipX, origTipY int) (*HandRenderer, error)`; `Draw(dst draw.Image, x, y int, frame int, style string, angleDeg int)` |
| internal/render/mask.go | Reveal mask generation + frontier (pencil tip) point | ~183 | `GenerateMask(width, height int, progress float64, style string, config MaskConfig) *image.Alpha`; `GetFrontierPoint(width, height int, progress float64, style string, config MaskConfig) (int, int)` |
| internal/render/camera.go | Camera pan/zoom lerp + crop/scale | ~127 | `LerpCamera(start, end CameraState, t float64) CameraState`; `CropAndScale(src *image.RGBA, cam CameraState, targetW, targetH int, fastMode bool) *image.RGBA`; `GetPresetViewport(preset string, canvasW, canvasH int) CameraState` |
| internal/render/banner.go | Title banner panel rendering | ~125 | `RenderBannerPanel(canvasW, canvasH int, title, subtitle, colorHex string) *image.RGBA` |
| internal/render/text.go | Text rendering with embedded fonts | ~94 | `RenderText(text string, fontPreset string, size float64, isBold bool, fgColor color.Color) (image.Image, error)` |
| internal/render/eventlog.go | Frame-transition event logger (v2 JSONL format) | ~71 | `NewEventLogger(path string) (*EventLogger, error)`; `LogTransition(dir string, frame int, ev model.FrameEvent, fps int)`; `Close() error` |
| internal/render/pool.go | Frame render worker pool + job/result types | ~41 | `NewRenderPool(width, height int) *RenderPool`; `FrameJob`, `RenderResult` |
| internal/render/easing.go | Easing curves for animation | ~29 | `CalcProgress(frameNum, startFrame, endFrame int) float64`; `EaseInOut(t float64) float64`; `EaseOutCubic(t float64) float64`; `EaseInOutCubic(t float64) float64` |
| internal/render/bg_utils.go | Style → background/text/bg color resolution | ~52 | `ResolveStyleBg(style string) image.Image`; `ResolveStyleTextColor(style string) color.RGBA`; `ResolveStyleBgColor(style string) color.RGBA`; `ResolveStr(val, fallback string) string` |
| internal/render/draw_utils.go | Masked blit helper | ~16 | `DrawWithMask(dst draw.Image, r image.Rectangle, src image.Image, visibility float64)` |
| internal/render/mask_utils.go | Alpha/mask helpers | ~28 | `ApplyAlpha(c color.RGBA, visibility float64) color.RGBA`; `ApplyEasedProgressMask(mask *image.Alpha, easedProgress, visibility float64)` |
| internal/render/hand_utils.go | Hand angle/offset math | ~33 | `ComputeHandAngle(dx, dy int) int`; `HandOffset(dx, dy, renderW, renderH int) (int, int)` |
| internal/ffmpeg/pipe.go | ffmpeg stdin pipe; audio mix + subtitle + metadata args | ~222 | `NewPipe(outputPath, audioPath, assPath, bgmPath string, bgmVolume float64, width, height, fps int, duration float64, metadataPath string, fastMode bool) (*Pipe, error)`; `WriteFrame(data []byte) error`; `Close() error`; `NewPreviewPipe(width, height, fps int, audioPath, bgmPath string, bgmVolume float64, duration float64, metadataPath string) (*Pipe, error)` |
| internal/subtitle/ass.go | ASS subtitle generation | ~93 | `GenerateASS(timings []model.WordTiming, width, height int, events []model.SubtitleEvent) string` |
| internal/svg/edit.go | SVG varianting (fill/color) + rgba→rgb preprocessing for oksvg | ~169 | `ModifySVG(rawXML []byte, variants Variant) ([]byte, error)`; `PreprocessSVG(rawXML []byte) ([]byte, error)`; `ResolveCurrentColor(doc *etree.Document)`; `Variant` |
| internal/svg/render.go | SVG rasterization + cache key | ~46 | `RasterizeSVG(svgXML []byte, w, h int, cfg RasterConfig) (*image.RGBA, error)`; `CacheKey(rawXML []byte, variants Variant, w, h int) string` |
| internal/assets/indexer.go | Asset index.json load/scan/save | ~199 | `LoadIndex(assetsDir string) (*AssetIndex, error)`; `AutoIndex(assetsDir string) (*AssetIndex, error)`; `SaveIndex(assetsDir string, idx *AssetIndex) error` |
| internal/assets/bg.go | Background removal backends (rembg / lights / chroma-key) | ~197 | `ProcessBackgrounds(assetsDir, backend, lightsAddr string) error`; `removeBgRembg / removeBgLights / removeBgChromaKey(path string, ...) error` |
| internal/assets/gen.go | AI asset generation via zen-lights | ~189 | `BatchGenerate(assetsDir, promptsFile, style, lightsAddr string) error`; `GenerateSingleAsset(prompt, destPath, lightsAddr string) error` |
| internal/assets/server.go | Web GUI asset server | ~265 | `StartServer(assetsDir string, port int, lightsAddr string) error` |
| internal/assets/cli.go | `zen-board assets` subcommand CLI | ~128 | `RunCLI(args []string, defaultAssetsDir string) error`; `PrintUsage()` |
| internal/testutil/mock_tts.go | Mock TTS server returning a minimal WAV | ~34 | `NewMockTTSServer() *httptest.Server` |

### Tooling (Python)

| File / Module | Role | LOC | Key Exports (with signatures) |
|---|---|---|---|
| review_svg.py | Standalone SVG review helper script | ~125 | no exports (script) |

## Cross-References

| File | Called by / calls | Why it's central |
|---|---|---|
| main.go | Entry point; calls Parse, NewDefaultProject, OrchestrateTTS, CompileTimeline, RenderTimeline, NewPipe, RunCLI/StartServer | Top orchestrator — everything funnels through `Run()` |
| internal/builder/timeline.go | Called by main + renderer.go; calls LoadIndex, GetPresetLayout, RenderText, ModifySVG/PreprocessSVG, RasterizeSVG, LoadAsset/RegisterAsset, GeneratePaintEvent | Compile stage hub — owns CompileTimeline + PrepareAssets |
| internal/builder/renderer.go | Called by main.Run; calls engine.RenderFrame, pipe.WriteFrame/Close, eventLog.LogTransition | Bridges compiled timeline → frame engine → ffmpeg pipe |
| internal/render/engine.go | Called by renderer.go; calls handle*Event (annotate), CursorInterpolate, HandRenderer.Draw, RenderBannerPanel, CropAndScale, GenerateMask | Per-frame compositor and event dispatcher |
| internal/ffmpeg/pipe.go | Called by renderer.go + main; referenced by assets/indexer, bg, gen, timeline, tts/client | Encoding sink — owns all ffmpeg arg assembly |
| internal/tts/orchestrator.go | Called by main; calls SynthesizeWithTimings, ConcatenateWAVs, EstimateWordTimings, ParseWAVParams, CreateSilentWAV | TTS hub — produces audio + word timings that drive the timeline |

## Data Flow
```
zen-board (main.go)
   │  script text
   ▼
script.Parse ──────────────► model.ScriptLine[] ─► SplitInlineWaits
   │                                                    ▼
   └─► OrchestrateTTS ──HTTP POST──► zen-lights TTS ─► WAV + word timings
              │
              ▼
   CompileTimeline ──► TimelineCompilation (events, camera, style keyframes)
              │
              ▼
   PrepareAssets ── SVG ModifySVG/RasterizeSVG ─► Engine.LoadAsset/RegisterAsset
              │
              ▼
   RenderTimeline ─► Engine.RenderFrame (per frameNum) ─► *image.RGBA
              │                                                     │
              ▼                                                     ▼
   ffmpeg Pipe.WriteFrame (stdin) ◄──── mixed audio + ASS + metadata ──► MP4
```
External protocol: HTTP to zen-lights (TTS synthesis + asset generation, address from `--lights-addr`). Encode: ffmpeg binary via pipe. Asset management: Web GUI + CLI read/write `assets/index.json`.

## Key Architectural Patterns
1. **Staged pipeline orchestration**: `main.go Run()` sequences script parse → TTS synth → `CompileTimeline` → `RenderTimeline` → ffmpeg encode; each stage emits typed artifacts (`ProcessedLine`, `TimelineCompilation`, `*image.RGBA`) that decouple stages.
2. **Event-driven frame compositing**: `model.FrameEvent` drives `Engine.RenderFrame`; annotate.go dispatches per-event handlers (`handleArrowEvent`, `handleHighlightEvent`, `handleCompareEvent`, ...) that draw into the shared frame buffer.
3. **Deterministic parallel frame rendering**: `Engine.StartWorkers` + `RenderPool` process frame jobs in parallel; `RenderFrame` is a pure function of `frameNum` + events + camera, enabling reproducible renders.
4. **Sprite rotation caches**: `CursorRegistry`/`HandRenderer` precompute per-style rotation buckets at load time (`buildRotCache`, `snapAngle` → 5° steps) so per-frame sprite draws are cheap.
5. **SVG varianting + rasterization with cache keys**: `svg/edit.go ModifySVG` applies fill/color variants and `PreprocessSVG` converts rgba→rgb (oksvg limitation); `svg/render.go RasterizeSVG` + `CacheKey` rasterize once per variant; results enter the engine via `LoadAsset`/`RegisterAsset`.
6. **TTS with cached per-word timing**: `SynthesizeWithTimings` caches by `(text, speed, voice)` key and reads timing from the response; `timing.go` estimates syllable-based timings as fallback; orchestrator compiles absolute word timings.
7. **ffmpeg raw-video stdin pipe**: `Pipe` streams RGBA frames via `WriteFrame`, assembling input/filter_complex/map args in `buildAudioArgs` for audio + BGM mixing, ASS subtitles, and metadata; `NewPreviewPipe` provides a fast preview path.
8. **Preset-driven geometry**: `model/layout.go GetPresetLayout` and `render/camera.go GetPresetViewport` centralize canvas rect and camera framing so annotations and render stages share one geometry source.

## Read Triggers
| If you need to... | Open these files |
|---|---|
| Add a new script action keyword | internal/script/parser.go (extractActions), internal/model/types.go (FrameEvent/DrawAction) |
| Add a new render overlay / event type | internal/render/engine.go (RenderFrame dispatch), internal/render/annotate.go (handle*Event), internal/model/types.go (FrameEvent) |
| Change pipeline stage ordering / flags | main.go (Run), internal/builder/timeline.go, internal/builder/renderer.go |
| Change ffmpeg encode args or filters | internal/ffmpeg/pipe.go (buildAudioArgs, NewPipe, NewPreviewPipe) |
| Change TTS synthesis, caching, or timings | internal/tts/client.go, internal/tts/orchestrator.go, internal/tts/timing.go |
| Change canvas geometry / camera presets | internal/model/layout.go, internal/render/camera.go |
| Add an SVG variant attribute | internal/svg/edit.go (ModifySVG, Variant), internal/svg/render.go (CacheKey) |
| Change asset pipeline (bg removal / generation / indexing) | internal/assets/bg.go, internal/assets/gen.go, internal/assets/indexer.go |
| Change subtitle output | internal/subtitle/ass.go (GenerateASS) |
| Change fonts / text styling | internal/render/text.go (RenderText, pickEmbeddedFont), internal/render/banner.go |
| Change frame event logging | internal/render/eventlog.go (LogTransition) |

## Dependencies

### Go modules (go 1.25)
| Package / Module | Role | Version |
|---|---|---|
| github.com/beevik/etree | SVG XML DOM manipulation (varianting) | v1.7.0 |
| github.com/srwiley/oksvg | SVG decoding (used by RasterizeSVG) | 2022-10-21 commit |
| github.com/srwiley/rasterx | Rasterization backend for oksvg | 2022-07-30 commit |
| golang.org/x/image | Font faces, draw, color processing | v0.41.0 |
| golang.org/x/net | HTTP/2 (indirect) | 2021-11-18 commit |
| golang.org/x/text | Text transforms (indirect) | v0.37.0 |

### External services / binaries
| Component | Role |
|---|---|
| zen-lights | Remote HTTP server for TTS synthesis + AI asset generation (`--lights-addr`) |
| ffmpeg | External binary; encoding via stdin pipe (audio mix, subtitles, metadata) |
| rembg (optional) | Background-removal backend for asset processing |

## Build & Run
| Command | Purpose |
|---|---|
| `go build .` | Build the zen-board binary (single main.go at root) |
| `go test ./...` | Run tests (mock TTS server in internal/testutil) |
| `zen-board assets ...` | Asset CLI: index, background removal, AI generation (internal/assets/cli.go) |
| `zen-board ...` (server mode) | Web GUI asset server (StartServer) |
