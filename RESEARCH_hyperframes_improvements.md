# RESEARCH — HyperFrames → zen-board Improvement Plan

**Subject:** `heygen-com/hyperframes` (cloned to `/tmp/hyperframes`, HEAD `8be76fc`, release `v0.8.163`)
**Target:** zen-board (Go whiteboard renderer, `PROJECT_OVERVIEW.md`, 38 files, pipeline `script → TTS → CompileTimeline → Engine.RenderFrame → ffmpeg pipe`)
**Question:** what makes HyperFrames easy to *drive* and hard to break, and which of those properties zen-board should adopt to (a) be easier to use and (b) produce higher-quality videos.
**Method:** full clone + source/doc read (`README.md`, `docs/concepts/*`, `docs/reference/lint-rules.mdx`, `docs/guides/*`, `packages/*/src`, `skills/hyperframes/SKILL.md`). Every HyperFrames claim below is `[VERIFIED]` against a path in the clone. zen-board claims are `[VERIFIED]` against this repo.

---

## 1. What HyperFrames actually is `[VERIFIED]`

HyperFrames is **not** a better renderer than zen-board in the pixel sense — it is a *production loop* around a renderer.

| Layer | Package | What it does |
|---|---|---|
| Authoring contract | `packages/core` | HTML with `data-start` / `data-duration` / `data-track-index`, `class="clip"`, sub-compositions, declared variables. Composition = the project = the render input. One folder is the whole video, no export step (`docs/concepts/index.mdx`). |
| Determinism contract | `docs/concepts/determinism.mdx` | `t = floor(frame)/fps`, never a clock. Forbidden: `Date.now()`, `requestAnimationFrame`, unseeded `Math.random()`, mid-render fetch. Frame adapters must satisfy `init → seekFrame(n) (any order) → destroy`. |
| Validation | `packages/lint` (+ `cli/src/commands/{lint,check,validate,inspect,keyframes,snapshot,doctor,compare,grade-compare}`) | 135 documented lint rules (`docs/reference/lint-rules.mdx`), each with `severity`/`message`/`fixHint`/`line`/`column`/`snippet` (`packages/lint/src/types.ts:2-18`). `check` = lint + runtime + layout + motion + contrast in one gate (`checkPipeline.ts:1511-1605`). |
| Render | `packages/{engine,producer}` | Headless Chrome `beginFrame` capture per frame → ffmpeg. Quality tiers `draft/standard/high`, formats mp4/webm/mov/gif/png-sequence/hls, optional Docker pin, `benchmark` preset comparison (`docs/guides/rendering.mdx`). |
| Reuse | `registry/` (**210 blocks** + components + 8 examples) | `hyperframes add <block>`, `hyperframes catalog`, registry manifest JSON-schema'd (`docs/schema/registry-item.json`). |
| Media/AI | `packages/cli/src/{tts,whisper,beats,audio,media-use,capture}` | TTS providers, Parakeet/Whisper forced transcription + `.srt/.vtt` import, beat/onset/loudness analysis, LUFS normalization, voiceover carve (band-selective ducking). |
| Agent surface | `skills/` (21 skills), `packages/cli/src/telemetry`, `history`, `usage` | Router skill → intent interview → `BRIEF.md` / `STORYBOARD.md` / `SCRIPT.md` in the project folder; every command has `--json` "one agent-readable envelope"; project `history begin/undo --who <agent>` for auditable agent edits. |

**The transferable insight:** HyperFrames spends most of its code on *shortening and de-risking the iteration loop*, not on the compositor. Its own README order proves it: plan → write → lint → preview → render. zen-board today has the same five stages, but only two of them (write, render) exist as first-class surfaces.

---

## 2. Side-by-side, axis by axis `[VERIFIED]`

