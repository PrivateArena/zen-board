# ARCHITECTURE PLAN — Quick Wins: Agent Ergonomics + Genre Expressiveness (`zen-board` × HyperFrames / competitor roadmap)

**Slug:** `quick-wins-hyperframes-and-competitors` · **Date:** 2026-10-24 · **Owner:** zen-board
**Inputs:** `REVIEW_ARCHITECTURE_PLAN_agent-verify-loop.md` (3 verification passes; all `[VERIFIED]` facts cited below come from it), `/media/jang/home/Deve/zen-hyperframes/REPORT.md` (hands-on HyperFrames 0.8.208, two real videos), `RESEARCH_hyperframes_improvements.md` §3, `STUDY_COMPETITOR_script-whiteboard-video.md` §4 (#1 agent kit, #2 fast feedback, #4 aspect presets, #5 starter pack).
**Relation to `ARCHITECTURE_PLAN_agent-verify-loop.md`:** this plan **supersedes its build sequence** and narrows scope. What it keeps, it keeps with the review's blockers fixed. What it defers is listed in §9. Shared-file collision rules with that plan are in §10.

---

## 1. Goal and thesis

**Goal:** an AI agent can reliably produce a *structured* whiteboard video — narration-synced drawing, a recurring talking mascot, meme beats — and know what the screen looks like without paying for an encode.

**Thesis `[VERIFIED]`:** zen-board already owns the hard part (deterministic pure-function frame core, audio-first timing, single binary, no browser). Two things block the goal, and neither is the render engine:

1. **The loop has no eyes and no gate.** No `AGENTS.md`, no `skills/`, no `docs/` — an agent starts every session by reading a 290-line doc; the only feedback artifact is an MP4, which a text agent cannot read.
2. **The DSL cannot express the target genres.** Mascot expression swaps *ghost* (every layer is `FRAME_FOREVER`), `[clear]` is the only removal and it wipes the board, arrows/highlights *cannot address an element*, `[text:]` has no colour/outline/real size, nothing has an exit animation.

HyperFrames' own hands-on verdict is that `check --json` + `snapshot` PNGs + `init`/`catalog` are "the product". zen-board's equivalent of `catalog` is not 448 HTML blocks — it is **identity + lifetime + anchors + genre templates**, because the engine is one deterministic compositor, not a DOM.

---

## 2. Baseline and target (measured on this machine, `examples/demo.zen` 1080×720@30, ffmpeg 6.1.1, 16 cores)

| Metric | Today `[VERIFIED]` | After W1 | After W2 | After W3 |
|---|---|---|---|---|
| Cold first render (TTS cache empty) | 19.5 s wall — TTS 16.3 s (**83 %**), encode 3.0 s | ~7–9 s (bounded TTS concurrency) | same | same |
| Warm re-render | 3.2 s (encode-pipe-limited: `Total time writing to FFmpeg pipe: 2.98 s`) | 3.2 s | 3.2 s | 3.0 s |
| **Cost to learn a tag is wrong** | full encode, then *the video looks fine, the mistake is invisible* | **~0 s**, `check -json`, exit 10, no HTTP | same | same |
| **Cost to see frame 372** | not possible without a full encode + an MP4 reader | n/a | **PNG in < 5 s** | same |
| Malformed script | **process panics** (3 live panic sites) | exit 10 + `file:line:col` | same | same |
| Persistent-layer frame cost | 19.9 ms/frame **per** 1024² layer (8 layers ≈ 151 ms/f, 16 ≈ 302 ms/f) | unchanged | unchanged | **~0** (per-frame cache, W0) |
| Mascot video, 30 s | not expressible (ghosting, no removal, invisible under `[zoom:]`, no hand) | not expressible | expressible-but-broken, visible | **6 tags, no ghosting** |

Frame CPU split `[VERIFIED]`: `Crop & Scale 79.2 %` (camera on, bilinear), `Scale Asset 11.9 %`, mask/img 4.4 %. Peak RSS 1.50 GB. `go test .` = 3.4 s → the whole integration suite is cheap enough to gate every step.

**Note on fast-mode crop:** `engine.FastMode` already switches `CropAndScale` to nearest-neighbour (`camera.go:70`, set from `-fast`/`-preview` at `main.go:248`). `snapshot` **must not** use it — the parity contract (§6 W2) is "the PNG the encode would write". Downscaling happens in the sink, after render.

---

## 3. Benchmark → gap → what ships here

| HyperFrames / competitor capability | zen-board today `[VERIFIED]` | Ship in this plan |
|---|---|---|
| `check --json` single gate, `Fix:` lines, non-zero exit | none; 3 live panic sites; "0 events, exit 0" is the common failure | **W1** `check` + `diag` lint + exit codes |
| `snapshot` PNGs + contact sheet ("grid view for AI review") | impossible — ffmpeg pipe is the only output | **W2** `FrameSink` + PNG sink + sheet |
| `init` + `catalog` (448 blocks) = blank-canvas solved for agents | 6 examples, **one of which cannot render** (`advanced.zen` uses asset `test`, absent) | **W0** genre examples + **W3** building blocks that make them work |
| `SKILL.md` corpus, ≈5 k-token minimum path | no `AGENTS.md`, no skills; 290-line API doc | **W0** `AGENTS.md` + `skills/zen-board/SKILL.md` (≤ 1.2 k tokens) |
| Deterministic render (3× identical MD5) | already true — keep, and *prove* it (byte-exact PNG gate) | **W2** regression gate |
| Whiteboard genre is a *composition of blocks*, 9/10 | the engine is best-in-class here (reveal masks + travelling hand) — but the hand is **missing in 2 of 6 examples** | **W0** hand/cursor fallback |
| `data-var-src` / variables / batch, competitor roadmap #3 | none (line-scoped parser, no include/var/scene) | **deferred §9** (real value, not on the path to a correct mascot video) |
| Aspect presets, competitor roadmap #4 | raw `-w/-h` only; `@x,y,w,h` absolute → resolution-fragile | **W3** named anchors + size-`%` (prerequisite for ratios; ratio presets themselves deferred) |
| Studio/preview scrub | `NewPreviewPipe` exists, no frame addressing | deferred (verify-loop plan) |

---

## 4. Binding decisions

| # | Question | Decision |
|---|---|---|
| Q1 | Do we keep the verify-loop plan's 4-verb CLI + `session`? | **Verbs yes, `session` not yet.** `render` / `check` / `snapshot` (+ `assets`, unchanged) route through a thin `internal/cli` router. `session.Open`, `ctx`, estimate-timing and temp-ownership belong to the other plan (§9). |
| Q2 | Does `Run()` become `os.Exit`? | **No.** `integration_test.go` calls `Run()` in-process at 6 sites (`:64,133,191,272,348,407`). `os.Exit` stays in `main()` only. |
| Q3 | Regression gate for the sink refactor? | **Byte-exact PNG** of frame 0 / mid / last for every example, pre- vs post-refactor (`image/png` is byte-deterministic across processes `[VERIFIED]`). PSNR is *weaker and hides the double-`Put` bug*; a hang is not a pixel difference. |
| Q4 | Who owns the pooled frame buffer? | **The collector.** A sink never `Put`s (review B3: `renderer.go:173` already Puts, and unzoomed frames are the same pointer as the pooled buffer, `camera.go:51-55`). A sink that retains a frame copies it. |
| Q5 | New event type for the mascot? | **No.** Two `FrameEvent` fields — `ID`, `Layer` (+ `Sticky`, `Dur/Exit` window) — and a compile-time *character track* pass. `EVENT_CHARACTER` rejected (§8 R2). |
| Q6 | Exit animation modelling? | **A second event**, mirroring the existing `EVENT_ARROW` + `EVENT_ARROW_STATIC` pair (`compile_actions.go:571-590`). Retroactive `EndFrame` rewriting is what makes `[overlay:]` unremovable today; we do not add to it. |
| Q7 | Backward compatibility of the DSL? | **Strict.** Every existing `examples/*.zen` must compile to an identical event list (new fields zero-valued). Legacy positional forms (`[draw:x:TR:3]`, last-integer-as-duration, `timeline.go:188-197`) keep working; named params win when both are present. |
| Q8 | Lint scope | Text-only rules over the raw `.zen` bytes (never over cleaned `ScriptLine.Text`) + the DSL table. Error tier = the 14 codes in §5; **only `error` blocks**. Warnings never block (narration may legitimately contain `array[0]`). |
| Q9 | Sequencing | 4 waves, 6 checkpoints, report at each. W0 ships with **zero** architectural change; W3 ships after W1 so its new params are validated the day they exist. |
| Q10 | `[sfx:]` | Documented and a total no-op (`compile_actions.go:57-58`). **Ship `E0014 directive_noop` now** (so agents stop writing it into a silent video); real muxing deferred (§9). |

---

## 5. DSL target grammar (authoritative spec; the Go table in W1-B owns it)

Unified tail, backward compatible:

```
[ <tag> : <target> ( : <part> )* ( @ <coords> )? ]
   <part>  ::= <preset> | <key>=<value> | <bare-legacy>
   <key>   ::= id | dur | exit | h | w | size | anchor | color | fit | layer | keep | cursor | mask | hand
   <value> ::= [-\w.%#=]+            ← the missing "." is defect D3
   <coords> ::= x,y | x,y,w,h
   <element-ref> ::= @<id>           ← new; resolves at COMPILE time
```

| Verb | New capability | Example |
|---|---|---|
| all placement tags | `id=` (addressable), `dur=` (window), `exit=fade\|pop\|slide` (second event) | `[draw:robot:TR id=bot dur=6 exit=fade]` |
| draw / text / overlay / caption / sticker | named anchors + size-only, resolution-independent | `[character:robot_happy anchor=br size=45%]` |
| text / arrow / highlight / caption | `color=#hex` (field `FrameEvent.ColorHex` already exists, unreachable) | `[highlight:@bot:circle:2 color=#EB4034]` |
| arrow / highlight / zoom / caption | element addressing `@id` | `[arrow:@bot:@curve:curved:2]`, `[zoom:@bot]` |
| `character` | persistent camera-independent sticky layer + **state switching without ghosting** (same `id` ⇒ previous state's window is closed at the new state's start frame) | `[character:robot_shocked anchor=br size=45%]` … later `[character:robot_thinking anchor=br size=45%]` |
| `caption` | meme/impact text: px means px, outline, box, align, timed exit, overlay layer | `[caption:"IT WEDGES"@top color=#FFFF00 outline=6 size=110 dur=2.5 exit=pop]` |
| `sticker` | pop-in **and** out, `fit=`, coords | `[sticker:meme_lol anchor=tr w=280 pop dur=2 exit=pop]` |
| `clear` | board-only wipe keeps characters; `[clear:all]` wipes everything | `[clear]` after a scene change, mascot stays |

`[overlay:]` keeps its current meaning (backward compat) and is documented as the low-level primitive; `[character:]` / `[sticker:]` are the agent-facing spellings.

### Lint code table (`internal/diag`, error tier unless marked)

| Code | Condition | Today's behaviour `[VERIFIED]` |
|---|---|---|
| `E0001` | unknown asset id | hard error **after** TTS is spent |
| `E0002` | `[prefix:…]` not consumed by any pattern, prefix within edit-distance 2 of a known tag | read aloud by TTS |
| `E0003` | overlapping/unclosed tag spans | **panic** (`parser.go:229`); 9 of 10 nesting shapes |
| `E0004` | malformed coords / wrong part count | silent or absorbed |
| `E0005` | unknown preset **or anchor** | silently = full canvas; and the same unknown name becomes a bogus camera state |
| `E0006` | unknown style / mask / hand / **cursor** keyword | `cursor=magic_wand` in 2 examples ⇒ **no hand drawn at all** |
| `E0007` | `:<num>.<num>` duration the legacy regex cannot match | 0 actions + read aloud |
| `E0008` | tag consumed → **0 events** (`[text:…]` without two quotes) | silent, exit 0, clean TTS |
| `E0009` | arity below `ActionSpec.MinArgs` (`[arrow:TL]`, `[compare:onlyone]`) | **panic** (`compile_actions.go:568`, `:631`) |
| `E0010` | `@coords` on a tag that has no coord group (`slide`, `overlay`, `watermark`) | coords absorbed into the asset id → hard error or silent miss |
| `E0011` | SVG asset + coords without w,h | **invisible**, `timeline.go:480-482` `continue`s before rasterizing |
| `E0012` | unknown `[zoom:]` name | silently makes other events invisible (`engine.go:165-194`) |
| `E0013` | duplicate `id=`, or `@ref` to an id never defined / defined later | W3 |
| `E0014` | directive is a documented no-op (`[sfx:]`) | silent no-op |
| `W0001` | literal `[...]` in narration that is a plausible code token (`array[0]`) | read aloud — warning only, never blocks |
| `W0002` | `exit=` without `dur=`/`id=` | W3 |
| `I0001` | snapshot truncated by `-max` | W2 |

---

## 6. Implementation blueprint

`Depends on` = step numbers in §7.

### W0 — no architecture, immediate payoff (~1.5 days)

| File | Action | Concrete shape | Acceptance ("done when") |
|---|---|---|---|
| `internal/svg/render.go` | modify | `CacheKey`: collect `k=v` into a slice, `sort.Strings`, then hash. Today it iterates a map ⇒ **4 distinct keys per 200 identical calls `[VERIFIED]`** | `TestCacheKeyStable` — 1000 calls over the same 4-entry `Variant` → 1 key |
| `internal/builder/compile_actions.go` | modify | guard arity in `handleArrow` (`:568 to := parts[1]`) and `handleCompare` (`:631 right := parts[1]`): return a typed `errArity` the compiler turns into `E0009` | `[arrow:TL]` and `[compare:onlyone]` no longer panic; they produce a finding |
| `internal/script/parser.go` | modify | span-conflict rejection in `extractActions` (`:229`): sort matches by start, require `t.start >= lastPos`, else record a `spanConflict` diagnostic instead of slicing `line[lastPos:t.start]`. **All 21 patterns × 20 others**, not the text+draw pair the other plan cites | the 9-shape panic matrix (`[text:"a [draw:x] b"]`, unquoted, `[slide:…]`, `[banner:…]`, `[chapter:…]`, `[erase:…]`, nested `[wait:1]`, nested `[clear]`) returns diagnostics, never a panic; `Parse` output byte-identical on `examples/*.zen` |
| `internal/render/engine.go` + `main.go` | modify | hand/cursor fallback: if the requested cursor preset is absent **or** `engine.Hand == nil` (`engine.go:433-442`), fall back to `assets/cursors/hand/` and to the shipped `assets/hand.png`; `main.go:226` currently uses the sprite only if the path differs from the literal default `"./assets/hand.png"` | `examples/demo.zen` and `advanced.zen` (which request `cursor=magic_wand`) visibly draw a hand |
| `internal/tts/orchestrator.go` | modify | `semTTS := make(chan struct{}, 1)` (`:35`) → `constant.TTS_MAX_CONCURRENCY` (default 4, flag-configurable); results are already indexed by `job.Index`, ordering is preserved by the `results []*SynthResult` slice | cold `demo.zen` TTS stage < 6 s; warm output byte-identical; word-timing concatenation unchanged (`integration_test.go` green) |
| `internal/render/annotate.go` | modify | per-frame caches by copying the shipped idiom in `watermark.go:44-56,76-86`: overlay scale (`:513`), the two full-canvas `scaleImageFill` in compare (`:436-437`), and the two per-frame `RenderText` calls (compare label `:475`, counter `:550`). Keys `id_w_h` / `text_font_size_bold` into `e.ScaledAssets` under `AssetMu` | measured 1080p frame time drops by ~19.9 ms × layer count; **frames byte-identical** before/after |
| `examples/advanced.zen` | modify | asset `test` → `robot` (`:2,:8,:10`); today `validateAssets` hard-errors (`timeline.go:286-288`), so 1 of 6 authoring examples cannot render | it renders |
| `examples/genre_whiteboard.zen`, `examples/genre_mascot.zen`, `examples/genre_meme.zen` | create | one canonical script per target genre, using **only assets that exist** (`city pyramids robot world` + `comic_*`) plus the new W3 tags where required; each ≤ 20 lines | each renders end-to-end; after W1 each lints to **0 errors** |
| `AGENTS.md` (root) | create | today's real CLI + the 5-verb loop *as it will exist* + the verified trap list (§5's "today's behaviour" column) + two rules: *"run `check` before you spend TTS"* and *"never claim a frame looks right without a PNG you actually opened"*. **Budget ≤ 1.2 k tokens** — HyperFrames' own session cost ≈19 k read, ≈5 k minimum viable | every documented command executed by hand; doc length measured |
| `skills/zen-board/SKILL.md` | create | the copy-pasteable loop + the DSL table extracted from W1's `dsl.go` once it exists (second pass, step 11) | matches `--help` output exactly |
| hygiene | modify | `gofmt -l .` (currently flags `internal/render/watermark.go`); `.gitignore` += `frames/` | `gofmt -l .` empty |

