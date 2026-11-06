# REVIEW — `ARCHITECTURE_PLAN_agent-verify-loop.md`

**Reviewer input:** 3 investigation passes against this checkout (two read+execute surveys, one adversarial review). Baseline: `go build ./...` + `go vet ./...` clean, `go test ./...` **all green** — so every defect below is untested-by-omission, not contradicted by the suite.
**Benchmark:** hands-on HyperFrames evaluation at `/media/jang/home/Deve/zen-hyperframes/REPORT.md` (verdict: the `check --json` gate + PNG `snapshot` loop *is* the product; `init`/`catalog` + 2 × `SKILL.md` ≈ 5 k-token minimum path).
**Goal under test:** "zen-board very easy for an AI agent to create structured videos — whiteboard, mascot talking with meme."

---

## 1. Verdict

**Direction: correct. Sequence: wrong. Premise: overstated in one place, understated in four.**

* Keep: `FrameSink` + whitelist instead of `BuildFrameStates` extraction (Q10 / R3 — parity by construction is the right call), four verbs, `Envelope` + exit codes, `session` as the single sequencer, `AGENTS.md`/skill as a deliverable, no `Project.Seed`.
* Stop: **8 concrete items fail or mislead if implemented exactly as written** (§2). Two of them hang or break the test suite. The plan cannot be executed row-by-row as it stands.
* Missing: the plan buys the **loop** and delivers **nothing for the stated genres**. Verified facts §6: a mascot-talking video is not expressible today at any script length — expression swaps ghost, nothing can be removed except `[clear]` (which wipes the board), arrows/highlights cannot point at an element, and two of the six shipped examples render **no drawing hand at all**. `check`+`snapshot` will let an agent *see* a broken mascot video faster. That is the correct fix order: §2 blockers → lint → snapshot → expressiveness, not 11 steps of tooling then hope.
* Cost realism: the plan is ~11 steps / roughly 8–10 working days as scoped, of which **~3 days is plumbing with no product pull** (raster disk cache, `-diff` before identity, `-from mp4`, F13). Cut §5 items and the plan gets *more* valuable in less time.

---

## 2. Blockers — fails or hangs as written `[VERIFIED]`