| Axis | HyperFrames | zen-board today | Verdict |
|---|---|---|---|
| Source of truth | one folder of HTML; agent, Studio, CLI, SDK all read/write the *same* files (`docs/concepts/index.mdx`) | one `.zen` text file + `config.json` | **Parity of philosophy** — zen-board's single-file DSL is already simpler and git-diffable. Do not chase HTML. |
| Timing model | explicit `data-start/data-duration`, author owns time | implicit: actions anchored to **word boundaries** from TTS (`script/parser.go`, `tts/orchestrator.go`) | zen-board's audio-first timing is a genuine differentiator (VideoScribe-like). Keep, but make it *inspectable* (§3.1) and add an absolute escape hatch (§3.6). |
| Determinism | contract documented and lint-enforced | structurally present (`RenderFrame(frameNum, events, cam, style)` pure; `RenderPool` parallel) but **undeclared and unenforced** | Cheap win: write the contract down + assert it in a test (§3.2). |
| Validation | ~120 lint rules + runtime + layout + contrast, machine-readable | **none.** Bad tag → silent wrong render; missing asset → compile error only | Biggest gap (§3.1). |
| Frame verification | `snapshot`, `inspect`, `keyframes`, `compare` (labeled A/B sheet PNG) | `-preview` (ffplay live) + `-eventlog` JSONL + ad-hoc `_scratch/print_timeline.go`, `_scratch/frames/` | Second biggest gap: no *cheap* way to look at 6 frames instead of rendering 6 minutes (§3.3). |
| Reuse | 210-item registry, `add`/`catalog` | `assets/index.json` (10 fields, `AssetEntry`, `tags` unused) + `[gen:]` | Good substrate, no reuse layer (§3.5). |
| Audio | transcription, LUFS normalize, carve/duck, beats, sfx | TTS + cached WAVs + `bgm_volume` flat mix; **`[sfx:]` is parsed then dropped** (documented in `API_zen-board-script-and-config.md`) | Real gap for perceived quality (§3.7). |
| Variants / batch | declared variables (7 types, Studio-bound) + `batchRender` | none (§3.4) | Medium gap, high leverage for "produce quality videos fast". |
| Agent interface | skills + `--json` envelopes + `history` + `usage` | none (plain `log.Printf` throughout, exit codes only) | High leverage; zen-board's whole DSL is agent-authored in practice (§3.8). |
| GUI | Studio: canvas + timeline + property edit, writes back to source | `assets/ui` server: asset index, bg-removal, AI generate only | Extend toward script-aware preview (§3.9). |

---

## 3. Improvement plan

Effort: **S** ≤ 1 day, **M** ≈ 2–4 days, **L** ≈ 1–2 weeks. Impact scored against "easier to use" (EU) and "better video" (VQ).

### 3.1 P0 — `zen-board lint` : static validation of `.zen` **[Effort M · EU ★★★★★ · VQ ★★★☆☆]**

- **Issue.** `parser.go` runs 20 independent regexes; a typo (`[drw:robot]`, `[draw:robo]`, `[zoom:Tl]`, `[text:Hello]` without quotes, unbalanced `@x,y,w,h`) produces **no error** — the tag either stays in the spoken narration (TTS reads "`[draw:robo]`" aloud) or resolves to the fallback auto-grid. Only missing asset files error, at compile time, after a full TTS pass. `REVIEW_...enrichment.md` and `IMPROVEMENTS_2.md` both record classes of silent-timing bugs.
- **Precedent.** `packages/lint`: rule = stable `code`, `severity`, human `message`, `fixHint`, 1-based `line`/`column`, `snippet`. Exit code gates render (`shouldBlockRender.ts`).
- **Fix.** New package `internal/diag` (named by responsibility, not `utils`):
  ```go
  // internal/diag/finding.go
  type Severity string // "error" | "warning" | "info"
  type Finding struct {
      Code, Message, FixHint, File string
      Line, Column int
      Snippet string
      Severity Severity
  }
  // internal/diag/lint.go
  func Lint(source, projectFile string) ([]Finding, error)
  ```
  Rules to ship first (all derivable from text + `index.json`, no render needed): `unknown_tag`, `tag_not_closed`, `unknown_asset`, `asset_bg_not_removed` (`has_bg == true` + `[draw:]`), `unknown_preset`, `unknown_layout_coord` (w/h ≤ 0, off-canvas), `speakable_tag_leftover` (a bracketed token the parser did not consume → will be read aloud), `draw_never_erased` (board overflow), `chapter_missing`/`duplicate_chapter_title`, `style_never_reset`, `reveal_duration_zero`, `move_target_missing`, `erase_target_never_drawn`, `zoom_before_draw`, `bgm_without_volume` (BGM at default `0.05`/`0.45` inconsistency), `no_narration_last_line`. Wire as `zen-board lint <file.zen>` with `--json`; make `Run()` run lint before TTS and **refuse on error** unless `--no-verify`.
