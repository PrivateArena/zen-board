# Cursor System Architecture Plan

**Summary**: Replace the single `assets/hand.png` sprite with a data-driven cursor plugin system. Cursors live in `assets/cursors/<name>/` with a `config.json` declaring sprites, tip offsets, and procedural animation keyframes. A `CursorRegistry` lazily discovers and loads presets. The engine renders cursor sprites via a stateless `Interpolate` function — pure function of `(frameNum, fps, preset)` — preserving determinism across parallel frame workers. The existing hand and its 5 sprites (default/pencil/chalk/marker/eraser) become the built-in `"hand"` cursor. New cursors (magic wand with vibration, two-finger reveal with paired sprites) are additive.

---

## 1. System Boundaries & Component Breakdown

```
┌──────────────────────────────────────────────────────────────────────┐
│                          main.go / Run()                             │
│  --cursor flag (global default)                                      │
└────────┬─────────────────────────────────────────────────────────────┘
         │ passes cursor registry to Engine
         ▼
┌──────────────────────────────────────────────────────────────────────┐
│                         internal/render/                             │
│                                                                      │
│  ┌─────────────────┐    ┌────────────────────┐                       │
│  │  CursorRegistry  │    │  CursorPreset       │                      │
│  │  ─────────────   │    │  ────────────       │                     │
│  │  Scan(dir)       │───▶│  Name string         │                     │
│  │  Get(name)       │    │  Sprites map[string] │  (style→png)        │
│  │  Lazy-load       │    │  TipX, TipY (per     │                     │
│  │                  │    │    sprite key)        │                     │
│  └─────────────────┘    │  Animation *AnimationCfg                    │
│                          └────────┬───────────┘                      │
│                                   │                                   │
│  ┌──────────────────────┐         │                                   │
│  │  CursorAnimator      │         │                                   │
│  │  ───────────────     │◀────────┘                                   │
│  │  Interpolate(preset, │                                            │
│  │    frameNum, fps,    │  → []SpriteLayer                           │
│  │    baseX, baseY,     │                                            │
│  │    angle)            │  (supports N sprites per cursor,           │
│  └──────────────────────┘   e.g. 2 for two-finger reveal)            │
│                                                                      │
│  ┌──────────────────────┐                                            │
│  │  Engine.RenderFrame  │  calls CursorAnimator.Interpolate          │
│  │  (per job, no state) │  when handVisible                          │
│  └──────────────────────┘                                            │
│                                                                      │
│  ┌──────────────────────┐                                            │
│  │  easing.go           │  reused by keyframe interpolation          │
│  │  (existing)          │                                            │
│  └──────────────────────┘                                            │
└──────────────────────────────────────────────────────────────────────┘
         │
         ▼
┌──────────────────────────────────────────────────────────────────────┐
│                        internal/model/types.go                       │
│                                                                      │
│  ┌─────────────────────────────┐                                     │
│  │  FrameEvent                 │                                     │
│  │  ──────────                 │  NEW FIELD                          │
│  │  Cursor string              │  ← "hand" | "magic_wand" | ...     │
│  │  HandStyle string           │  ← existing (pencil/chalk/...)      │
│  │  ...                        │                                     │
│  └─────────────────────────────┘                                     │
│                                                                      │
│  ┌─────────────────────────────┐                                     │
│  │  Project                    │                                     │
│  │  ───────                    │  NEW FIELD                          │
│  │  Cursor string              │  ← global default                   │
│  └─────────────────────────────┘                                     │
└──────────────────────────────────────────────────────────────────────┘
         │
         ▼
┌──────────────────────────────────────────────────────────────────────┐
│                        assets/cursors/                               │
│                                                                      │
│  cursors/                                                           │
│  ├── hand/                                                          │
│  │   ├── config.json                                                │
│  │   ├── hand.png          (was assets/hand.png)                     │
│  │   ├── hand_pencil.png                                           │
│  │   ├── hand_chalk.png                                            │
│  │   ├── hand_marker.png                                           │
│  │   └── hand_eraser.png                                           │
│  ├── magic_wand/                                                    │
│  │   ├── config.json                                                │
│  │   ├── magic_wand.png                                            │
│  │   └── sparkle.png            (future use, schema reserved)       │
│  └── two_finger/                                                    │
│      ├── config.json                                                │
│      ├── finger_left.png                                           │
│      └── finger_right.png                                          │
└──────────────────────────────────────────────────────────────────────┘
```

