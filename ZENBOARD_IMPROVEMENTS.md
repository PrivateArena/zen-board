# zen-board: Improvement Proposal
## Reaching SOTA Whiteboard Video Generation

---

## Executive Summary

zen-board has a solid, clean architecture: parallel TTS, diagonal reveal mask, camera presets, karaoke subtitles, direct FFmpeg pipe. The pipeline is fast and the scripting format is genuinely AI-agent friendly.

The weaknesses fall into three buckets:

1. **Scripting language gaps** — the annotation DSL is incomplete compared to what top SOTA tools expose
2. **Render quality** — the mask, hand, and drawing simulation are functional but flat
3. **Pipeline limitations** — no text-on-canvas, no multi-style boards, no transitions, no asset generation

Below is a prioritised breakdown with concrete proposals for each.

---

## 1. Scripting Language: Current Gaps vs. SOTA

SOTA whiteboard tools (Doodly, VideoScribe, Vyond, Animaker) all offer things the current `.zen` DSL does not:

### 1.1 Missing: `[text:...]` — On-Canvas Text Rendering

The biggest gap. SOTA tools let you write text directly on the whiteboard surface, with a drawing-hand animation that writes it letter by letter. Right now, zen-board can only reveal pre-baked PNG images — the narrator text exists only as subtitle overlay.

**Proposed syntax:**

```
[text:"The Age of AI":HB:serif:64:bold]
```

Parameters: `content`, `preset/position`, `font`, `size`, `weight/style`

**Implementation:** Render text to an `image.RGBA` buffer at script-parse time using `golang.org/x/image/font` + a TTF loader (e.g. `github.com/golang/freetype`). The resulting texture is injected as a synthetic `FrameEvent`, revealed with the existing mask. The hand traces left-to-right per character instead of diagonal — matching the letter-writing animation that defines the whiteboard aesthetic.

### 1.2 Missing: `[erase:asset_name]` — Targeted Erasure

Currently `[clear]` wipes the entire canvas. SOTA tools let you erase individual elements while keeping others. The eraser hand animation (back-and-forth swipe) is also a key part of the aesthetic.

**Proposed syntax:**

```
[erase:pyramids]        → erase a specific named element
[erase:*]              → equivalent to current [clear]
```

**Implementation:** Reverse-animate the alpha mask from 1.0→0.0 over ~1 second for the targeted `FrameEvent`. The hand renderer plays an eraser sprite instead of pencil sprite during this window.

### 1.3 Missing: `[move:asset:preset]` — Repositioning Existing Drawings

SOTA tools allow drawings to slide/animate to a new position after being placed. Currently assets are static once drawn.

**Proposed syntax:**

```
[draw:robot:HB]
... later ...
[move:robot:TR]      → smoothly translate robot from HB to TR over 1 second
```

**Implementation:** Add `MoveEvent` to the timeline with source/dest rects. Linear interpolation (or ease-in-out) between positions during the move window.

### 1.4 Weak: Inline `[wait]` Semantics

Currently `[wait:1.0]` must occupy its own line. It works, but it breaks AI generation flow — the agent must interrupt a sentence to insert a pause.

**Proposed enhancement:** Allow `[wait]` inline anywhere, triggering at the word boundary where it appears:

```
The history of the world is a story [wait:0.5] of constant change.
```

**This already partially works** (the parser handles inline tags), but the timeline builder silently skips WAIT actions that appear on lines with text — fix `main.go` to honour inline waits by inserting a short silent audio chunk after the word they follow.

### 1.5 Missing: `[style:...]` — Board Style Switching

SOTA tools support blackboard, glassboard, whiteboard, and sketchbook modes within the same video. zen-board is hardcoded to white.

**Proposed syntax:**

```
[style:blackboard]    → dark bg, chalk-style hand, light-on-dark assets
[style:whiteboard]    → current default
[style:glass]         → transparent blue-tinted bg, marker-style
```

**Implementation:** A `BoardStyle` struct controls: background colour, hand sprite path, mask feather style (chalk = high feather/noise, marker = sharp), subtitle colour. `[style:X]` at any point inserts a keyframe into the style timeline. The renderer interpolates (or cuts) between styles at the designated frame.

### 1.6 Missing: `[sfx:...]` — Sound Effects

This was explicitly discarded, but it's worth revisiting. The SOTA tools all support click/squeak/chalk sounds timed to drawing events. For AI-generated content, keeping it optional is correct — but the hook should exist.

**Proposed syntax (opt-in):**

```
[draw:robot:HB][sfx:marker]    → plays a short marker-squeak SFX while robot draws
```

No change to the default output. SFX files ship as optional assets.

### 1.7 Weak: Draw Trigger Semantics — Before vs. After Word

Currently `[draw:asset]` placed after a word triggers at the START of that word. The intent is usually "draw this as I say the next word." The off-by-one is subtle but causes visual drift.

**Proposed fix:** Add a `+` suffix modifier:

