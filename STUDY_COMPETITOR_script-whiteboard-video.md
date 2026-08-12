# Competitive Intelligence Study — Script-Driven Whiteboard Video Generation

**Date:** 2026-08-12
**Subject:** zen-board — deterministic `.zen` script → MP4 whiteboard video engine
**Method:** Competitive landscape scan across 5 products, verified against vendor docs, GitHub, and third-party analyses. Confidence labels: `[VERIFIED]` (multi-source confirmed), `[UNCERTAIN]` (one-sided or stale), `[HYPOTHESIS]` (strategic inference — clearly marked).

---

## 1. Executive Summary & Our Specialization

### What zen-board is (`[VERIFIED]` — from PROJECT_OVERVIEW.md)

zen-board is a **local, deterministic, agent-first whiteboard video engine**. A human or an LLM agent writes a declarative `.zen` script; zen-board parses it into a timeline, orchestrates TTS narration with word-level timing, renders every frame on the CPU (hand-drawn strokes, annotations, camera moves, lower-thirds), and pipes frames through FFmpeg to MP4.

Pipeline: `.zen` → Parser → Timeline Compiler → Render Engine → FFmpeg. Key architectural traits:

- **Frame-granular timeline** — compiled to a flat array of `FrameEvent`s; per-frame composition, no scene graph.
- **Goroutine frame pool** — embarrassingly parallel rendering, near-linear speedup.
- **TTS caching layer** — hash-keyed file cache; iteration does not re-pay TTS API costs.
- **Camera as post-process crop** — pan/zoom via crop-and-scale on a hi-res buffer, no re-render.
- **SVG via XML editing** — fill/color swaps and `rgb()` normalization for oksvg rasterization.
- **Background removal strategies** — rembg / chroma-key / brightness, config-selected.
- **ASS subtitle generation** from word timings.
- **Optional** AI asset generation (zeni-lights prompts) and background removal (Python + rembg).

### What makes us unique (Core Differentiators)

1. **A true declarative DSL, not a programming language.** Manim, Remotion, and Motion Canvas all require the author to write *code* (Python scenes / React components / TypeScript generators) and understand a program's execution model + build toolchain. zen-board's `.zen` is a flat, readable markup that a non-programmer — or more importantly an **LLM — can emit without a full language runtime and without dependency risk**. Lowest "agent token cost to first render" in the category. `[VERIFIED + inference from PROJECT_OVERVIEW.md]`
2. **Local-first and essentially free per render.** No cloud render farm, no headless Chromium (Remotion), no LaTeX + Cairo chain (Manim), no subscription (VideoScribe/Pictory). FFmpeg is the only hard external dependency. `[VERIFIED]`
3. **Whiteboard aesthetic as the product, not a plug-in.** VideoScribe is the only direct aesthetic peer; the code-driven tools are generic motion-graphics engines that do whiteboard only by hand-building strokes. `[VERIFIED]`
4. **Built-in narration-first timing.** TTS word timings drive both the timeline and ASS subtitles — narration sync is the backbone, not a bolt-on. `[VERIFIED]`
5. **Deterministic and versionable.** Same `.zen` → same MP4. Scripts are git-diffable, testable, and cache-friendly — the properties that make agent-driven production loops viable. `[VERIFIED]`

**Defensible positioning:** *zen-board is the whiteboard explainer renderer that AI agents can drive reliably — code-free script in, deterministic MP4 out, zero per-render cost, fully local.*

---

## 2. Competitor Landscape

