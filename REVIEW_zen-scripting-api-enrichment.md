# REVIEW: zen-board scripting API enrichment — feature-research review

**Date:** 2026-08-18
**Reviewer:** Zen (senior code reviewer) · **Scope:** enriching the `.zen` scripting API (draw/slide/text/...) toward professional-whiteboard-video parity
**Method:** codebase-research mapping → first-pass findings → empirical verification (throwaway module copy) → second opinion via browser.chat (claude) → reconcile

---

## 1. Summary of What's Being Reviewed and Its Blast Radius

This review assesses the feasibility of a feature-research whose goal is to enrich the existing `.zen` scripting API (`[draw]`, `[slide]`, `[text]`, `[arrow]`, `[banner]`, etc.) so zen-board can produce whiteboard explainers "on par with millions-subscriber YouTube channels", building on the competitive baseline in `STUDY_COMPETITOR_script-whiteboard-video.md` and the current DSL contract in `API_zen-board-script-and-config.md`. Because the enrichment will be implemented by *extending* the existing parser→compiler→render path, the blast radius is the entire scripting surface: `internal/script/parser.go` (regex → string-encoded `DrawAction.Tag`), `internal/builder/compile_actions.go` + `timeline.go` (tag re-split → `FrameEvent[]`), `internal/model/types.go` (`DrawAction`/`FrameEvent`), and every `handle*Event` consumer in `internal/render/*`. Findings #1–#4 and #A1 are **test-confirmed** (reproduced by running the real `Parse`/`CompileTimeline` against a throwaway copy of the module); the rest are **reasoned** from code reads. The review finds that the current surface contains two crash paths and three silent-corruption paths that any new-API work would inherit and multiply, that the tag vocabulary is maintained in **three** independent places, and that the highest-value first move is the validation/error-surface pathway (per the study's own gating rule), not new feature keywords.

## 2. Reconciled Findings Table