### W1 — the gate and the contract (~2 days)

| File | Action | Concrete shape | Acceptance |
|---|---|---|---|
| `internal/script/source.go` | create | `Source{Path string; Raw []byte; Lines []Line}`; `Line{Number, Offset, Length int; Text string}`; `Load`, `Locate(byteOffset) (line, col int)`, `Snippet`, `Hash` | `Locate` matches what an editor shows on a non-ASCII fixture |
| `internal/script/dsl.go` | create | the **single source of truth** for the grammar: `ActionSpec{Name, Tag, Status, Patterns []*regexp.Regexp, Parts []Param, MinArgs, MaxArgs, AssetArgs []string, EmitsEvents []string}`; `Param{Name, Kind, Default, Enum}`; `func All()`, `ByTag(tag)`, `Match(raw)`. Owns the 21 patterns now inlined at `parser.go:12-32` | (i) every `ActionSpec` matches a canonical fixture string; (ii) the set of tags `dispatch` routes to a **non-`default`** handler equals the spec set (derive by routing, not AST grep — the old "every `case` is reachable" formulation silently excluded bare `[draw:]`); (iii) `TAG_ERASE_ALL`/`TAG_ERASE` share one spec with 2 patterns — the test must allow it explicitly |
| `internal/script/tag.go` | create | `SplitParts` (quote-aware, the logic inlined at `parser.go:47-67`) + `ParseTagSpec(raw, spec) (Tag, error)` used by **both** the compiler and lint — today the split exists twice (`timeline.go:188-197`, `compile_actions.go`) | one split implementation repo-wide (`grep -c 'strings.Split(parts' internal/builder` → 0 for tag parts) |
| `internal/model/types.go` | modify | `DrawAction` += `SrcOffset, SrcLength, SrcLine, SrcCol int; SrcRaw string` (**string identity, never in `AssetVariant`** — that map is aliased by `preprocessor.go:66-76` and `extractCursor` mutates it `[VERIFIED]`). `FrameEvent` += `ID string; SrcLine, SrcCol int; RevealDuration float64` (+ json tags: `FrameEvent` is the untagged struct, `Project` already has them) | `TestSrcLinePropagation` on every example; `RevealDuration` replaces the positional hack at `timeline.go:188-197` and `DrawAction.RevealDuration` (dead since birth, `types.go:56`) is finally read |
| `internal/diag/finding.go` | create | `Finding{Code, Severity, Message, FixHint, File string; Line, Col int; Snippet, Stage string}`; `Report` with **pointer-receiver** `Add`, `Extend`, `Blocked()`, `ByCode`, `Sorted`; `String()` → `demo.zen:14:22: error[E0008]: … \n  hint: …` | `Blocked()` true iff any `error`; existing 9 `log.Printf("Warning:` sites in `internal/builder` are routed into the report instead |
| `internal/diag/lint.go` + `rules.go` | create | `Lint(src *script.Source, spec []script.ActionSpec, inv *Inventory) Report` — tokenizes `src.Raw`, masks consumed spans, emits §5's codes | the 5 live inputs (`[drw:robot]`, `[draw:robot:2.5]`, `array[0]`, `[text:"a [draw:x] b"]`, `[overlay:x:1.0:BR:3.0]`) behave per spec; all examples lint to 0 errors after W0 fixes |
| `internal/assets/resolve.go` | create | `Resolve(assetsDir, idx, id) (path string, ok bool, fromIndex bool)` — the rule duplicated at `timeline.go:243-253, 271-279, 394-411, 426-434` (**four** sites, not three) | lint and compile resolve identically, asserted on a `.svg`-on-disk-but-not-in-index fixture |
| `internal/envelope/{envelope,errors}.go` | create | `Envelope{ok, command, version, script_hash, findings, artifacts, stages, data, error}`; `EnvError`→3, `UsageError`→2, `BlockedError`→**10**, generic→1 (today `main.go` exits 1 for everything, so 1 is preserved) | `ExitCode` table test; envelope is the only thing on real stdout under `-json` |
| `internal/cli/{app,flags,redirect}.go` | create | `App.Run(args) int`; verbs `render`, `check`, `snapshot`, plus pass-through to `assets.RunCLI`; `RedirectForJSON()` = `os.Stdout = os.Stderr` swap (the 35-site `*cli.UI` refactor is rejected — it created the `cli→diag→builder→cli` cycle) | `integration_test.go` **unmodified and green** (`Run() error` kept, Q2); a twice-in-process `-json` invocation yields exactly one JSON doc each time |
| `internal/cli/check_cmd.go` | create | parse → lint → (unless `-fast`) asset-inventory resolution → **stop**. No TTS, no HTTP, no compile | `check` on an unknown-asset script exits 10 having issued **0 HTTP requests** (assert with a counting mock in `internal/testutil`) |