- **Accept.** Golden test per rule in `internal/diag/lint_test.go`; `examples/*.zen` all lint clean; a deliberately broken fixture exits 1.

### 3.2 P0 — declare + enforce the determinism contract **[S · EU ★★☆☆☆ · VQ ★★★★☆]**

- **Issue.** `Engine.RenderFrame` is already a pure function of `(frameNum, events, cam, style)` (PROJECT_OVERVIEW §Key Patterns 3) — a property HyperFrames needed Chrome-internal machinery to obtain and zen-board gets for free. But nothing states it, so drift is invisible: `hand_utils.go` "breathing jitter", `Math/rand` anywhere, wall-clock in a handler, or a lazily-loaded asset silently breaks reproducibility and every cache built on it.
- **Precedent.** `docs/concepts/determinism.mdx` (rules + per-frame lifecycle) and `non_deterministic_code` / `requestanimationframe_in_composition` lint rules.
- **Fix.** (1) Add `docs/DETERMINISM.md` to zen-board: frame time is `t = frameNum / fps`, integer math; no `time.Now()`, no unseeded `rand` in `internal/render/**`; all assets resolved before frame 0 (already true via `PrepareAssets`). (2) Add `TestRenderFrameDeterministic` in `internal/render/`: same events → byte-identical buffers over two runs and over shuffled frame order. (3) Add `internal/diag` rule `nondeterministic_clock` scanning `internal/render/*.go` for `time.Now` / `rand.` outside an allowlist.
- **Accept.** Test green; any future jitter change must be seeded by `frameNum` (a `seed` field on `Project`, default `0`).

### 3.3 P0 — `snapshot` + `frames` (see output without paying for the encode) **[M · EU ★★★★★ · VQ ★★★★☆]**

- **Issue.** Today the only feedback is `-preview` (ffplay, real-time, must watch it) or a full render (`output.mp4`, minutes). Debugging layout requires `_scratch/frames/frame_001.png…` hand-rolled tooling and `_scratch/print_timeline.go` for the timeline. This is the single biggest cost per iteration.
- **Precedent.** `hyperframes snapshot` — "capture key frames as PNG screenshots for visual verification", with **range parsing + sampling** (`snapshotSampling.ts: parseSnapshotRange/sampleSnapshotRange`), and finding-linked crops; `inspect` — layout across the timeline; `compare` — "render composition variants into one labeled comparison sheet".
- **Fix.**
  ```
  zen-board snapshot <file.zen> [-o snapshots/] [-at 3.5,7,12.2 | -every 2s | -events] [--w 640]
  zen-board frames   <file.zen> -o frames/ [-at 3.5]        # single frame, full res, exact seek
  zen-board compare  <a.zen> <b.zen> -o sheet.png           # tiled PNG with per-cell labels
  ```
  `-events` samples at every `FrameEvent.StartFrame` + `EndFrame` (camera-settled variants), which is exactly the set a human wants to check. All three reuse `CompileTimeline` + `Engine.RenderFrame` and skip ffmpeg entirely (write PNG via `image/png`) — no audio stage needed if the TTS cache is warm.
- **Accept.** `zen-board snapshot examples/v3_test.zen -events` produces ≤ 40 PNGs in < 5 s at 640 px, including one per chapter and one per camera settle.

### 3.4 P1 — project files: `BRIEF.md` / `SCRIPT.md` / variables + batch **[M · EU ★★★★☆ · VQ ★★★☆☆]**