| Product | Category | Core Stack | Script/authoring model | Target audience | License / cost |
|---|---|---|---|---|---|
| **Manim / ManimCE** `[VERIFIED]` | Programmatic math/explainer animation engine | Python; Cairo or OpenGL backends; FFmpeg; LaTeX (external) | Python `Scene` classes + `self.play()`; imperative sequencing | Math/STEM educators, devs, 3Blue1Brown-style channels | MIT, free; steep deps |
| **Remotion** `[VERIFIED]` | Code-driven video framework | React + TypeScript; headless Chromium frame capture; FFmpeg; Node.js | React components "video as code"; `useCurrentFrame`/`interpolate`/`Sequence` | Developers, data-driven & batch marketing video, agent tooling | Source-available; free individuals/labs ≤3 employees, paid company license for 4+ |
| **Motion Canvas** `[VERIFIED]` | Code-driven vector animation library + editor | TypeScript generators; Canvas 2D API; Vite dev server; browser editor | Procedural generator functions (`yield*`, `waitUntil`) | Technical explainer / dev-content creators | MIT, free |
| **VideoScribe (Sparkol)** `[VERIFIED]` | Whiteboard animation SaaS (GUI) | Browser/desktop app; SVG assets; drag-drop canvas + timeline | GUI drag-drop; optional AI script *generator* (narration text only, not a render spec) | Non-designers, educators, marketers, SMEs | Commercial subscription (~$150–280/yr per third-party review data `[UNCERTAIN]` on exact pricing) |
| **Pictory / InVideo AI** `[VERIFIED]` | Cloud AI text/script → stock-video platform | Cloud web app; stock footage DB; AI TTS; captioning | Paste script/URL/PPT/prompt → AI builds stock montage; no deterministic local spec | Content creators, marketers, course builders (SDY repurposing) | Commercial subscription; trial watermarks `[VERIFIED]` |

**Read on the landscape:** two products (Manim, Remotion, plus Motion Canvas) prove the *code-driven deterministic* thesis at scale — but all three require a real language + dependency chain and target *generic* motion graphics. Two products (VideoScribe, Pictory) prove the *whiteboard/narration* market and the *script → video* UI pattern — but are GUI/cloud, non-deterministic, and paywall local rendering. **No current product combines: declarative markup + whiteboard idiom + narration sync + deterministic local render + LLM-first ergonomics. That unclaimed square is zen-board's addressable wedge.** `[HYPOTHESIS]`

---

## 3. Feature Comparison Matrix

Legend — Ours: ✅ native / ▶ partial or via config / ✖ absent. Classification column uses the 5 mandated buckets.