---

## 2. Data Flow & State Management

### 2.1 Startup

```
main.go
  │
  ├── registry := render.NewCursorRegistry()
  │       (no scan yet — lazy)
  │
  ├── engine, err := render.NewEngine(...)
  │       Engine.CursorRegistry = registry
  │       Engine.DefaultCursor = *cursorFlag  (or "hand")
  │
  └── RenderLoop
          └── for each frame: RenderEvent uses registry.Get(cursorName)
                  on first call: lazy-loads from assets/cursors/<name>/
                  subsequent calls: returns cached *CursorPreset
```

### 2.2 Per-Frame Render

```
RenderFrame(frameNum, events, ...):
  for each active event:
    compute baseX, baseY, angle, cursorName, handStyle

  if handVisible:
    preset = e.CursorRegistry.Get(cursorName)
    if preset == nil:
        log error, skip cursor (visible placeholder: red "?" sprite)
        continue

    layers = render.CursorInterpolate(preset, frameNum, e.FPS,
                                      baseX, baseY, angle,
                                      handStyle)
    for _, layer := range layers:
        draw.Draw(buf, ..., layer.Sprite, ..., draw.Over)
```

### 2.3 Determinism Invariant

`CursorInterpolate` is a **pure function**. Return value depends only on:
- `preset` content (loaded once, immutable)
- `frameNum / fps` (wall-clock time, not accumulated delta)
- `baseX, baseY, angle, handStyle` (caller-provided position)

Animation phase is computed as `(frameNum / fps) mod preset.Animation.Duration`. This guarantees byte-identical output regardless of worker thread, rendering order, or partial re-renders.

### 2.4 Multiple Sprites Per Cursor

```go
type SpriteLayer struct {
    Sprite  image.Image
    X, Y    int           // absolute position on canvas
    Opacity float64
}

// Interpolate returns 0..N sprites per cursor.
// hand: 1 sprite. two_finger: 2 sprites. wand: 1 sprite + 0..N optional particle sprites.
func CursorInterpolate(preset *CursorPreset, frameNum, fps int,
    baseX, baseY, angle int, handStyle string) []SpriteLayer
```

---

## 3. CursorPreset Schema

```json
{
  "name": "magic_wand",
  "version": 1,

  "sprites": {
    "default": "magic_wand.png",
    "pencil": "magic_wand.png",
    "chalk": "magic_wand.png",
    "marker": "magic_wand.png",
    "eraser": "eraser.png"
  },

  "tip": {
    "default": { "x": 40, "y": 10 },
    "eraser":  { "x": 28, "y": 28 }
  },

  "sprite_count": 1,

  "animation": {
    "duration_sec": 0.5,
    "loop": true,
    "keyframes": [
      { "t": 0.0, "x": 0, "y": 0, "rotate": 0, "scale": 1.0, "opacity": 1.0, "ease": "ease-in-out" },
      { "t": 0.25,"x": 2, "y": -3,"rotate": 6, "scale": 1.03,"opacity": 1.0, "ease": "ease-out" },
      { "t": 0.5, "x": 0, "y": 0, "rotate": 0, "scale": 1.0, "opacity": 1.0, "ease": "ease-in" },
      { "t": 0.75,"x": -2,"y": -3,"rotate": -6,"scale": 1.03,"opacity": 1.0, "ease": "ease-out" },
      { "t": 1.0, "x": 0, "y": 0, "rotate": 0, "scale": 1.0, "opacity": 1.0, "ease": "ease-in" }
    ]
  }
}
```

### Schema rules:
- `tip` — per-sprite-key. NOT shared across sprites. Missing key falls back to `"default"`.
- `sprites` — all paths are BASENAME only (e.g. `"magic_wand.png"`). The loader joins them onto the preset's directory. Paths containing `..` or `/` are rejected at load time.
- `sprite_count` — integer ≥ 1. For `sprite_count > 1`, sprite names are `finger_left.png`, `finger_right.png` etc. Each sprite has its own animation tracks, or all follow the same keyframes with independent `sprite_index` parameter in the keyframe (future).
- `ease` — one of `"linear"`, `"ease-in"`, `"ease-out"`, `"ease-in-out"`, `"ease-out-cubic"`, `"ease-in-out-cubic"`. Unknown values fall back to `"linear"` with a logged warning.
- Scale is applied at the sprite's center, not top-left.