### W2 — eyes (~2 days)

| File | Action | Concrete shape | Acceptance |
|---|---|---|---|
| `internal/builder/sink.go` | create | `FrameSink{Wants(frame int) bool; WriteFrame(*image.RGBA) error; Close() error}`; `PipeSink` wraps `ffmpeg.Pipe.WriteFrame` (`pipe.go:135`, kept untouched); `Whitelist map[int]bool`. **Ownership rule in the doc comment: the collector owns the buffer; a sink never `Puts`; a sink that retains must copy** | compile-time `var _ FrameSink = (*PipeSink)(nil)`; `TestSinkOwnership` fails if a sink `Put`s |
| `internal/builder/renderer.go` | modify | `pipe *ffmpeg.Pipe` → `sink FrameSink`. **Collector driven by the ordered whitelist slice, not `nextFrame++`** — otherwise a skipped feed frame never produces a result and `renderer.go:150-176` blocks forever. The `-eventlog` `prevActive` scan (`:120-131`) moves to a separate full-range pass so `-eventlog` keeps its meaning. `pipe.Close()` → `sink.Close()` | `TestSinkWhitelistSkipsRender` (a counting sink proves non-whitelisted frames reach no pixel work); **byte-exact PNG of frame 0 / mid / last for all `examples/*.zen`, pre- vs post-refactor** (Q3) |
| `internal/inspect/frameplan.go` | create | `Plan(fps, totalFrames int, events []model.FrameEvent, r Request) (frames []int, dropped int, err error)` — pure over plain data. Selectors `-at`, `-every`, `-events start\|mid\|end\|all`, `-frame`, cap `-max 24` **before any pixel work**. Precondition: `EndFrame` already clamped to `totalFrames-1`; `mid`/`end` of a `FRAME_FOREVER` event resolves to `totalFrames-1`, never 999998 | `-every 0.5s` on a 1-hour timeline renders ≤ `-max` frames; `-events end` on a `[draw:]` picks the last frame, asserted |
| `internal/inspect/pngout.go` | create | `PNGSink` (implements `FrameSink`): copy → optional downscale (`-max-w`) → `png.Encode` (BestSpeed) → name `f%06d_%07.2fs.png` → record `Artifact{path, frame, time_sec, kind: "engine", label}`; then write `manifest.json` `{script_hash, timing, version, frames[]}` and a contact sheet ≤ 1568 px wide (a vision model's native tile) | `snapshot -events end examples/v3_test.zen` → ≤ 24 PNGs + sheet, **< 5 s warm, zero ffmpeg processes**; a stale PNG from an older script cannot survive the run (hash differs ⇒ dir wiped) |
| `internal/cli/snapshot_cmd.go` | create | `Open`-free: parse → TTS **from cache only** → compile → prepare → `RenderTimeline(sink)`; refuses `-fast` (`main.go:110-120` halves the canvas before compile and misplaces every absolute `@x,y` `[VERIFIED]`); refuses a cold TTS cache with an actionable `E`-class hint instead of silently issuing 8 HTTP calls | documented error paths; `snapshot` never spawns ffmpeg |

### W3 — expressiveness (~4 days; this is the product)

| File | Action | Concrete shape | Acceptance |
|---|---|---|---|
| `internal/model/types.go` | modify | `FrameEvent` += `Layer string` (`board` default \| `overlay`), `Sticky bool`, `ExitFrames int` | see tests below |
| `internal/script/dsl.go` | modify | new params `id, dur, exit, anchor, size, h, w, color, fit, layer, keep`; `@id` element-ref kind | `TestRoundTrip` on every new form |
| `internal/builder/compile_actions.go` | modify | (1) assign `ID` at construction in every handler (`id=` supplied, else `fmt.Sprintf("%s#%d", kind, n)` from a per-compile counter) — **do not** reuse `TargetImage`, which is an asset id for draw but a synthesized `__text_N`, and content-smuggled (`__banner_t\|s\|c`, `__watermark_src\|pos`) for others; (2) `handleCharacter` = overlay-layer event + `Sticky` + **track pass**: a new state with the same `id` closes the previous state's `EndFrame` at the new start frame (anti-ghosting); (3) `exit=` emits a **second event** (the `EVENT_ARROW`+`EVENT_ARROW_STATIC` pattern); (4) the three cut loops (`handleEraseAll` `:219-238`, `handleClear` `:417-436`, `handleTransition` `:702-713`) honour `Sticky` — today they rewrite `EndFrame` for **every** earlier event, which is exactly why `[overlay:]` is unremovable and why a transition eats the mascot mid-wipe | mascot state swap renders **without** ghosting; `[clear]` keeps characters, `[clear:all]` does not; `[transition:]` no longer drops the character at its midpoint |
| `internal/render/engine.go` | modify | before the visibility test (`engine.go:165-194`), `Layer == overlay` ⇒ `visibility = 1.0` (the mascot stops disappearing when `[zoom:]` points elsewhere); draw order: stable sort by `(layer, index)` done **once at compile**, never in `RenderFrame` (determinism contract) | a character placed before a full-bleed slide stays visible and on top |
| `internal/builder/anchor.go` | create | `func (c *timelineCompiler) Anchor(id string, atFrame int) (image.Rectangle, bool)` — resolved at **compile time**, so `FrameEvent` stays self-contained and the render path keeps its purity; backs `E0013` | `[arrow:@bot:@curve]`, `[highlight:@bot]`, `[zoom:@bot]` land on the element, not the canvas — today `parsePoint`/`parseRegion` (`annotate.go:18-44`) fall through `GetPresetLayout`'s `default` and **silently use the whole canvas** |
| `internal/model/layout.go` | modify | named anchors as *boxes* (`l/third-l/third-c/third-r/mr/br/…`, computed from canvas size, no hardcoded pixels) + `LayoutPresets()` returning the authoritative list for `E0005`. Size-only placement = anchor box + `fit=`, so the engine letterboxes from the real image bounds (no compile-time asset size needed) | one script renders sanely at 1920×1080 and 1280×720 |
| `internal/render/text.go` | modify | outline + `px-means-px` blit path (bypass the contain-fit that makes `fontSize` cosmetic today, `engine.go:281-322`); reuse for `caption` | `[caption:…] size=110` produces 110-px glyphs |
| `internal/builder/compile_actions.go` (`handleCaption`, `handleSticker`) | create | the two agent-facing sugar tags; `color=` wired to the already-existing `FrameEvent.ColorHex` (2-hour win, currently unreachable from the DSL) | meme beat: caption in/out, sticker in/out, no `[clear]` required |
| `internal/inspect/timeline.go` | create | `TimelineDoc{chapters, events[{index, type, asset, id, start/end sec, rect, cameraFocus, layoutPreset, srcLine}], gaps, words}`. **`ZoomFocus` is split into `CameraFocus` and `LayoutPreset`** — it is overloaded today (overlay `:683`, counter `:741-769` store a *layout* key that the camera code also reads), so a naive dump would report a box as a camera move and teach the agent wrong | `timeline -json`: every `srcLine` hand-checked against one example; asset column shows `robot`, never an `e3b0c4_400_300` raster key |

---

## 7. Build sequence

| # | Step | Requires | Done when | Risk |
|---|---|---|---|---|
| 0 | W0 crash trio: `CacheKey` sort, arrow/compare arity, parser span-conflict (all patterns) | — | panic matrix green; `go test ./...` green; `gofmt -l .` empty | low |
| 1 | W0 perf/ergonomics: TTS concurrency, 4 per-frame caches, hand/cursor fallback | 0 | cold TTS stage < 6 s; frame time −19.9 ms × layers; hand visible in both examples; frames byte-identical | low |
| 2 | W0 content: `AGENTS.md`, `SKILL.md` (today's CLI + traps), `advanced.zen` fix | 0,1 | every documented command verified by hand | none |
| **CP-1** | *report:* the binary no longer crashes; an agent has a map | | | |
| 3 | Source model + offsets (`script/source.go`, `model/types.go`) | 0 | `TestSrcLinePropagation` green; `Parse` output unchanged on all examples | low |
| 4 | DSL table (`script/{dsl,tag}.go`) + compiler/lint share one splitter | 3 | `TestDSLCoverage` (routing-derived form), `TestPatternTableMatchesLegacy` on examples **+ a synthetic corpus** (the examples contain no nested brackets and no `:num.num`, so alone they would have caught neither flagship defect) | low |
| 5 | Envelope + exit codes + verb router (`envelope/*`, `cli/{app,flags,redirect}.go`); `Run() error` kept | 4 | `integration_test.go` unmodified + green; twice-in-process `-json` smoke test | medium |
| 6 | `diag` lint + `check` verb + pre-TTS gate | 4,5 | §5 codes behave; examples lint clean; `check` blocked ⇒ 0 HTTP (mock counter) | low |
| **CP-2** | *report:* script validated with 0 TTS spent | | | |
| 7 | `FrameSink` + collector-by-whitelist + **byte-exact** gate | 5 | byte-identical frame-0/mid/last PNGs for all examples; `TestSinkOwnership`, `TestSinkWhitelistSkipsRender` | **high** |
| 8 | `inspect/frameplan.go` + `PNGSink` + manifest + sheet; `snapshot` verb | 7 | ≤ 24 PNGs + sheet < 5 s warm; refuses `-fast`; refuses cold TTS cache with a hint | medium |
| 9 | `timeline` verb (`inspect/timeline.go`, `CameraFocus`/`LayoutPreset` split) | 6,7 | asset column canonical, `srcLine` verified | low |
| **CP-3** | *report:* the loop exists — `check` → `snapshot` → read PNG → edit | | | |
| 10 | Identity + lifetime + layers: `ID`, `Layer`, `Sticky`, `dur=`, `exit=` (second event), sticky-aware cut loops, character track | 6,7 | mascot swap without ghosting; `[clear]` keeps characters; `E0013` | medium |
| 11 | `Anchor(id)` + element refs in arrow/highlight/zoom/caption | 10 | `[arrow:@a:@b]` hits the elements | medium |
| 12 | Named anchors + size-`%` + `color=` exposure | 10 | same script saner at two resolutions | low |
| 13 | `[caption:]`, `[sticker:]`; genre examples rewritten to use them; `SKILL.md` DSL pass | 10-12 | meme + mascot examples render, lint clean, and match the doc | medium |
| **CP-4** | *report:* the target genres are expressible and verifiable | | | |

Total ≈ **11–12 engineer-days**, of which steps 0–2 (~1.5 days) already change the agent experience materially.

**Verification ladder:** L1 unit = `script`, `svg`, `diag`, `inspect/frameplan`, `model/layout` · L2 = `builder` sink + renderer byte-exact test, `integration_test.go` untouched until step 13 · L3 = the three genre `.zen` files rendered end-to-end with the mock TTS server (`internal/testutil`) · L4 = `go build ./... && go vet ./... && gofmt -l .` at every step.

---

## 8. Decisions and rejections

| Decision | Alternative | Why rejected |
|---|---|---|
| **R1** Verbs now, `session` later | adopt the whole verify-loop plan at once | that plan's steps 1–3 are load-bearing and its steps 5–10 are not, for this goal; landing `session.Open` + estimate-timing + `-diff` before the crash fixes puts the riskiest edit (`main.go:120-238`) first |
| **R2** `Layer`/`Sticky` fields, not `EVENT_CHARACTER` | new event type + new dispatch branch | the mascot needs (a) camera-independence, (b) `[clear]` survival, (c) draw order, (d) addressability. (a)/(c)/(d) come from fields + one sort; only (b) needs the cut loops to honour `Sticky` (~10 lines across 3 loops). A new event type would also have to be added to two `EventType` skip lists (`timeline.go:246,441`) or it raises phantom "missing asset" errors |
| **R3** `exit=` = a second event | extend the retroactive `EndFrame` rewrite | three compiler functions already rewrite earlier events' `EndFrame`; an exit animation needs frames *after* the cut, so the rewrite model cannot express it — and adding a fourth rewriter makes compile order dependence permanent |
| **R4** Byte-exact PNG gate | "PSNR ≥ 45 dB vs pre-change render" | step 7 changes only what happens *after* `RenderFrame`; `image/png` is byte-deterministic across processes. The loose gate is not conservative — it hides the known double-`Put` aliasing bug |
| **R5** `-timing estimate` not in this plan | ship it with `snapshot` | measured spread 3.5–7.9 vowel-groups/s (±40 %) and reveal/camera windows are anchored to *word starts*, so estimate error accumulates with line count → it is timeline/lint infrastructure, not visual infrastructure. `snapshot` requires a warm TTS cache and says so |
| **R6** Per-frame in-memory caches, no disk caches | raster disk cache (other plan, Group D) | `CacheKey` is non-deterministic until step 0; and warm iteration is already encode-limited at 3.2 s. The real waste is `scaleImage`/`RenderText` **per frame** — fixable with a pattern the repo already ships (`watermark.go`) |
| **R7** `[sfx:]` → lint error now, mux later | implement SFX in W3 | per-event audio means a second input map chain in `ffmpeg/pipe.go:16-63`; wrong wave. Meanwhile it is a documented tag that does nothing — an agent trap |
| **R8** Genre examples before templates/`new` | ship `zen-board new <template>` in W0 | a scaffolder that emits scripts the *next* step starts flagging is a liability; templates come after the lint exists, and the examples are the scaffolder's payload |

---

## 9. Explicitly deferred to `ARCHITECTURE_PLAN_agent-verify-loop.md` (do not duplicate here)

`session.Open` + `context`/SIGINT + ffmpeg **process-group kill** (no signal handling exists anywhere today; `ffmpeg.Close()` only `Wait()`s and is called only on the success path — real leak, real step, not a quick win) · `-timing estimate` + `EstimateLineDuration` calibration · `TargetImage` → `RasterKey` at all **17** lookup sites (its own step, with an SVG `overlay`/`slide`/`watermark` fixture — no example uses an SVG overlay, so nothing would catch a partial migration) · `Project.LightsAddr` replacing `localhost:8765` at `timeline.go:353` **and** `assets/cli.go:83,93,105` · paint/diffusion cache · `-diff`, `-from mp4` (measured engine-vs-decode PSNR 40.5 dB at crf18 — a codec sanity check, not an agent need) · determinism test suite · variables/batch/`[include:]`/scenes · `[sfx:]` real mux, ducking, loudness.

## 10. Shared-file collision rules

`FrameEvent`/`DrawAction` gain fields in **this order, in this plan first**: `ID`, `Layer`, `Sticky`, `ExitFrames`, `RevealDuration`, `SrcOffset/SrcLength/SrcLine/SrcCol`. The other plan's `AssetID`/`RasterKey` land afterwards, in their own commit (review B7). `internal/diag` is created here in step 6 and is a **leaf** package — `builder` may import `diag`, `diag` imports nothing above `model`/`assets`; no `cli` dependency from any pipeline package (that is the cycle the other plan already removed). `internal/envelope` is likewise a leaf. `AssetVariant` maps must never carry identity (aliased + mutated today).

## 11. Open questions

1. **Mascot rasters.** The genre examples need 3–4 expression states per character. Today that is 3–4 whole PNGs (`comic_*` exist for one caricature). Do we (a) ship one 4-state mascot in `assets/` with the plan, or (b) add `variants: {happy: file, shocked: file}` to `index.json` so a state set is one asset id with one file per state? (b) is ~5 h and is the cheaper half of `ARCHITECTURE_PLAN_svg_migration.md`'s idea without touching SVG rigging.
2. **`[clear]` semantics change.** Making `Sticky` survive `[clear]` is backward-compatible (no existing script sets `Layer`), but the *documentation* change is behavioural for readers. Confirm: default `[character:]` sticky = **yes**.
3. **Sheet format.** PNG vs JPEG for the contact sheet (HyperFrames ships `.jpg`; PNG is 2–4× the tokens for a vision model, and the sheet is the single most-read artifact). Proposal: `-sheet jpg` default, PNG on request.
4. **`snapshot` vs `-freeze`/`extendedDuration`.** Frame addressing must use the *extended* duration (freeze tail is real frames); confirm the frame-count probe before W2 ships.