| Feature | zen-board | Manim | Remotion | Motion Canvas | VideoScribe | Pictory/InVideo | Classification |
|---|---|---|---|---|---|---|---|
| Declarative text authoring (no full language) | ✅ `.zen` DSL | ✖ needs Python | ✖ needs React/TS | ✖ needs TS | ✖ GUI | ▶ "script" = narration text only | **Our Core Advantage** |
| Agent-writable by LLM (low token + dep risk) | ✅ design goal | ▶ agents exist but emit Python `[VERIFIED via emergentmind]` | ✅ ships agent skills (Claude Code) `[VERIFIED]` | ▶ possible | ✖ GUI | ✖ webapp API | **Our Core Advantage** (Remotion = closest peer) |
| Deterministic, versionable output | ✅ | ✅ | ✅ | ✅ | ✖ manual/external deps drift | ✖ AI montage non-deterministic | **Our Core Advantage** (shared with code tools) |
| Local rendering, zero per-use cost | ✅ | ✅ | ✅ (but Node+Chromium heavy) | ✅ | ✖ subscription | ✖ cloud credits | **Our Core Advantage** |
| FFmpeg pipeline (H.264/AAC) | ✅ | ✅ | ✅ | ◀ via exporter | ✅ export | ✅ cloud | We Have It (parity) |
| Parallel/CPU frame rendering | ✅ goroutine pool | via cairo parallel `[UNCERTAIN]` | via headless browser (slower per frame) | via canvas parallel ⚠ unverified | ✖ | ✖ | **We Have It — Competitor Stronger (Manim/MC on pure GPU/GL speed)** |
| TTS narration with word-timed sync | ✅ orchestrator + syllable estimation | ✖ (no native TTS) | ▶ via external audio tools | ▶ audio sync in editor (no TTS) | ✅ AI voiceover (45+ languages) `[VERIFIED]` | ✅ AI voiceover 20+ languages `[VERIFIED]` | We Have It (parity on core; VideoScribe/Pictory stronger on languages) |
| TTS response caching for iteration | ✅ hash-keyed cache | ✖ | ✖ | ✖ | ✖ | ✖ | **Our Core Advantage** |
| Auto subtitles (dialog) | ✅ ASS from word timings | ✖ (static Text only) | ▶ via captions pkg | ✖ | ▶ manual/auto captions `[UNCERTAIN]` | ✅ auto captions `[VERIFIED]` | We Have It (Comparable; Pictory stronger for social styling) |
| Hand-drawn whiteboard animation | ✅ hand sprite + stroke progress | ✖ | ✖ | ✖ | ✅ best-in-class `[VERIFIED]` | ✖ | **Our Core Advantage vs code tools; VideoScribe (the GUI peer) is stronger on asset polish** |
| Annotations — arrows/highlights/compare/overlay/counters | ✅ dedicated handlers | ▶ via shapes | ▶ via SVG/absolute | ▶ via shapes | ✅ via drag-drop objects | ✖ | **Our Core Advantage (unique integrated set)** |
| Camera pan/zoom post-process | ✅ CropAndScale | ✅ `self.play_camera_frame` | ▶ via transforms/zoom | ▶ via view transforms | ✅ camera positions per scene `[VERIFIED]` | ▶ scene-level pan/crop | We Have It (Comparable) |
| Lower-thirds / banner overlays | ✅ banner panel | ✖ | ✅ via React layout | ▶ via scene compositing | ▶ text/logo overlays | ▶ text overlays | **Our Core Advantage for broadcasting UX** |
| SVG ingestion + recolor via XML | ✅ oksvg + etree | ✖ | ✅ full DOM (stronger) | → canvas raster only | ✅ SVG preferred import `[VERIFIED]` (stronger: dev-made SVG packs) | ✖ (stock VFX) | We Have It (Comparable; Remotion stronger via DOM, VideoScribe stronger via curated lib) |
| AI image generation for assets | ▶ zeni-lights (optional) | ✖ | ▶ via APIs/React | ✖ | ✅ built-in prompt → image `[VERIFIED]` | ✅ AI Studio text/image `[VERIFIED]` | **Competitor Stronger (bigger baked-in gen UX)** |
| Background removal | ✅ rembg/chroma/brightness | ✖ | ▶ via canvas libs | ✖ | ✖ `[UNCERTAIN]` — may auto-clean | ✖ (stock only) | We Have It (Unique) |
| Real-time preview / scrubbing | ▶ preview pipe exists (`NewPreviewPipe`) but no frame scrub UI `[INFERRED]` | ✅ `-pql` + preview window `[VERIFIED]` (framework-level) | ✅ Studio hot-reload scrub `[VERIFIED]` (best) | ✅ Vite live editor sync audio `[VERIFIED]` | ✅ timeline playback `[VERIFIED]` | ✅ browser player `[VERIFIED]` | **We Lack It → Adaptation Candidate** |
| Batch / data-driven templating (one spec, many videos) | ▶ config-driven, no variable-injection layer `[INFERRED]` | ✖ (scripts are custom code) | ✅ data-inject props + Lambda batch `[VERIFIED]` | ▶ generator pipelines possible | ✖ | ✖ | **We Lack It → Adaptation Candidate** (Remotion strongest) |
| Aspect-ratio presets (16:9 / 1:1 / 9:16) | ▶ via config resolution only | ✖ (fixed canvas classes) | ✅ preset compositions `[VERIFIED]` | ▶ via config | ✅ choose 16:9, 1:1, 9:16 at start `[VERIFIED]` | ✅ social exports `[VERIFIED]` | **We Lack It → Adaptation Candidate** |
| Math/LaTeX typesetting | ✖ (embedded fonts, no math) | ✅ MathTex via LaTeX `[VERIFIED]` (best) | ▶ via KaTeX libs | ✅ built-in LaTeX components `[VERIFIED]` | ✖ | ✖ | **We Lack It → Adaptation Candidate** (strong for STEM niche) |
| Code-block / syntax-highlight animation | ✖ | ▶ Text only | ✅ via components `[VERIFIED]` | ✅ built-in code components `[VERIFIED]` | ✖ | ✖ | We Lack It → Adaptation Candidate |
| Music / SFX library | ✖ (no audio assets module) | ✖ | ▶ via audio pkg | ▶ audio in editor `[UNCERTAIN]` | ✅ library + record VO `[VERIFIED]` | ✅ background music `[VERIFIED]` | **They Have It → Rejected** (licensing burden; out of agent scope) |
| AI avatars / voice cloning | ✖ | ✖ | ✖ | ✖ | ▶ some AI VO | ✅ avatars + voice clone `[VERIFIED]` | **They Have It → Rejected** (heavy cloud ML; not whiteboard) |
| Headless-browser DOM rendering | ✖ | ✖ | ✅ core mechanism | ✖ | ✖ | ✖ | **They Have It → Rejected** (heavy dep) |
| Cloud / serverless render farm | ✖ | ✖ | ✅ Lambda `[VERIFIED]` | ✖ | ✖ | ✅ cloud platform | **They Have It → Rejected** (violates local/privacy position) |
| Community template/asset ecosystem | ▶ local assets dir | ✅ big community | ✅ huge (56k stars) | ✅ growing | ✅ 8 000+ image lib `[VERIFIED]` | ✅ 40+ templates `[VERIFIED]` | **We Lack It → Adaptation Candidate** (ship curated starter library) |