---

## 4. Failure Modes

| Failure | Detection | Handling |
|---|---|---|
| `cursors/` directory missing | `os.Stat` at startup | Log warning. Fall back to hardcoded single `hand.png` from old path if it exists. |
| `config.json` malformed JSON | `json.Unmarshal` | Return error to caller; engine init fails. Do not silently skip. |
| `config.json` missing required field | Schema validation | Return error. Do not load partially. |
| Referenced sprite PNG missing | `os.Open` at lazy-load time | **Hard error**: log error with path, return error to `Get()`. Engine writes a visible fallback sprite (red "?" placeholder) so the export is visually broken but not silently invisible. |
| Sprite dimension mismatch | Load-time check | If `tip` references a key whose sprite has different dimensions from `"default"`, log warning but allow (tip offset is per-sprite-key). |
| Path traversal in sprite name | String scan | Reject any sprite path containing `..`, `~`, or absolute path prefix. Error at load. |
| Unknown ease function | Key lookup | Fall back to `linear`. Log warning at first use (not every frame). |
| `animation` block missing | Nil check | Cursor renders without animation: static sprite at base position. Existing hand breathing jitter is a special case kept in the `"hand"` preset. |
| `FrameEvent.Cursor` references unknown cursor name | `registry.Get` returns nil | Log warning, skip cursor render (no fallback — silent behavior is better than wrong sprite). |
| `HandStyle` set but `Cursor` not set | FrameEvent handling | `Cursor` defaults to `"hand"`. The hand cursor uses `HandStyle` to select sprite key. This preserves backward compatibility. |

---

## 5. Migration Path

### 5.1 File Move
```
BEFORE:  assets/hand.png
         assets/hand_pencil.png
         assets/hand_chalk.png
         assets/hand_marker.png
         assets/hand_eraser.png

AFTER:   assets/cursors/hand/hand.png
         assets/cursors/hand/hand_pencil.png
         assets/cursors/hand/hand_chalk.png
         assets/cursors/hand/hand_marker.png
         assets/cursors/hand/hand_eraser.png
         assets/cursors/hand/config.json
```