```
[draw:robot:HB]+    → trigger AFTER the preceding word ends (= start of next word)
[draw:robot:HB]     → trigger AT the start of the preceding word (current behaviour)
```

Or, more naturally, the position relative to a word should be:

- Tag BEFORE a word → triggers at word start
- Tag AFTER a word → triggers at word end

The parser already tracks `WordIndex` — the fix is to use `allWordTimings[idx].End` instead of `.Start` when the tag follows (not precedes) a word.

### 1.8 Missing: `[chapter:"Title"]` — Named Chapters

Long-form educational videos need chapter markers for navigation in YouTube/VLC. The ASS subtitle track can embed chapter markers; FFmpeg can write them into MP4 metadata.

**Proposed syntax:**

```
[chapter:"The Industrial Revolution"]
```

Zero visual change. Just drops a chapter marker into the output MP4.

---

## 2. Render Quality

### 2.1 Mask Direction — Diagonal is Wrong for Drawing Simulation

The current diagonal sine-wave mask reveals assets from top-left to bottom-right. Real marker/chalk drawing follows a **left-to-right, top-to-bottom stroke pattern** — horizontal sweeps, not diagonal fans. SOTA tools simulate this with horizontal band reveals with slight oscillation.

**Proposed fix in `mask.go`:**

Replace the diagonal scan (`posY = fY / (2*fH)`, `normalizedPos += stepX`) with a **horizontal band sweep**:

```go
// Progress maps to a horizontal band sweeping top-to-bottom
// with a sine-wave right-edge for the "stroke end" feel
bandY := progress * float64(height)
sineOffset := config.Amplitude * float64(height) * math.Sin(2*math.Pi*float64(x)/config.Wavelength)
threshold := bandY + sineOffset
if float64(y) < threshold - featherPx {
    alpha = 255
} else if float64(y) < threshold {
    alpha = feather blend
} else {
    alpha = 0
}
```

This alone makes the reveal look dramatically more realistic. For text elements, override to left-to-right horizontal sweep per character block.

### 2.2 Hand Sprite — Needs Multiple Styles

The current single `hand.png` is drawn at every frame of every reveal. SOTA tools use:

- A **pencil hand** for initial drawing
- A **marker/chalk hand** for different board styles
- An **eraser hand** for the erase animation
- A **pointer** hand for highlights

**Proposed:** `HandRenderer` loads multiple sprites keyed by type. The `FrameEvent` carries a `HandStyle` field (default: pencil). The `[style:blackboard]` command switches the default to chalk.

### 2.3 Scaling — Bilinear, Not Nearest-Neighbour

`scaleImage()` in `engine.go` uses integer nearest-neighbour sampling:

```go
srcX := bounds.Min.X + (x * srcW / w)
```

This produces aliased, pixelated results when downscaling large PNGs into grid cells. Replace with bilinear interpolation. A drop-in option is `golang.org/x/image/draw`:

```go
import xdraw "golang.org/x/image/draw"
xdraw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
```

This is a 3-line change with a visible quality jump. Use `CatmullRom` for highest quality (slightly slower).

### 2.4 Hand Breathing Jitter — Currently Static

The hand jitter (`breathing jitter` mentioned in the architecture doc) should add subtle per-frame noise to the tip coordinates — simulating the micro-tremor of a real hand. If it's already implemented in `hand.go`, verify the amplitude is perceptible (2–4 px range). If not, add:

```go
jitterX := int(2 * math.Sin(float64(frameNum)*0.37))
jitterY := int(2 * math.Cos(float64(frameNum)*0.29))
```

### 2.5 Missing: Asset Fade-In After Reveal

Once an asset is fully revealed (`progress >= 1.0`), it snaps to fully opaque. A gentle fade-in over the last 10% of the reveal (progress 0.9→1.0) would smooth the transition. Currently the `progress >= 1.0` branch skips the mask entirely for performance — this is correct, but the fade should be applied in the `0.9–1.0` window before that optimisation kicks in.

---

## 3. Pipeline & Architecture

### 3.1 Missing: AI Asset Generation Hook

The biggest SOTA gap. Tools like Vyond and Animaker have asset libraries. zen-board's philosophy is "bring your own PNGs" — which is AI-agent friendly if the agent can generate the PNGs. The missing piece is a first-class generation hook.

**Proposed: `[gen:prompt text]` annotation**

```
[gen:a simple line drawing of a robot on a whiteboard background:HB]
```

At script-parse time, if an asset named `robot` doesn't exist in `./assets/`, zen-board calls a configurable image generation endpoint (local Stable Diffusion / DALL-E / Flux) to generate it and saves it to `./assets/robot.png`. The rest of the pipeline is unchanged.

Config:
```json
{
  "image_gen_url": "http://localhost:7860/sdapi/v1/txt2img",
  "image_gen_style_prompt": "whiteboard sketch, black line art, white background"
}
```

This single feature closes the largest gap between zen-board and commercial SOTA tools.