Notes on unverified cells: Motion Canvas audio-in-editor details and parallel rendering claims could not be fully confirmed; VideoScribe caption behavior `[UNCERTAIN]`.

---

## 4. Prioritized Adaptation Roadmap

Ranking by (Impact × Effort), weighted for the agent-first strategy.

| # | Adaptation | Impact | Effort | Where it fits architecturally |
|---|---|---|---|---|
| 1 | **Agent skill / authoring kit for `.zen`** — ship `AGENTS.md`-style spec, a JSON Schema for `.zen`, curated examples, and a "validate+build" fast command. Remotion proves the pattern (shipped agent skills for Claude Code `[VERIFIED]`). | ⭐⭐⭐ High — directly monetizes the "agents love text" wedge; lower LLM iteration cost | Low — docs + schema + small CLI flag; no engine change | Spec lives beside parser `internal/script`; schema generated from `model/types.go` types |
| 2 | **Fast feedback loop** — `--watch`/`--frame 1234` single-frame render + PNG sequence export for sub-second iteration, plus a strict `.zen` linter emitting per-line actionable errors. Remotion Studio scrub / Manim `-n <n>` / `-s` prove the value `[VERIFIED]`. | ⭐⭐⭐ High — agent retry loops (generate → render one frame → fix) collapse in latency | Medium — renderer already frame-indexed; add frame-select target + lint pass; watch mode reuses existing pipeline | `builder/renderer.go` RenderTimeline with target-frame short-circuit; parser errors enriched with line/col |
| 3 | **Data-driven templating** — `.zen` variable substitution (`{{var}}`) + multi-video batch mode. Remotion data-inject is the strongest precedent `[VERIFIED]`. | ⭐⭐ High for scale use-cases (personalized explainers) | Low-Medium — preprocessor pass alongside `SplitInlineWaits` | `internal/script/preprocessor.go` — add token-subs + batch loop in `main.go` dispatch |
| 4 | **Aspect-ratio + size presets** — first-class 16:9 / 9:16 / 1:1 and 720p/1080p selection rather than raw config resolution. | ⭐⭐ Medium — unlocks social platform publishing where whiteboard content is growing | Medium — layout presets (`layout.go`) must reflow for vertical; render res scaling | `internal/model/layout.go` `GetPresetLayout` gains per-ratio regions; `config.json` gains preset enum |
| 5 | **Curated starter asset + template pack** — 50–100 whiteboard-stroke SVGs + 5 `.zen` scene templates shipped in repo (VideoScribe's 8 000+ library and Pictory's 40+ templates are the proof `[VERIFIED]`). | ⭐⭐ Medium — removes the hardest blank-canvas problem for LLM generation quality | Medium — asset curation + generator integration with zen-lights | `assets/` dir + `examples/` templates; indexer already auto-indexes |
| 6 | **Math typesetting for STEM explainers** — render LaTeX/TeXMath to SVG (temp-file → oksvg) or embedded MathMarkdown subset. Manim MathTex / Motion Canvas LaTeX components are the reference `[VERIFIED]`. | ⭐⭐ Medium — opens the STEM niche where deterministic notation matters | High — external LaTeX dep conflicts with lightweight philosophy, or build a subset renderer | New `internal/text/` module; SVG path from `internal/svg/render.go`; requires dep decision (tradeoff below) |
| 7 | **Code-block animation** — monospace panes with stepwise reveal + syntax-ish styling for dev content. Low effort if text renderer gains monospace + masked reveal (mask.go already exists). | ⭐ Medium — complements #6 for developer explainers | Medium | Reuse `internal/render/mask.go` reveal/wipe on text panes; new `internal/render/code.go` |

---