### 5.2 Backward Compatibility
- `NewHandRenderer(path, tipX, tipY)` — kept as a **deprecated shim**. Internally creates a `CursorPreset` from the file at `path` and registers it as `"hand_legacy"`. Emits a deprecation warning to stderr. The plan is to remove in a future release.
- `FrameEvent.HandStyle` — fully preserved. If `FrameEvent.Cursor` is empty or `"hand"`, `HandStyle` maps to the sprite key. If `FrameEvent.Cursor` is a non-hand cursor, `HandStyle` is IGNORED (wand hasn't got styles) — the cursor's own `"default"` sprite is used.
- `main.go --hand` flag — kept for now. If set, creates a legacy hand preset from that path. If not set but `--cursor` is set, uses cursor system. Mutual exclusivity check added: `--hand` and `--cursor` together is an error.

### 5.3 Regression Testing
- Extract current hand rendering output as golden images for each style (default/pencil/chalk/marker/eraser) at 3 rotation angles (0°, ±15°, ±30°).
- After migration, assert pixel-identical output for the same inputs.
- Integration test: `go test ./internal/render/ -run TestHandMigration` compares before/after RGBA buffers.

---

## 6. Concurrency Model

```
CursorRegistry — read-only after lazy-load populates the cache.
                 sync.RWMutex protects the map; read lock for Get(),
                 write lock for first-time lazy load.

CursorInterpolate — PURE FUNCTION. No mutex, no state, no allocation
                   beyond the returned []SpriteLayer slice.
                   Safe to call from any goroutine at any time.

Sprite cache — rotation variants are pre-computed ONCE per preset
               at lazy-load time (same as current rotCache but per-preset,
               not per-style). Memoized in CursorPreset.rotCache.
               Lazy: only presets actually referenced incur cache cost.
```

### Red-team critique folded in:
- **Stateful animator rejected**. No `CursorAnimator` instance per worker. Stateless `Interpolate` function only. This eliminates the concurrency bug entirely.
- **Effects block reserved**, not implemented. Schema omits `effects` array until a follow-up plan designs particle lifetime, z-ordering, and GC.

---

## 7. Key Decisions

| Decision | Chosen | Alternatives Rejected |
|---|---|---|
| Plugin mechanism | Data-driven JSON+PNG presets | Go interface (requires recompilation; user explicitly chose data-driven) |
| Lazy vs eager loading | Lazy on first `Get()` call | Eager directory scan (scales poorly with cursor pack accumulation) |
| Animation model | Procedural keyframes with easing | Sprite sheets (heavier assets, less flexible timing); Math expression DSL (harder to author/validate) |
| Tip offset scope | Per-sprite-key | Per-preset (would misalign eraser vs wand); per-frame computation (unnecessary perf cost) |
| Sprite count | Configurable ≥ 1, built into `Interpolate` return | Single-sprite return (breaks two-finger reveal — Phase 4 wouldn't fit) |
| Config layout | One `config.json` per cursor directory | Single manifest file (merge conflicts, harder to distribute cursor packs) |
| Cursor vs Style | Separate fields | Folded into one (breaks existing scripts and migration) |
| Image packaging | Files on disk | `go:embed` (users can't swap or add cursors without recompile) |
| Animator determinism | Pure function of `(frameNum, fps, preset)` | Stateful per-worker instance (non-deterministic across parallel chunks) |
| Fallback on missing sprite | Hard error at load; visible placeholder at render | Silent no-op (invisible cursor in exported video — unacceptable) |

---

## 8. Red-Team Critique Summary

Critique from Claude (adversarial review):

| Critique | Disposition |
|---|---|
| Plan self-contradicts: Data Flow says "one hand per frame" but Phase 4 requires two. `Animate()` returns single sprite. | **Folded in**. `CursorInterpolate` now returns `[]SpriteLayer`. `sprite_count` in config. All cursors support N sprites from day one. |
| Stateful animator conflicts with parallel workers. | **Folded in**. No state. Pure function only. |
| `effects` block is speculative and undocumented. | **Folded in**. Removed from schema. Reserved for future plan. |
| Rotation caching doesn't scale — eager load of all presets. | **Folded in**. Lazy-load on first `Get()`. Cache per-preset. |
| Tip offset shared across sprites is fragile. | **Folded in**. Per-sprite-key tip offsets. Dimension mismatch logged. |
| Silent fallback on missing sprite is dangerous. | **Folded in**. Hard error at load. Visible "?" placeholder at render. |
| Path traversal in sprite names. | **Folded in**. Path validation: reject `..`, `/`, absolute paths. |
| Migration/back-compat hand-waved. | **Folded in**. Explicit deprecation shim for `NewHandRenderer`, clear HandStyle+Cursor interaction rules, golden-image regression test spec. |
| Do you even need a plugin system? | **Rejected**: user explicitly chose "Plugin system from day one" and "Purely data-driven" in the interview. The plan honors that choice while surfacing costs transparently. |

---

## 9. Open Questions / Low Confidence Items

- [UNCERTAIN: <70%] **Two-finger reveal positioning**: The exact geometry of how two hand sprites track a reveal frontier while sweeping apart needs visual prototyping. The current model (pair of sprites, mirrored offsets from frontier point) is a hypothesis. May need additional config fields per-sprite for `"reveal_offset_x"` / `"reveal_offset_y"` or a separate `"sweep_amplitude"` parameter.
- [UNCERTAIN: <70%] **`sprite_count` > 1 animation model**: Whether all sprites share the same keyframes with positional offsets, or have independent keyframe tracks, is unresolved. Initial impl: same keyframes, offsets defined per-sprite in config (`"position_offset": {"x": 40, "y": 0}`). If independent tracks are needed later, schema can add `"animations": [{...}, {...}]` in a minor version bump.
- [HISTORICAL] The **breathing jitter** on the hand (`3 * sin(2π * frame / 60)`) is a special effect hardcoded in the original `HandRenderer.Draw()`. In the new system, this is modelled as a keyframe animation in the `"hand"` preset rather than special-case code. The keyframe approach should reproduce it exactly; need golden-image comparison to confirm.
- **Effects/particles** (wand sparkles, sand scattering) are deferred to a follow-up plan. The schema explicitly does NOT include them yet. This is intentional but means the magic wand will visually just vibrate — no particle trail — until that plan is implemented.