| # | Defect | Evidence | Fix |
|---|---|---|---|
| **B1** | **Step 4 (`FrameSink` + whitelist) deadlocks.** `RenderTimeline`'s collector walks frame indices sequentially and blocks on the channel: `for nextFrame < totalFrames { res := <-engine.Pool.Results … nextFrame++ }` (`renderer.go:150-176`). Plan change (2) skips non-whitelisted frames **in the feed loop** (`:117-139`) → no result is ever produced for them → the collector blocks forever on frame 1 for a whitelist starting at 0. `<-sem` in the result loop is balanced against feed submits, so a "skip feed, still expect result" variant also leaks semaphore tokens. | read `renderer.go:117-183` | Collector must be driven by the **ordered whitelist slice**, not `nextFrame++`. Also move the `-eventlog` `prevActive` scan (`:120-131`) into a separate full-range pass, or `-eventlog` silently changes meaning. |
| **B2** | **B7 and B6 are mutually unsatisfiable.** B7: `Run() = os.Exit(cli.New().Run(os.Args[1:]))`. B6 acceptance: "`integration_test.go` passes unmodified" — but that test calls `Run()` **in-process** at `integration_test.go:64,133,191,272,348,407` and `t.Fatalf`s on the error. An `os.Exit` inside turns the test binary's exit code into 10 mid-suite. | `integration_test.go` sites listed | Keep `func Run() error`; `os.Exit` stays only in `main()`. `RedirectForJSON()` swaps process-global `os.Stdout` and those six `Run()` calls share one process → add an "invoke twice in-process" smoke test for the restore path. |
| **B3** | **`PNGSink` double-`Put`s a pooled buffer** (the plan documents the bug as the rule, §4.2 "encodes then `Put`s"). `renderer.go:173` already does `engine.Pool.BufferPool.Put(frame)`; for **unzoomed** frames `CropAndScale` returns the *same pointer* (`camera.go:51-55`) so the sink frame **is** the pooled buffer. For zoomed frames `camera.go:57` allocates privately. No clear-on-Get (`pool.go:34-38`); the only sanitizer is the full-canvas bg blit `engine.go:134`. | executed: same-ptr identity confirmed both ways | Rule: **the sink never `Put`s**; the collector owns the buffer. Sink copies if it retains. |
| **B4** | **`svg.CacheKey` is non-deterministic → Group D2 / step 8 is unbuildable.** `render.go:38-46` hashes `variants` by iterating a Go map. Measured **4 distinct keys over 200 calls** with one 4-entry map ⇒ today's in-process `svgCache` (`timeline.go:466`) misses at random; a *disk* cache would never hit and would write unbounded garbage. Neither `TestRasterCacheRoundTrip` nor `TestRenderFrameDeterministic` catches it (identical pixels, different key). | `svg/render.go:38-46`, measured | Sort `k=v` pairs before hashing. Land in step 1, before any cache work. |
| **B5** | **`TestDSLCoverage` cannot be written against the real dispatch.** `dispatch` (`compile_actions.go:44-93`) is `switch { case strings.HasPrefix… default: handleDraw }` — "every case reachable from a spec" therefore **excludes bare `[draw:]`**, the most-used tag in the language. And `TAG_ERASE_ALL` (`erase:*`) shares `eraseRegex` with `TAG_ERASE`, so "every `TAG_*` has a spec and a pattern" is unsatisfiable as stated (21 constants vs 21 patterns is coincidence). | read `compile_actions.go:44-93`, `constant.go:9-33` | Rewrite as: (i) every `ActionSpec` has ≥1 pattern matching a canonical fixture string; (ii) the set of tags `dispatch` routes to a **non-`default`** handler equals the spec set (derive by routing, not AST grep). Add `MinArgs/MaxArgs` + `AssetArgs` to the spec and make the handlers use the same table (kills B6/D1 permanently). |
| **B6** | **A second and third live panic sit in `builder`, not `script`** — neither step 1 nor the lint tier covers them: `[arrow:TL]` → `to := parts[1]` unguarded (`compile_actions.go:568`); `[compare:onlyone]` → `index out of range [1] with length 1` (`:631`). Both confirmed by running `CompileTimeline` with synthetic word timings. | executed, recovered panics | Arity guards driven by the DSL table, same step as the parser fix. |
| **B7** | **`assetKey()` has 17 sites, not 6.** Plan C5 cites `engine.go:253,259-260,303,471,518` (two already off by two). Real: `engine.go:253,262,271,305,315,471,520,527,648,653` + `annotate.go:419,420,496` + `watermark.go:46,53,78,83`. `PrepareAssets`' SVG loop (`timeline.go:441-508`) rewrites `TargetImage` for **overlay/slide/static** too, and `handleOverlayEvent` reads `e.Assets[ev.TargetImage]` (`annotate.go:496`) — implement "the six sites" verbatim and `[overlay:<svg>]` breaks. No example uses an SVG overlay, so nothing catches it. | grep + read | Split `RasterKey` into its own step; migrate all 17 sites + the prefix checks (`engine.go:259`, `timeline.go:243,441,460`) and add an SVG overlay/slide fixture. |
| **B8** | **`validateAssets` is a method** (`(c *timelineCompiler)`, `timeline.go:230`) whose only caller is `CompileTimeline:90`. Plan F3 "demote it to a backstop that emits findings" never says how the report gets out while `CompileTimeline` still returns a joined `error`. Also `diag.FromCompileWarnings` is needed in step 4 but `diag` ships in step 7. | read | Put `*diag.Report` on `timelineCompiler` in step 2 (before the 9 `log.Printf("Warning:` sites in `internal/builder` become findings). |

---

## 3. Premise correction (measured, this machine, `examples/demo.zen` 1080×720@30)

| Run | Wall | TTS stage | Render+encode |
|---|---|---|---|
| cold cache | **19.5 s** | **16.3 s (83 %)** — 8 HTTP misses ≈ 2.04 s/line | 3.0 s |
| warm cache | **3.2 s** | 5.5 ms (8 hits) | 3.0 s (`Total time writing to FFmpeg pipe: 2.98 s`) |

Peak RSS 1.50 GB. Frame CPU: `Crop & Scale 79.2 %` (camera on), `Scale Asset 11.9 %`.