## 5. Explicit Rejections

| Competitor feature | Rationale (one line) |
|---|---|
| Cloud render farm / serverless batch (Remotion Lambda, Pictory cloud) | Violates core local-first, privacy, zero-cost position; adds ops surface. |
| Headless-browser DOM rendering (Remotion) | Heavy Node+Chromium dependency chain undermines the lightweight single-binary philosophy. |
| GUI drag-drop editor with timeline scrubbing (VideoScribe / Studio) | "Code & script is the source of truth" — a full GUI editor is scope creep against the agent-first wedge. |
| Stock footage / music library licensing (Pictory, VideoScribe) | License clearing + asset hosting overhead; whiteboard idiom should stay self-generated. |
| Realistic AI avatars / voice cloning (Pictory/InVideo) | High-opacity cloud ML, wrong persona (not whiteboard), ongoing model cost. |
| GPU/OpenGL real-time interactive canvas (ManimGL)  | Hardware-dependent rendering path splits the codebase; CPU determinism is a feature. |
| LaTeX full-toolchain dependency (Manim) | Heavy external dep (MiKTeX/MacTeX) precisely counter to "render anywhere". (A *subset* renderer stays on roadmap #6.) |

---

## 6. Tradeoffs & Risks

1. **#1 Agent skill pack** — Tradeoff: DSL **stability promise + doc maintenance** become a contract, not prose. Risk: LLMs will exploit unknown `.zen` corners; mitigates with strict linter (feeds every future self-doc'd release). Burdens: schema must track every new action keyword.
2. **#2 Fast feedback / watch mode** — Tradeoff: single-frame target + linter is a **second, parallel compile path** to keep in sync (timeline dependency on TTS + assets). Risk: "frame 1234" may not exist until TTS/timeline compile; mitigate by always compiling full timeline, rendering 1 frame.
3. **#3 Data-driven templating** — Tradeoff: variable substitution **grows the DSL surface** past "simple script", and untrusted batch input raises **injection/escaping concerns** in render text. Mitigate: escape by default, explicit `unsafe` flag.
4. **#4 Aspect-ratio presets** — Tradeoff: each new ratio = **layout reflow + player QA matrix**; vertical 9:16 changes camera/lower-thirds defaults. Risk of subtle placement bugs absorbed by layout presets regression tests.
5. **#6 Math subset renderer** — Tradeoff: either an **external LaTeX dependency** (breaks the "no heavy deps" claim) or building a math-SVG subset engine (high effort, narrow coverage). This is the single most scope-risky candidate; defer until STEM positioning is confirmed by real user pull.
6. **Cross-cutting** — every adaptation shares the true tradeoff of the stance: **the more features, the heavier `.zen`, the fatter agent prompts, the more tokens per render attempt.** The whole portfolio must be gated by the rule *a feature must pay for itself in fewer agent tokens or better first-attempt success*, not by feature parity against GUI tools. `[HYPOTHESIS]`

---

## 7. Strategic Bottom Line `[HYPOTHESIS]`

zen-board competes in an empty quadrant: **deterministic declarative markup + whiteboard idiom + narration sync + local zero-cost rendering + LLM-first ergonomics**. Its nearest intellectual siblings (Manim, Remotion, Motion Canvas) prove demand but require real programming; its aesthetic peers (VideoScribe, Pictory) prove the market but run on GUI/cloud with non-deterministic output. The winning move is *not* to chase their feature depth — it is to **make the `.zen` → MP4 agent loop unbeatable**: schema + linter + watch/frame-preview + data templates + starter assets, i.e. roadmap #1–#5, and only then evaluate STEM (#6–#7) against actual usage pull.

---

**Sources (primary):** `PROJECT_OVERVIEW.md` (zen-board) · Manim: pypi.org/project/manim, GitHub `3b1b/manim` & `ManimCommunity/manim`, manim-ce docs roundups · Remotion: remotion.dev, GitHub `remotion-dev/remotion`, remotion.dev/docs (compare/motion-canvas) · Motion Canvas: motioncanvas.io, GitHub `motion-canvas/motion-canvas`, motioncanvas.io/docs · VideoScribe: videoscribe.co (features/how-to), checkthat.ai & appmus.com analyses · Pictory: pictory.ai (text-to-video, features), unite.ai review, pexo.ai & nation.ai summaries.