Taxonomy: **Kept** (both passes agree / >85% confidence) · **Refined** (sub-agent changed severity or root cause) · **Added** (sub-agent caught) · **Rejected** (either party's point, one-line rationale).

| # | Severity | Location | Finding | Taxonomy | Verification | Recommendation |
|---|---|---|---|---|---|---|
| 1 | **Critical** | `internal/builder/compile_actions.go:535`, `:598` | `[arrow:TL]` and `[compare:robot]` (single arg) panic: `parts[1]` unguarded on a 1-element slice. Plausible minimal LLM output crashes the whole render with no recoverable error. | Kept | **test-confirmed** (panic reproduced: "index out of range [1] with length 1") | Bounds-check `parts` (and `parts[0]` empty) in both handlers before indexing; add regression tests for every handler's minimal-arg form |
| 2 | Major | `internal/script/parser.go:241` vs `API_zen-board-script-and-config.md:80` | Documented trailing-`+` (`[draw:robot]+`, `+` outside the bracket) is not parsed; the literal `+` leaks into narration text and is **spoken by TTS**. Only inside-bracket `[draw:robot+]` works. Doc's own phrasing is ambiguous (no concrete example), so this is doc-drift + parser mismatch, not just a user error. | Refined (root cause broadened: doc ambiguity + parser) | **test-confirmed** (clean text `"Hello + world"`) | Pick one canonical syntax; fix parser or doc; strip stray `+` from clean text; add doc example that matches the test |
| 3 | Major | `internal/script/parser.go:81-264` | Unknown/mistyped tags (`[foo:bar]`) match no regex, stay in clean text, and are **spoken aloud by TTS** — a silent failure with no error path anywhere in `Parse()`. Worst-case for LLM authoring: a near-correct tag produces narration that says the tag verbatim. | Kept | **test-confirmed** (clean text `"Unknown tag [foo:bar] here"`) | Unmatched `[...]` tokens should become parse errors (or at minimum a warning + stripping); this is the core of the linter (#13/#A5) |
| 4 | Major | `internal/builder/compile_actions.go:496`, `:596` | `[banner:"Chapter: One":"Subtitle"]` shreds: compiler re-splits with plain `strings.Split(rest,":")` while the parser used quote-aware `parseQuotedStringParts` → title `"Chapter`, subtitle `One"`. Same raw-Split exposure in `handleCompare`. **Scope correction:** `handleArrow:533` has no quoted args (its bug is the panic #1); `handleText:147` does its own first/last-quote extraction and is *not* vulnerable. | Refined (scoped to banner + compare only) | **test-confirmed** (`__banner_"Chapter\|One"\|Subtitle`) | Use the parser's quote-aware split (or a shared tokenizer, #15) for every tag with quoted fields; regression tests with embedded-colon quoted args |
| 5 | Major | `internal/script/parser.go:34-52` | `Parse()` has no error surface; `strconv.Atoi` errors discarded with `_`. Malformed input silently degrades (zero coords, defaults) and only surfaces downstream — if at all. Every new API inherits this. | Kept | reasoned (code) | Change to `Parse(input) ([]ScriptLine, error)`; surface parse warnings/errors; prerequisite for the schema/linter pathway |
| 6 | Major | `internal/render/text.go:43-94` + `engine.go:275-307` | `RenderText` is single-line, no wrap, no measure, no align. At render time the texture is **contain-fit** inside the preset rect (aspect-preserved, centered), so long text does not overflow — it is **downscaled to illegible smallness** instead, and multi-line layouts are impossible. | Kept (mechanism refined from "overflow/clip" to "contain-fit shrink + no wrap") | reasoned (code) | Text engine must wrap (auto or `\n`), measure-to-fit, and support alignment; prerequisite for bullet lists (#11) |
| 7 | Major | DSL surface | No primitive shapes (rect/ellipse/line/flowchart node/callout box). The core whiteboard vocabulary is absent; only `arrow`/`highlight`/`compare` overlays exist. | Kept | reasoned | Add `[shape]` (or extend `[draw]`) with stroke/fill/rounded styles; high-impact feature gap |
| 8 | Major | DSL surface | No z-order/stacking control: draw order = timeline order, no bring-to-front/send-to-back; annotations always render over assets. | Kept | reasoned | Add z/layer field to `FrameEvent`; sort at compile time; expose `[layer]` or `:z=N` |
| 9 | Major | `internal/builder/compile_actions.go:494-528` | No DSL-level element color: `ColorHex` unexported, arrow/highlight colors hardcoded (red/orange). Worse: `handleBanner` computes `colorHex` locally and **never assigns `FrameEvent.ColorHex`** — it is smuggled into `TargetImage` via `fmt.Sprintf("__banner_%s\|%s\|%s")`, then re-split by the renderer. | Refined (strengthened: banner color is structurally smuggled, not just unexposed) | reasoned (code) | Expose `ColorHex`/palette through the DSL; stop encoding structured data in `TargetImage` (#A1) |
| 13 | **Major** (elevated) | `internal/script/parser.go:11-31` + `compile_actions.go:32`, `:44` | The tag vocabulary is maintained in **three** independent places (parser regexes, `isSpecialAction` prefixes, `dispatch` switch). This triplication is the *proven root cause* of #4 (two independent parses of the same grammar drifted) — a shipped consequence, not a hypothetical. | Refined (elevated Minor→Major) | reasoned | Single source of truth (the `zenapi` registry from `ARCHITECTURE_PLAN_docs-generator-regex-parser.md`); parser/compiler consume it |
| A1 | **Major** | `internal/builder/compile_actions.go:520` + `internal/render/engine.go:595` | Banner `TargetImage` is `__banner_<title>\|<subtitle>\|<color>` with a bare `|` delimiter; the renderer `SplitN(rest,"|",3)` re-splits it. Any agent-authored banner title containing `|` silently corrupts the split. Second instance of "structured data in a string field". | Added | reasoned (code, both sides read) | Replace with real `FrameEvent` fields (`Title`/`Subtitle`/`ColorHex`) — same fix as #9 |
| 10 | Minor | DSL surface | No scene/step/group primitives: progressive builds require hand-ordering tags; no erase-by-group; `[move]` is single-asset only. | Kept | reasoned | Design a `[scene]`/`[group]` block or step-gating before building complex diagrams |
| 11 | Minor | DSL surface | `[text]` has only the `ltr`-mask entrance, no bullets/lists, no text-on-panel except `[banner]`. | Kept | reasoned | After #6 (wrap engine), add list/bullet layout + entrance variants |
| 12 | Minor | `internal/builder/compile_actions.go:517` | Dead branch: `HasSuffix(ctx.action.Tag, "+")` can never be true (parser strips `+` first). Banner `+` is documented unsupported — confirm and remove. | Kept | reasoned (code) | Delete dead branch; note unsupported trigger in API doc |
| 14 | Minor | `internal/model/types.go:68-100` | `FrameEvent` is a 31-field grab-bag (sub-agent count), most nil/zero per event type; every new API adds fields. | Kept | reasoned | Bound it: struct-per-event-type or a `Params map[string]string` for per-type fields |
| 15 | Minor | `internal/script/parser.go:12-31` | Regex-per-tag parsing is fragile for quoting/nested grammar (evidenced directly by #4/#13). | Kept | reasoned | One tokenizer (quote-aware) producing structured tags, over regex proliferation |
| 16 | Minor | DSL surface | `[sfx]` is parsed but a no-op (`dispatch` case is empty). Competitor videos use marker/stroke SFX. | Kept | reasoned | Decide implement (opt-in) vs document-as-unsupported; do not leave silent no-op |
| A2 | Minor | `internal/builder/timeline.go:252`, `:278` | No path-traversal sanitization on asset IDs / `index.json` `entry.File`: resolved via `filepath.Join(assetsDir, id+".png")` unchecked. Exploitability currently limited (validateAssets existence-check gates it), but becomes a real read vector once `{{var}}` templating lands (study §6.3 flags injection already). | Added | reasoned | Reject IDs containing `..`, `/`, `\`, `:` at parse/compile; validate `index.json` entries |
| A3 | Minor | `internal/builder/compile_actions.go:252`, `:285`, `:217-237`, `:415-435`, `:655-690` | `handleErase`/`handleMove` walk `c.timeline.Events` backward per call; `handleClear`/`handleEraseAll`/`handleTransition` rebuild the whole slice per call → O(n²) on long "professional-parity" scripts. | Added | reasoned | Maintain a per-asset event index / iterate once per pass; re-benchmark before group primitives (#10) multiply events |
| A4 | Minor | `internal/builder/compile_actions.go` (all `ParseFloat` durations/opacities/counters) | No negative/zero-duration validation: a negative duration yields `end < startFrame`, silently inverting/nullifying events downstream. | Added | reasoned | Range-check durations/opacities/counter bounds at compile; error on invalid |
| A5 | Minor | test suite | No parser/compiler fuzz harness or CI gate for the tag grammar. LLM-authored scripts are adversarial-by-accident; #1 proves a one-token mistake kills the run. | Added | reasoned | Add `go test -fuzz` over tag grammar + golden compile tests; prerequisite for the schema/linter (#13) |
| A6 | Nit | `internal/script/parser.go:230` | `strings.Fields(currentClean)` recomputed from full accumulated text inside the tag loop → O(tags × text) per line. | Added | reasoned | Increment a word counter as words pass instead of re-splitting |
| A7 | Nit | `internal/builder/compile_actions.go:16-26` | `actionContext` "handlers must not mutate shared compiler state" is a comment-only invariant; nothing enforces it if dispatch is ever parallelized (frame pool already exists downstream). | Added | reasoned | Enforce or document as advisory; add a concurrency test if dispatch ever goes parallel |

*#12 (Minor) and #16 (Minor) share a root class — silent no-ops / dead branches that the API doc is honest about but the code does not resolve.*

## 3. Red-Team Critique Summary (browser.chat, provider=claude)

Second opinion was run against the concrete findings table + five source files (DSL spec, parser.go, compile_actions.go, types.go, competitor study), per the chat-research Message-Quality Gate. Its verdicts, labeled per taxonomy:

**Kept** (sub-agent agreed, unchanged): #1 (Critical, confirmed at exact lines), #3, #5, #7, #8, #10, #11, #12 (100% unreachable), #14 (counted 31 fields), #15, #16.

**Refined**: #2 (root cause is also doc ambiguity — no concrete example anywhere; not purely a parser bug); #4 (scope narrowed to banner+compare; `handleArrow`'s problem is the #1 panic, `handleText` is safe via its own quote extraction); #9 (strengthened — `colorHex` never reaches `FrameEvent.ColorHex`, it is string-smuggled into `TargetImage`); #13 (elevated to Major — triplication is the proven cause of #4).

**Added** (caught by sub-agent): A1 (banner `|` delimiter collision), A2 (path traversal via asset IDs), A3 (O(n²) event rescans), A4 (no negative/zero-duration validation), A5 (no fuzz/CI harness for LLM-authored grammar), A6 (per-tag `strings.Fields` recompute), A7 (comment-only `actionContext` invariant).

**Rejected**: none. The sub-agent's #4 scope correction and #13 elevation were accepted after re-checking against the code; no point from either pass was silently dropped.

**Direction verdicts (Q3/Q4):** sub-agent strongly concurred with the ordering that validation/error-surface work precedes new feature keywords — #1 zeros out first-attempt success (full crash), #3/#4 produce wrong-but-silent renders (worse for an agent loop than an error, since nothing signals a retry). New keywords on the current architecture inherit the triplicated-vocabulary pattern (#13). Recommended sequence: fix #1 + #4 immediately (cheap, bounded), then build schema + linter + parse-error surface (study roadmap #1/#2), *then* evaluate shapes/z-order/color. **Ship #1 first** — it is the only Critical, confirmed-by-reproduction finding, a two-line bounds fix, and the only failure mode with zero salvage value (crashed render burns the full attempt with no partial output and no actionable error).

## 4. Actionable Plan (ordered by severity)

**Phase 0 — blockers before ANY new-API work (correctness):**
1. #1 — bounds-check `parts` in `handleArrow`/`handleCompare`; regression tests for minimal-arg forms of **all** handlers; fuzz tag grammar (#A5).
2. #4 — replace raw `strings.Split` with quote-aware split in `handleBanner`/`handleCompare` (shared tokenizer, #15); tests with embedded-colon quoted args.
3. #2 — canonicalize `+` trigger syntax (parser + doc + example); strip stray `+` from clean text.
4. #3 + #5 — unmatched `[...]` → parse error; `Parse() ([]ScriptLine, error)`.
5. #A1 + #9 — move banner title/subtitle/color into real `FrameEvent` fields; stop `TargetImage` string-smuggling.

**Phase 1 — foundation for API enrichment (design):**
6. #13 — consolidate the triplicated tag vocabulary into one registry (`zenapi` per `ARCHITECTURE_PLAN_docs-generator-regex-parser.md`); parser + compiler consume it.
7. #15 — single quote-aware tokenizer over regex-per-tag.
8. #A4 numeric range validation · #A2 asset-ID/path sanitization · #A5 fuzz + CI gate.
9. #A3 — active-event index / single-pass clear/erase to avoid O(n²).
10. #14 — bound `FrameEvent` growth before adding event types (params map or per-type struct).

**Phase 2 — the feature-research targets (professional-parity gaps):**
11. #7 primitive shapes · #8 z-order · #9 DSL colors/palette (post #A1 fix).
12. #6 text wrap/measure/align engine (prerequisite), then #11 bullets/lists/entrance variants.
13. #10 scene/step/group primitives (post #A3 perf fix).

**Phase 3 — polish/cleanup:**
14. #12 remove dead `+` branch · #16 implement-or-document `[sfx]` · #A6 word-counter · #A7 document/enforce the `actionContext` invariant.

## 5. Open Questions (neither pass resolved above 85%)

1. **Research target definition — feature-depth vs production-quality.** The user's goal ("enriching existing scripting APIs like draw, slide, text...") leans feature-depth; the study's gating rule ("a feature must pay for itself in fewer agent tokens or better first-attempt success") and both review passes strongly favor the validation/error-surface/linter pathway first. The *ordering* is resolved (>85%); the *research's definition of done* (which deliverable, what "parity" concretely means) is a product decision neither pass can make.
2. **Scope of "professional parity" feature list.** Both passes agree the top gaps are shapes, z-order, color, and group/step primitives, but no single source in the repo defines the authoritative target list for "millions-subscriber-channel" caliber. The competitor study's 20+ row matrix needs a scoped, prioritized subset; unresolved whether that list is shapes-centric or includes text-layout + aspect-ratio presets.
3. **`[text]` wrap semantics.** Mechanism is confirmed (contain-fit single line, downscales to illegible), but whether wrap should be automatic (measure + wrap at rect width) or explicit (`\n`) is an unresolved design choice affecting the DSL grammar (#15).
4. **`[sfx]` fate.** Both passes agree it should not remain a silent no-op; implement-vs-document-unsupported is unresolved (mirrors the API doc's existing "unconfirmed" flag).