* ⇒ "an agent must pay for a full encode to discover a typo" is **overstated**: a warm encode is 3.2 s. The real blockers are that the agent **cannot see anything** (MP4 is opaque; PNGs are the only eyes), that the binary **panics** on some typos, that most typos produce **zero events with exit 0 and clean TTS**, and that there is **no `AGENTS.md`** so every session starts by reading a 290-line doc.
* ⇒ Cold iteration cost is TTS, and `OrchestrateTTS`'s "parallel" is serialised: `semTTS := make(chan struct{}, 1)` (`orchestrator.go:35`). Bounded concurrency is a ~1-hour change worth more to iteration latency than the whole of plan Group D.
* ⇒ `-timing estimate` is **timeline/lint infrastructure, not visual infrastructure.** Measured spread 3.5–7.9 vowel-groups/s around 5.3 (±40 %), and reveal windows + camera transitions are anchored to *word starts*, so estimate error **accumulates with line count**. `check`/`timeline` can run estimate; `snapshot` may not present estimate frames as reviewable. Restate the milestone as: *"`check` spends 0 TTS; `snapshot` requires a warm TTS cache and says so."*

---

## 4. Additional live defects the plan missed `[VERIFIED by execution]`

| # | Defect | Where | Why it matters for the goal |
|---|---|---|---|
| D1 | **Panic scope is 9 of 10 nesting shapes, not one pair.** Any pattern whose content class is `[^\]@]+` or `[^\]]+` can swallow a `[`. Panics inside `text/erase/slide/banner/chapter`, quoted or not, incl. `[text:"a [wait:1] b"]`, `[text:"a [clear] b"]`. Control (two sibling tags on one line) is fine. Corollary: the overlap only occurs across **two different** regexes — same-regex nesting never panics, it **silently drops the inner tag** (`[text:"q [text:inner]"]` → inner lost, `"]` survives as narration). | `parser.go:229` (plan cites `:207-209`) | `E0003` must cover all 21 patterns × 20 others, plus an "absorbed/unterminated quote" code. Highest-value single rule in the plan. |
| D2 | **`examples/*.zen` would not have caught either flagship defect** — no nested brackets, no `:num.num` outside `[wait:1.0]`. | `examples/` (6 files, 69 lines) | `TestPatternTableMatchesLegacy "on all examples"` needs a **synthetic fixture corpus** alongside, or step 2's gate is decorative. |
| D3 | **Reveal duration is dead end-to-end.** `model.DrawAction.RevealDuration` (`types.go:56`) is never assigned and never read; the live path re-splits the tag string at `timeline.go:188-197`. Hence `[draw:robot:TR:3]` works and `[draw:robot:2.5]` yields 0 actions read aloud. `FrameEvent` has **no** reveal-duration field at all — `dsl.go` would describe a parameter with no home. | `timeline.go:188-197`, `types.go:56` | `E0007` must be paired with putting the value in the event struct, not just widening the regex (which changes `preset`/`variants` partitioning). |
| D4 | **Silent zero-event / silently-degraded classes** (each verified, none in the plan's 6-code tier): `[text:…]` without two quotes → **0 events**, no warning; `[overlay:x:1.0:BR:3.0]` → 4th part (duration) dropped, stays `FRAME_FOREVER`; `@coords` on `slide`/`overlay`/`watermark` → coords absorbed into the asset id (slide → hard error); `[draw:robot:happy]` → unknown bare token silently becomes a **preset** (full-canvas box + bogus camera name); `[zoom:<unknown>]`/`[style:<unknown>]` → silent default; **SVG + `@x,y` without w,h → invisible, exit 0, TTS clean** (`timeline.go:480-482`); unknown `cursor=` → no hand drawn at all. | `compile_actions.go:141-144`, `timeline.go:480-482`, `annotate.go`/`engine.go` | These are the failure classes an agent **cannot self-diagnose**. This is what "AI-agent friendly" actually means here. |
| D5 | **No drawing hand in two of six examples.** `assets/cursors/` contains only `hand`, so `cursor=magic_wand` (`demo.zen:9`, `advanced.zen:6`) resolves to nothing; and `main.go:226` uses the sprite only `if *handPath != "./assets/hand.png"`, i.e. the shipped default path is ignored, and `engine.Hand == nil` ⇒ **no hand at all** (`engine.go:433-442`). | verified by `ls` + code | The hand *is* the whiteboard genre's look. One `else if`. |
| D6 | **`examples/advanced.zen` cannot render** — lines 2/8/10 use asset `test`, which does not exist; `validateAssets` hard-errors (`timeline.go:286-288`). | `assets/` contains city/pyramids/robot/world.png only | 1 of 6 authoring examples is broken. 15-minute fix, high trust signal. |
| D7 | **Per-frame waste is broader than the overlay.** `handleOverlayEvent` re-`scaleImage`s every frame (`annotate.go:513`); `handleCompareEvent` runs **two full-canvas `scaleImageFill`** per frame (`:436-437`); `drawCompareLabel` and `handleCounterEvent` call `RenderText` (glyph shaping) **every frame** (`:475,550`). Measured: **19.9 ms/f per 1024² layer, linear** (8 layers ≈ 151 ms/f, 16 ≈ 302 ms/f). `watermark.go:44-56,76-86` already contains the exact key-and-cache idiom. | read + measured | 4 copies of a shipped pattern ≈ 3 h; bigger throughput win than any disk cache, zero invalidation surface. |
| D8 | **`ZoomFocus` is overloaded as a *layout* key** by overlay (`compile_actions.go:683`) and counter (`:741-769`), then fed to `GetPresetLayout` (`annotate.go:505,546`) — while also being the camera-visibility key (`engine.go:160-192`). Net: `[overlay:world:0.3:BR]` draws at BR but is only visible while the camera is at BR or reset. | read | Plan's `inspect.EventDoc.ZoomFocus` would report a layout box as a camera move — **the machine-readable timeline would actively teach the agent wrong**. Split `CameraFocus` / `LayoutPreset`. |
| D9 | No `context.Context` and no signal handling anywhere in the repo; `ffmpeg.Close()` only `Wait()`s (`pipe.go:142-144`) and is called only on the success path (`renderer.go:183`) → child processes leak on any error. | grep confirmed empty | Validates plan F16; also note `Session.Close()` must be sequenced **after** the sink is closed (the plan's §4.1 diagram puts it after `Envelope.Print`). |
| D10 | `AssetVariant` maps are **aliased and mutated**: `SplitInlineWaits` copies `DrawAction` by value (`preprocessor.go:66-76`) so the map is shared, and `extractCursor` **deletes** `"cursor"` from it. Benign today, fatal with a disk cache keyed on variants or with `id=` inside the map. | read | Keep identity in a **string field**, never in the variant map. |

---

## 5. Scope cuts (resource efficiency ↑, no product pull removed)

| Item | Decision | Reason |
|---|---|---|
| Group D2 `svg/diskcache.go` (raster disk cache) | **DROP** | B4 (key is non-deterministic) + warm iteration is already encode-limited at 3.2 s. Cache the *per-frame* scales instead (D7). |
| Step 10 `-diff` | **DEFER past identity** | Diffing by event requires stable event ids (G1). Diffing now = diffing things you are about to redesign. |
| Step 9 `-from mp4` | **DEFER** | Measured engine-vs-decode **PSNR 40.5 dB** at crf18 with no colour flags; the guard is nice, the agent need is near zero. Keep the frame-count probe. |
| F13 `wordcount_mismatch` as specified | **REWRITE** | `len(Words)` vs script word count proves nothing about alignment; and `model.WordTiming` has **no json tags** (`types.go:60-64`) while the server's tokens are what matter. Assert `Words[i].Word == strings.Fields(cleanText)[i]` positionally, at `StageTTS`. |
| `TestPooledBufferIsCleared` (Group G) | **RE-SCOPE** | There is nothing to clear (`pool.go:34-38`); a dirty buffer yields a correct frame *because of* the bg blit (`engine.go:134`). Assert that instead, or lose a day on a non-bug. |
| `TestNoNondeterministicImports` | **WEAKEN** | The grep proves nothing that `TestRenderFrameOrderIndependent` doesn't — and lazy asset population in `watermark.go:46-53` / `engine.go:648-653` is genuinely order-dependent state. Keep the order test, drop the grep's claim. |
| Step 4 gate "PSNR ≥ 45 dB" | **→ BYTE-EXACT** | Step 4 changes only what happens *after* `RenderFrame`; `image/png` is byte-deterministic across processes (verified). Byte-exact PNG of frame 0 / mid / last for every example is **stronger and easier to read** — and a 45 dB tolerance would have hidden exactly the double-`Put` bug in B3. A hang is not a pixel difference: the PSNR gate also cannot see B1. |
| `-timing cache-only` third mode, `doctor` verb, `Project.Seed` | keep plan's rejections | Agree with §10 and R2; `ErrorInfo.Hint` is the right vehicle. |
| Dead config: `Timeline.AudioPath` (written `timeline.go:103`, never read), `DrawAction.RevealDuration` | remove or wire | Don't let the new DSL table institutionalise dead fields. |

Also: **`gofmt -l .` flags `internal/render/watermark.go`** today — free hygiene for step 1.

---

## 6. The gap the plan and the research doc both leave unfilled `[VERIFIED]`

For the stated goal, expressiveness is the binding constraint, not tooling. What an agent must write today for one mascot across a 30 s script:

```zen
[overlay:mascot_neutral:1.0:RH]     # FOREVER; no @coords allowed; stretched (aspect lost); no hand
[overlay:mascot_shocked:1.0:RH]     # "swap" = another FOREVER layer → old state ghosts underneath
[clear]                             # the only removal → also destroys the board
[overlay:mascot_neutral:1.0:RH]     # …so re-issue by hand; invisible whenever [zoom:] points elsewhere
```

* **Lifetime:** `FRAME_FOREVER = 999999` (`constant.go:80`) is emitted after every draw/text/gen/slide/overlay/watermark. Only banner/highlight/transition/counter have real windows. Only entry animations exist; **no exit animation for anything** except `[erase:]` and the banner.
* **Identity:** elements are addressed by **asset-id string reverse-scan** (`compile_actions.go:252-266`). Two copies of one asset are ambiguous (erase hits the LAST); `slide`/`overlay`/`banner`/`watermark`/`compare` are **permanently unaddressable**; `[erase:city]` on a `[slide:city]` fails outright.
* **Anchoring:** 8 region presets (`model/layout.go`), absolute `@x,y,w,h` on **only** draw/text/erase/move/gen, a 3×2 auto-grid, and 4 hardcoded watermark corner fractions clamped to 8 % canvas height. No thirds, no margins, no size-only spec, no named anchor. Unknown preset ⇒ full canvas, silently.
* **Meme:** impact-style caption text is impossible — `[text:]` has no colour/outline/box/align/wrap, and `fontSize` is cosmetic because the raster is **contain-fit** into the region (`engine.go:281-322`). `ColorHex` exists on `FrameEvent` and is **unreachable from the DSL**. A zoom-punch is impossible: `[zoom:]` takes only the 8 preset names with a hard 1 s transition (`renderer.go:67`). A sticker that leaves needs `[clear]`.
* **N12 — the actual blocker:** `parsePoint`/`parseRegion` (`annotate.go:17-43`) accept `x,y` / `x,y,w,h` / a **preset name**; an **asset id silently falls through `GetPresetLayout`'s default → whole canvas**. So `[arrow:robot:world]` is a zero-length invisible arrow and `[highlight:robot]` frames the entire screen, no warning. This single gap blocks arrows, highlights, zoom-to-element and caption anchoring at once, and it appears nowhere in the plan.
* **Structure:** none. `script.Parse` is line-scoped (`parser.go:35-53`); no block, include, scene, variable or loop. `[chapter:]` produces **zero visual output** (metadata only, `main.go:191-213`). This is the axis where HyperFrames' catalog/`init` wins; zen-board's equivalent is identity + anchors + a canonical genre example.
* Cost of one asset onboarding: `cp` → `assets index -dir ./assets` → `assets process-bg`; there is **no import/upload command** (`cli.go:22-113`, no multipart handler in `server.go`), `AutoIndex` **overwrites** the whole `assets` array (`indexer.go:285`), and ids must be extension-less or resolution looks for `robot.png.png`.

---

## 7. Corrected build sequence

Hours are wall-clock with the existing test harness (`go test .` = 3.4 s, so cheap enough to gate every step).

| # | Step | Files | Hrs | Done when | Risk |
|---|---|---|---|---|---|
| **0** | `AGENTS.md` + `skills/zen-board/SKILL.md` describing **today's** CLI + the verified trap list (D1–D6); fix `examples/advanced.zen` (`test`→`robot`); gofmt | new docs, `examples/` | 3 | every documented command produces the documented output; `advanced.zen` renders | none |
| **1** | Kill **all** panics + cache key: `extractActions` span-conflict (all 21 patterns) at `parser.go:229`; arity guards `:568`/`:631`; sort variant keys in `svg.CacheKey` | `script/parser.go`, `compile_actions.go:564,627`, `svg/render.go:38-46` | 5 | 21-pattern panic matrix green; no malformed tag can crash the binary | low |
| **2** | Offsets + DSL table (`MinArgs/MaxArgs`, `AssetArgs`) + `SrcLine/SrcCol`; `*diag.Report` onto `timelineCompiler`; 9 `log.Printf("Warning:` → findings | `script/{tag,dsl}.go`, `model/types.go`, `compile_actions.go` | 6 | B5-correct `TestDSLCoverage`; `TestPatternTableMatchesLegacy` on a **synthetic** corpus + examples; `SrcRaw == src[off:off+len]` | low |
| **3** | Envelope + 4 verbs + exit codes; **`Run() error` kept**; `-json` with stdout swap | `envelope/*`, `cli/*`, `main.go:64-66` | 8 | `integration_test.go` unmodified + green; one JSON doc on real stdout, twice in-process | medium |
| **4** | `FrameSink` — **B1-correct** (collector driven by whitelist slice) — **sink never `Put`s**; **byte-exact** PNG gate | `builder/{sink,renderer}.go` | 6 | frame 0/mid/last PNG byte-identical pre/post for every example; skipped frames cost no pixel work | **high** |
| **5** | `RasterKey` + `assetKey()` at **all 17** sites + prefix-check migration | `timeline.go:441-508`, `render/{engine,annotate,watermark}.go` | 4 | SVG `overlay`/`slide`/`watermark` fixtures render; a second `PrepareAssets` registers | **high** |
| **6** | `session.Open` (+`ctx`, SIGINT, ffmpeg process group, temp ownership sequenced **after** sink close) — `check` first, `render` last | `session/`, `ffmpeg/pipe.go:139-149`, `tts/*` | 6 | no orphan `ffmpeg` after any error path; render green through `Open` | medium |
| **7** | `diag` lint with the **expanded** error tier (D1–D4 + N12/N13) + pre-TTS gate | `diag/*`, `assets/resolve.go`, `cli/check_cmd.go` | 8 | the §2/§4 live inputs behave per spec; all examples lint clean; `check` blocked-with-0-HTTP verified by request counter | low |
| **8** | `timeline` verb, `EventDoc{CameraFocus, LayoutPreset, ID}` (D8) | `inspect/`, `cli/` | 5 | every `srcLine` hand-checked against one example; no raster keys leak | low |
| **9** | Per-frame caches: overlay + compare(×2) + counter + compare-label (copy `watermark.go` idiom); bounded TTS concurrency | `render/annotate.go:436,475,513,550`, `orchestrator.go:35` | 4 | frame time drops by the measured 19.9 ms × layers; cold iteration drops materially; frames byte-exact | low |
| **10** | `snapshot` + manifest + sheet + cursor/`-hand` fallback fix (D5) | `snapshot/*`, `cli/`, `main.go:216-247` | 8 | ≤ 24 PNGs + sheet, warm, < 5 s, zero ffmpeg processes; `cursor=magic_wand` no longer deletes the hand | medium |
| **11** | **Genre examples + templates** (one canonical whiteboard, one mascot-talking, one meme-beat) — *after* lint so they can be asserted lint-clean | `examples/`, `AGENTS.md` | 3 | each example renders and lints clean; the doc's minimum path is ≤ 5 k tokens | low |
| **12** | **W3a identity + lifetime**: `id=`, `dur=`, `exit=` (exit = a **second event**, mirroring `EVENT_ARROW`+`EVENT_ARROW_STATIC`), `Layer`/sticky field honoured by all three cut loops (`:219-238, :417-436, :702-713`) | `model/types.go`, `compile_actions.go`, `dsl.go` | 8 | mascot expression-swap renders without ghosting; `[clear]` and `[transition:]` keep the character layer; `E0008 duplicate_id` | medium |
| **13** | **W3b element addressing**: `Anchor(id)` resolved at **compile** time; `parsePoint/Region` accept ids; arrow/highlight/zoom-to-element consume them | `annotate.go:17-43`, `compile_actions.go` | 8 | `[arrow:@robot:@text_1]` and `[highlight:@robot]` land on the element, not the canvas | medium |
| **14** | Named anchors/thirds/size-only placement (resolution independence) + expose `ColorHex` | `model/layout.go`, `script/parser.go` grammar | 8 | one script renders sanely at 1080p and 720p; colours reachable from the DSL | low |
| **15** | `[caption:]` (px-means-px, outline, timed exit), `[sticker:]` (pop-in **and out**) | `render/text.go`, `compile_actions.go`, `constant.go` | 7 | meme beat renders without `[clear]`; text size means pixels | medium |
| **16** | Determinism suite, `-diff` (event-relative, needs 12), `-from mp4`, paint cache *only if* `[gen:]` is in the workflow | `render/*_test.go`, `snapshot/{diff,mp4}.go`, `builder/paintcache.go` | 12 | `TestRenderFrameOrderIndependent` green incl. lazy-asset paths | low |
| — | **Dropped / moved**: raster disk cache; `-timing estimate` as a *visual* path; scenes/variables/include as a block later | — | — | — | — |

Sequencing rules the plan currently violates, restated: `diag` must precede the lint step (B8); `RasterKey` must not share a commit with the sink refactor (B7); `exit=` must come after `id=` (lifetime is meaningless unaddressed); `Anchor(id)` must resolve at compile time so `FrameEvent` stays self-contained — which is what preserves the plan's own determinism contract (§4.3).

---

## 8. Top 5 quick wins

1. **`AGENTS.md` + `SKILL.md` describing today's CLI and the verified trap list — 3 h.** Highest product pull per hour in the entire programme, zero code risk, and it is the *only* thing that stops an agent writing `[arrow:robot]` today. The HyperFrames session cost ≈5 k tokens on two skill files; zen-board has none, and one of its six examples doesn't even render (D6).
2. **The three crash fixes + cache key (B6, D1, B4) — 5 h.** `[arrow:TL]` and `[compare:onlyone]` panic in `builder`; nested tags panic in `script`; `svg.CacheKey` returns 4 distinct keys per 200 identical calls. Every downstream agent tool is worthless if the binary dies on a typo.
3. **Lint the silent-failure tier, not just the 6 planned codes (D4) — 5–8 h.** "0 events, exit 0, TTS clean" is precisely the failure an agent cannot self-diagnose; this raises the *correctness of the agent's output*, not just the quality of its report.
4. **Sink fix + `snapshot`/`check`/`timeline` verbs (B1–B3, then step 10) — 10–14 h.** PNGs are the agent's only eyes; the plan's core instinct, executed with the deadlock and buffer-ownership corrected and a byte-exact regression gate.
5. **Per-frame scale/text caches by copying `watermark.go` (D7) + bounded TTS concurrency (N9) — 4 h.** 19.9 ms/frame/layer → ~0, and 83 % of cold iteration is one HTTP call at a time. Larger real-world iteration win than the entire plan Group D, at a quarter of the cost, with no new cache-invalidation surface.

**Then the product step:** 12 → 13 → 15 (identity/lifetime, element addressing, meme primitives). Without them, `snapshot` is a fast way to look at a mascot that ghosts, an arrow that points at nothing, and a caption that never leaves.

---

## 9. Keep-as-written (the plan is right about these)

Sink-over-state-extraction (Q10/R3) · four verbs, no `doctor`/`explain` (Q7) · `os.Stdout = os.Stderr` swap instead of the 35-site `*cli.UI` refactor (correctly avoids `cli→diag→builder→cli`) · `-fast` refused on inspection verbs (premise **proven**: an authored `@600,500` rect goes off-canvas at half resolution) · raw-`Pix` container over PNG for any future raster cache (premultiplied round-trip) · manifest-hash dir wipe (F7) · `FrameAt` epsilon (F14) — **but** unify it with `processAction`'s `int(triggerTime*fps)` truncation (`compile_actions.go:178`), or `snapshot -at <event start>` lands one frame early · no `Seed` · `ErrorInfo.Hint` for environment failures · one `Engine` per invocation (`renderer.go:144` closes `Pool.Jobs`).

## 10. Not resolved here

`estimate`-mode rate constant per voice (plan's open question 1 stands; `TestEstimateCalibration` reading a developer's TTS cache is not a CI test — assert a ±25 % bound against a **checked-in** fixture cache and put the per-voice rate in `Project`, not `constant.go`).
Whether `check` also writes `plan.json` (plan's open question 2 — agreed deferred; it is one line once `timeline` exists).