- **Issue.** `config.json` is a global singleton read from the binary dir or cwd (`loadConfig()`), so two videos in one repo fight each other; there is no per-project identity, no variant mechanism, and no planning artifacts. Every re-run of a hand-tuned script is edit-then-rerender-everything.
- **Precedent.** `docs/concepts/variables.mdx` — typed declared variables (`string|number|color|boolean|enum|font|image`) with `label`/`description`/`default`, bound by `data-var-text` / `data-var-src`; Studio previews overrides; `batchRender` produces approved variants from one source. Plus project files `BRIEF.md` / `STORYBOARD.md` / `SCRIPT.md` as durable decisions across sessions (`docs/concepts/index.mdx`).
- **Fix.**
  1. **Project file beats flags for identity:** `-project <dir>` loads `<dir>/project.json` (rename of today's schema, keep `config.json` fallback), so output path, assets dir, voice, board style default per project.
  2. **Variables in the DSL:** `[var:<id>:<default>[:<type>]]` declarations + `{id}` interpolation inside `[text:"{title}"]`, `[chapter:"{topic}"]`, `[banner:"{name}"]`, and asset refs. CLI: `-var title="Zen Board V3" -var-json vars.json`.
  3. **Batch:** `zen-board render --variants variants.json -o out/` looping the variable sets, reusing the compiled timeline and TTS cache (voice per variant, text per variant, geometry identical).
  4. `zen-board init <name>` scaffolding `project.json`, `SCRIPT.md`, `assets/`, `examples/hello.zen`.
- **Accept.** One `.zen` renders 3 branded variants with identical timing; changing a name is a one-line JSON edit, not a script rewrite.

### 3.5 P1 — scenes (sub-compositions) + a scene registry **[M · EU ★★★★☆ · VQ ★★★★☆]**

- **Issue.** A 3-minute video is one flat `.zen` file with hand-numbered `[wait:]` padding and a 3×2 auto-grid (`API_...md`: "grid resets on `[clear]`/`[erase:*]`/`[transition:]`"). Reusing an intro/outro means copy-paste; drift between copies is invisible. `assets/index.json` has `tags` and `style` fields that nothing consumes (`indexer.go:21-31`).
- **Precedent.** Nested compositions via `data-composition-src` + own start/duration/track (`docs/concepts/index.mdx`); **210 blocks** installed with `hyperframes add` from a JSON-schema'd registry (`registry/registry.json`, `docs/schema/registry-item.json`); lint caps file size and track density (`composition_file_too_large`, `timeline_track_too_dense`) — reuse is *enforced* by making monoliths lint-warned.
- **Fix.**
  1. `[include:<path.zen>[:<preset>]]` — a scene file with its own local grid/camera scope, inlined at compile time with an absolute time offset; `[macro:<name>]` resolving from `assets/scenes/<name>.zen`.
  2. `zen-board scenes <list|add|new|remove>` backed by a `scenes/registry.json` (same shape discipline as HyperFrames' `registry-item.json`: `name`, `title`, `description`, `presets_used`, `assets_required`, `files`). Ship 6–10 starter scenes cut from the real examples: `title-card`, `problem-statement`, `two-option-compare`, `stat-reveal`, `demo-shot`, `outro-cta`.
  3. Reuse the existing asset tags: `zen-board assets list -tag vector` and lint rule `asset_style_mixed` (vector + realistic in one scene is a visible quality drop; `[gen:]` `style` param is already in `GenerateSingleAsset`).
- **Accept.** `examples/advanced.zen` rewritten to 4 `[include:]` lines + narration with identical output frames.

### 3.6 P1 — inspector surface: `info` / `timeline` / `stats` **[S · EU ★★★★☆ · VQ ★★☆☆☆]**

- **Issue.** To answer "when does the robot get drawn / how long is the video / where is the dead air", you must render. `PROJECT_OVERVIEW.md` puts this state in `TimelineCompilation` and throws it away; only `-eventlog` (JSONL of transitions) survives, and `_scratch/print_timeline.go` is an untracked one-off.
- **Precedent.** `hyperframes timeline` — "Print the project's tracks and clips", explicitly the alternative to reading source; `info` — project metadata incl. orientation/duration/size; `check` prints per-section counts.
- **Fix.** Promote to first-class: `zen-board info <file.zen>` (duration, word count, est. TTS seconds, event count per type, chapters, camera moves, missing assets) and `zen-board timeline <file.zen> [--json]` (per-line start/end, per-event `StartFrame/EndFrame`, gap detection). Both run **TTS-cache-only** (no render). Add `zen-board stats` output at end of render: wall clock per stage (TTS / compile / render / encode), frames, fps achieved, cache hit rate — HyperFrames does this with `LintTimings` (`groupMs`, `slowestRule`) and render telemetry including CPU load.
- **Accept.** `timeline --json` is stable enough to be the input for `snapshot -events` and for external agents.

### 3.7 P1 — audio pass: real `[sfx:]`, ducking that isn't a constant, loudness normalization, real alignment **[M · EU ★★☆☆☆ · VQ ★★★★★]**

Audio is where whiteboard videos most obviously read as "amateur", and zen-board currently has one flat multiplier.

- **Issue (verified).** `buildAudioArgs` (`internal/ffmpeg/pipe.go:16-62`) supports exactly narration + looped BGM at a constant `bgm_volume`; `[sfx:<name>]` is parsed (`sfxRegex`) and dropped (`API_...md`: "no-op in the timeline builder"). Word timing comes from the TTS server or a **syllable estimate** fallback (`tts/timing.go: EstimateWordTimings`) with no verification, so a mis-timed estimate silently shifts every downstream action. No loudness control: output LUFS depends on the voice model and BGM file.
- **Precedent.** `hyperframes transcribe` (Parakeet → Whisper fallback, word-level transcript, imports `.srt/.vtt`), `normalize-audio` ("match one authored audio clip's loudness to another using integrated LUFS"), voiceover **carve** (takes "only the bands the voice occupies out of the music", `docs/guides/voice-and-audio.mdx`), `beats` / `audio` (onsets, hits, loudness, silence in film time), `audioFx*` presets and automation lanes.
- **Fix, in order of payoff:**
  1. **Implement `[sfx:]`**: `assets/sfx/<name>.wav` + `amix`/`adelay` at the trigger frame in `buildAudioArgs`. Cheap, huge perceived-quality lift on the reveal moments zen-board already knows about.
  2. **Sidechain duck** as a filter chain (`sidechaincompress` narration→BGM) exposed as `[bgm:<file>:<mode>]`, `mode ∈ {constant(legacy), duck, duck-soft}`; keeps `bgm_volume` as ceiling. Better than HyperFrames' carve for a Go port, far better than today's constant.
  3. **Two-pass loudness normalize** on the mixed track: measure with `ebur128` (`ffmpeg -filter_complex loudnorm=print_format=json -f null -`) into a cache file, then apply `loudnorm=I=-16:TP=-1.5:LRA=11` during the real encode. Target baked into `project.json`, not hardcoded.
  4. **Timing verification:** `-align auto` re-transcribes the concatenated narration (whisper.cpp / Parakeet if installed, or reuse the zen-lights server) and *diffs* against the TTS-provided/estimated `WordTiming`. Report drift per word; lint rule `word_timing_estimated` when the fallback path was used. This turns "my drawing appears half a second early" from a mystery into a measurable diff.
  5. Per-line `-16 LUFS` normalization when concatenating multi-voice scripts (voice changes today would produce level jumps).
- **Accept.** Output integrated loudness within ±1 LU of target; `[sfx:whoosh]` lands within one frame of the reveal start; `lint` warns when any word's estimated timing drifts > 80 ms from the aligned value.

### 3.8 P1 — agent-first CLI: `--json` envelopes, `--explain`, exit codes, project history **[M · EU ★★★☆☆ · VQ ★★★☆☆]**

- **Issue.** Output is `log.Printf("[Config] Loaded from: %s", …)` and progress chatter (`"  SVG cache hit: %s"`), i.e. human text an agent must regex against. Failures are partial: a missing asset errors mid-`PrepareAssets`, after TTS money is spent. `-freeze`, `-fast`, `-preview` semantics are discoverable only by reading `main.go`.
- **Precedent.** Every command takes `--json` for "one agent-readable envelope"; commands carry `examples` for in-CLI help (`commands/_examples.ts`, `hyperframes docs`); `history begin --who claude --label …` / `history undo --who claude` / `history --since mine` record who changed what — explicitly designed for a human + agent sharing one project; render failure messages name *how* the tool ended and keep its last line (commit `c03532b`).
- **Fix.**
  1. Global `--json`: one envelope `{ ok, command, version, findings[], artifacts[], stages{tts,compile,render,encode}, meta }` for `lint|check|info|timeline|render|snapshot`. Machine messages stay stable strings; human text is derived from the same struct.
  2. Errors: "what failed + the one line that says why + the exact fix" (HyperFrames' ffmpeg message style), with the `.zen` **line:column** — requires the parser to record byte offsets per action (`extractActions` already knows positions; persist them on `DrawAction`).
  3. `zen-board doctor`: ffmpeg/ffprobe present + version, `assets_dir` writable, TTS/paint server reachable (`-tts`, `-lights-addr`), fonts embedded, `index.json` parseable, disk space, `-w/-h` even. With `hint` per failed check (`doctor.ts` `CheckResult{ok, detail, hint}`).
  4. `zen-board explain <action>` printing the DSL reference from an in-binary table (today the DSL spec lives only in a Markdown file, and `[sfx:]`/`[watermark:]` are inconsistently documented).
  5. **Project history:** record each render (`renders/history.json`: script hash, config, voice, duration, output size, lint summary) and `zen-board history [--since mine]` / `zen-board revert <render-id>`. HyperFrames' `check`/`snapshot` both attach artifacts to history revisions and record attribution — same idea, trivial to do with a JSONL sidecar.
  6. Ship a `skills/zen-board/SKILL.md` + `AGENTS.md` template (HyperFrames scaffolds `templates/_shared/AGENTS.md` into every project so agents can't miss the contract): DSL grammar, action table, "lint → snapshot -events → render" loop, and the determinism rules.
- **Accept.** A coding agent that never reads Go can go from brief to passing `lint` to inspected `snapshot` using only `--json` output.

### 3.9 P2 — from asset UI to a real Studio-lite **[L · EU ★★★★☆ · VQ ★★★☆☆]**

- **Issue.** `assets/server.go` (265 LOC, 6 routes) manages the *library* only. Editing timing means editing text and re-rendering. HyperFrames' pitch — "Studio is already a full editor: click anything on canvas, edit text, drag and trim clips on the timeline, every edit saves to the project" (`skills/hyperframes/SKILL.md` §6) — is the target state, but without a browser engine.
- **Fix.** Extend the existing server (do not add a JS framework; keep it vanilla per project conventions):
  - Load the compiled `timeline --json`; render per-event thumbnails from the §3.3 snapshot path; show chapter/event rails.
  - Click an event → jump to that `.zen` line; edit `[wait:]`/reveal durations/preset in-place → **write back to the same `.zen` file** (single source of truth; git-diffable, so an agent and a human can take over afterwards — the exact property HyperFrames protects).
  - "Preview from here" = seek-render 1–2 s around the cursor rather than the whole film.
  - Style-safety constraints surfaced as a checklist (HyperFrames: "lay out a project so it reads well: caption track, tracks per element kind, safe zones" — `/hyperframes-studio`).
  - Optional later: a `tview` TUI for `timeline`/`snapshot` browsing (project convention prefers `tview` for TUI tooling).
- **Accept.** Round-trip: nudge one `[wait:]` from the UI, save, `git diff` shows only that token, `lint` still passes.

### 3.10 P2 — render pipeline hardening & output tiers **[M · EU ★★☆☆☆ · VQ ★★★★☆]**

- **Issue.** Two modes only: `-fast` (half-res + `ultrafast`, crf 18) and default (`-preset fast`, `-crf 18`, hardcoded). No per-chapter resume, and `-freeze` appends trailing freeze frames as a special-case duration (`main.go` step 5) rather than a first-class outro.
- **Precedent.** `--quality draft|standard|high` + explicit `--fps`/`--crf`/`--video-bitrate`; png-sequence output as a handoff/compositing escape hatch; `benchmark` — "render with preset fps/quality/worker configs and compare speed and file size"; Docker to pin the external toolchain because "the platform stops being an input" (`determinism.mdx`).
- **Fix.**
  1. `Quality` struct in `project.json`: `draft|standard|high` → `(scale, x264 preset, crf, threads)`; `RenderProfile` passed to `ffmpeg.NewPipe` instead of the `fastMode bool`. Keep explicit `-crf`/`-preset` overrides.
  2. `-o frames/` png-sequence + a remux step (parity with HyperFrames' png-sequence), enabling external polish and frame-level A/B diffs.
  3. **Resume/cache:** hash `(script, config, voice, seed)` → `renders/<hash>/manifest.json`; if a previous run's hash matches, reuse TTS + raster cache (extend today's in-process `svgCache` to disk keyed by `svg.CacheKey`, already computed at `timeline.go:465`).
  4. `zen-board benchmark`: same script at the quality tiers → speed/size table (uses the same harness as §3.6 stats).
  5. Toolchain pinning for reproducibility: record `ffmpeg -version` and voice model ID into the render manifest; warn on mismatch (a Go-scale analogue of the Docker pin, without Docker).
  6. Turn `freeze_frames` into `[outro:<title>[:<dur>]]` — a designed end card instead of a static hold.
- **Accept.** `--quality draft` ≥ 3× faster than default at visually-acceptable 640–960 px; an unchanged script re-render is ≥ 2× faster with a warm cache and says so in `stats`.

### 3.11 P2 — motion and composition quality (targeted, not a rewrite) **[M · EU ★☆☆☆☆ · VQ ★★★★☆]**

HyperFrames' motion model is HTML/GSAP and is out of scope, but three of its *principles* port directly to the existing event system:

- **Easing is first-class and seek-safe.** `render/easing.go` has 3 curves; every overlay handler (`annotate.go`) animates with `CalcProgress`. Add `ease-out-back`, `ease-in-out-quint`, `linear`, `steps`, plus a per-action `:ease=<name>` token — the cheapest visible polish on arrows, banners, and camera. Add lint rule `motion_over_animated_property` analogue: warn when a `[zoom:]` settles on the same frame a reveal starts (already half-handled by `adjustForZoom`).
- **Camera is a layer, not a flag.** `camera_enabled` is a global bool; a real explainer cuts: wide → punch-in on the object → hold → out. `[cam:punch:<asset>[:<scale>]]` / `[cam:wide]` / `[cam:hold:<sec>]` compiled into `StyleKeyframe`-adjacent camera keyframes, with `LerpCamera` reused. Verify against `camera_test.go`.
- **Text is a system, not one font.** `render/text.go` exposes a single embedded `sans` (and `IMPROVEMENTS_2.md` §8 flagged Linux-only font paths, since fixed by embedding). Add a small curated set (1 humanist sans, 1 slab, 1 handwriting for the hand-drawn look), a `[theme:]` with palette + typography + default reveal style (HyperFrames' `frame.md`/`design.md` idea, `docs/guides/design-tools.mdx`), and expose `ColorHex` to `[arrow:]`/`[highlight:]` — already implemented in the renderer, only missing from the parser (documented gap in `API_...md` Open Questions).
- **VFX chain for the hand sprite** — a subtle chalk-dust/pencil-texture stroke alpha rather than a hard diagonal mask (`determinism`-safe because it is a pure function of `progress`). Mask styles already live in `GenerateMask`/`MaskConfig`, so this is an added style, not a new subsystem.

---

## 4. Roadmap

| # | Item | Effort | EU | VQ | Depends on |
|---|---|---|---|---|---|
| 1 | `internal/diag` + `zen-board lint` (+ `--json`), render refuses on error | M | ★★★★★ | ★★★ | — |
| 2 | Determinism contract + `TestRenderFrameDeterministic` | S | ★★ | ★★★★ | — |
| 3 | `snapshot` / `frames` / `compare` (PNG, no ffmpeg) | M | ★★★★★ | ★★★★ | — |
| 4 | `info` / `timeline --json` / per-stage `stats` | S | ★★★★ | ★★ | — |
| 5 | `[sfx:]` implemented + sidechain ducking | M | ★★ | ★★★★★ | 1 |
| 6 | Loudness normalize (2-pass `loudnorm`) | S | ★ | ★★★★ | — |
| 7 | `doctor` + structured error messages w/ line:col | M | ★★★ | ★★ | 1 |
| 8 | Word-timing verification pass (`-align auto`) | M | ★★ | ★★★★ | 4 |
| 9 | `project.json` + `init` + variables + `--variants` batch | M | ★★★★ | ★★★ | 1 |
| 10 | `[include:]` scenes + `scenes/registry.json` + starter pack | M | ★★★★ | ★★★★ | 9 |
| 11 | Render profiles, disk asset cache, png-sequence, `benchmark` | M | ★★ | ★★★★ | 3 |
| 12 | SKILL.md / AGENTS.md authoring guide for agents | S | ★★★ | ★★ | 1,3,4 |
| 13 | Easing tokens, camera verbs, multi-font + `[theme:]`, `ColorHex` in DSL | M | ★★ | ★★★★ | 2 |
| 14 | Studio-lite: timeline UI + `.zen` write-back + preview-from-here | L | ★★★★ | ★★★ | 3,4 |
| 15 | Render history + `revert` + artifact attribution | M | ★★ | ★★ | 4,11 |

**Ship 1–4 together.** They are ~2 weeks of work, touch no existing renderer code, and convert zen-board from "guess, render, watch, repeat" into the HyperFrames loop — `lint → snapshot → render` — which is the actual reason HyperFrames is easy to use with an agent.

---

## 5. Explicitly do **not** copy

| HyperFrames feature | Why rejected for zen-board |
|---|---|
| HTML/CSS composition + headless Chrome engine (`packages/engine`, `producer`, `browser`) | zen-board's whole value is a single Go binary, sub-100 MB, deterministic by construction, piped to ffmpeg. Importing Chromium costs determinism risk and a ~300 MB dependency for zero whiteboard-specific gain. |
| Runtime adapters (GSAP/Lottie/Three/WAAPI/Anime, `hyperframes-keyframes`) | Motion in a whiteboard video is *hand-drawn reveals*, not arbitrary tweening. `easing.go` + `mask.go` + `camera.go` is the right size (§3.11 only). |
| `shader-transitions`, HDR, color LUTs, `grade-compare` | Not applicable to a flat whiteboard canvas; only `contrast-*` auditing ports meaningfully — as a lint check on `[style:blackboard]` + `ColorHex`, not as a color pipeline. |
| Docker-pinned rendering | Right idea, wrong tool for a Go binary; §3.10.5 (record ffmpeg/voice versions in the manifest) gets ~80 % of the value. |
| 21 skills + plugin marketplaces (`claude plugin …`, `npx skills add`) | Overkill distribution. Ship **one** `skills/zen-board/SKILL.md` + an `AGENTS.md` scaffold (§3.8.6). |
| Canary rollout infra (`packages/core/src/canary.ts`) | Solves a fleet problem; zen-board is a single-user binary. |
| Slideshow / presenter mode, `webmcp`, publish/cloud/Lambda/CloudRun backends | Different product shape (interactive decks, hosted render). Revisit only if zen-board ever goes multi-tenant. |
| Remotion port workflow | No `.zen` equivalent demand today; if porting is ever needed it is from VideoScribe/Doodly project files, not React. |

---

## 6. Risks

1. **Lint drift.** If `lint` is advisory it will be ignored and the DSL stays unsafe. Make `Run()` gate on lint errors (opt-out `--no-verify`), and keep every rule's `fixHint` actionable — HyperFrames ties `check` to the exit code (`checkExitCode`, `shouldBlockRender.ts`).
2. **Write-back correctness (§3.9).** Editing a hand-tuned `.zen` from a GUI can reformat the whole file. Never regenerate: patch token ranges only — which is why §3.8.2's line/column tracking on `DrawAction` must land first.
3. **Scope creep toward HTML.** Every new capability tempts a "just embed a browser" shortcut. The event-tuple model (`model.FrameEvent`) plus `Engine.RenderFrame` purity is the moat; new features enter as event types + handlers, exactly as `annotate.go` already does.
4. **Timing verification changes existing outputs.** §3.7.4 will move actions relative to syllable estimates. Gate behind `-align` (default `off`) and note the version in the render manifest (§3.10.5) so old scripts stay reproducible.
5. **Cache invalidation.** Disk caches (SVG raster, TTS, aligned timings) need a hash of every input that affects them — including `Width/Height`, `voice`, `speed`, `seed`. §3.2's determinism contract is precisely the precondition that makes caching sound.

---

## 7. Bottom line `[HYPOTHESIS]`

zen-board already has what HyperFrames had to *engineer into* its stack: a deterministic, pure-function, single-binary render core with an audio-first timing model that is better suited to narration-driven video than HTML `data-start` attributes. What it lacks is everything around the core — validation, cheap visual verification, an inspectable timeline, reusable scenes, a real audio mix, and a machine-readable CLI. The first four roadmap items deliver most of the perceived "ease of use" without touching a single renderer file; the audio items (§3.7) deliver most of the perceived "quality".