### 3.2 Missing: Per-Asset Reveal Duration

All assets use a hardcoded `revealDuration := 2.0` seconds in `main.go`. More complex drawings should reveal slower; icons faster.

**Proposed syntax:**

```
[draw:complex_diagram:TL:4.0]    → 4 second reveal
[draw:icon:TR:0.8]               → fast 0.8 second reveal
```

Parser change: add an optional 3rd parameter after the preset to `drawRegex`. Default remains `2.0`.

### 3.3 `[clear]` Resets `gridIndex` but Not Asset Z-Order

After `[clear]`, `gridIndex` resets to 0 which is correct. However, if the same asset name is re-used post-clear, `ScaledAssets` cache will serve the old scaled image without checking whether dimensions have changed. This is a subtle cache key collision bug — the cache key already includes dimensions (`name_w_h`), so it's only a bug if the same asset is placed at a different size post-clear. Low priority, but worth noting.

### 3.4 Missing: Subtitle Positioning Modes

The ASS subtitle is fixed to bottom-centre. SOTA tools can position captions:
- Bottom (current)
- Top (for content-heavy lower halves)
- Hidden per-section (the current `disable_transcript` only offers all-or-nothing)

**Proposed:** `[subtitle:top]` / `[subtitle:bottom]` / `[subtitle:off]` annotations that change the `MarginV` and `Alignment` in the ASS style block at the matching timestamp.

### 3.5 Missing: Outro / Freeze Frame

Videos often end abruptly when audio ends. SOTA tools hold the final frame for 1–2 seconds. Add a `freeze_frames` config option (default: `60` = 2 seconds at 30fps) that appends silent padding after `exactDuration`.

---

## 4. Scripting Format: Recommended New Grammar

Putting it all together, the proposed enhanced `.zen` DSL:

```
# Comments with #

# Style block (optional, defaults to whiteboard)
[style:blackboard]

# Chapter markers
[chapter:"The Beginning"]

# Basic draw with auto-layout
The history of the world [draw:world] is a story of constant change.

# Draw with preset + custom reveal duration
[draw:pyramids:TL:3.5] From ancient civilizations to the modern era.

# On-canvas text rendering (hand writes it)
[text:"Key Insight":HB:sans:48:bold]

# Inline wait at a word boundary
The future [wait:0.5] is here.

# Move an existing drawing to a new position
[move:pyramids:BR]

# Erase a specific asset
[erase:world]

# AI-generate a missing asset on-the-fly
[gen:minimalist line drawing of a neural network:LH]

# Zoom + draw in one beat (unchanged)
[zoom:TL][draw:city:TL] Now we see the modern city.
```

---

## 5. Prioritised Roadmap

| Priority | Change | Effort | Impact |
|---|---|---|---|
| 🔴 P0 | Bilinear scaling (`xdraw.BiLinear`) | 30 min | High visual quality lift |
| 🔴 P0 | Horizontal band reveal mask | 2h | Core aesthetic improvement |
| 🔴 P0 | Per-asset reveal duration parameter | 1h | Scripting expressiveness |
| 🟠 P1 | `[text:...]` on-canvas text rendering | 1 day | Closes biggest SOTA gap |
| 🟠 P1 | Inline `[wait]` on text lines | 2h | Scripting correctness |
| 🟠 P1 | `[erase:asset]` targeted erasure | 3h | SOTA parity |
| 🟠 P1 | Freeze-frame outro | 30 min | Polish |
| 🟡 P2 | `[gen:prompt]` AI asset generation | 1 day | Closes commercial SOTA gap |
| 🟡 P2 | `[style:blackboard\|glass]` board modes | 4h | Visual variety |
| 🟡 P2 | `[move:asset:preset]` repositioning | 3h | Dynamic storytelling |
| 🟡 P2 | Subtitle position annotations | 2h | Layout control |
| 🟢 P3 | `[chapter:"Title"]` markers | 1h | Long-form UX |
| 🟢 P3 | Multiple hand sprite styles | 3h | Realism |
| 🟢 P3 | Asset fade-in smoothing | 1h | Polish |
| 🟢 P3 | `[sfx:marker]` optional sounds | 4h | Optional realism |

---

## 6. One Concrete Code Change to Do Right Now

`scaleImage()` in `internal/render/engine.go` — replace with this:

```go
import xdraw "golang.org/x/image/draw"

func scaleImage(src image.Image, w, h int) image.Image {
    if w <= 0 || h <= 0 { return src }
    if src.Bounds().Dx() == w && src.Bounds().Dy() == h { return src }
    dst := image.NewRGBA(image.Rect(0, 0, w, h))
    xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
    return dst
}
```

Add to `go.mod`: `golang.org/x/image` (likely already a transitive dep). This is the highest-quality-per-line-of-code change available.

---

*zen-board's architecture is cleaner than most commercial whiteboard tools. These improvements are evolutionary, not structural.*