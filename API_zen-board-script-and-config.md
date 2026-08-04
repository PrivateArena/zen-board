---
name: zen-board Scripting Language & Config
description: The .zen whiteboard-video DSL and config.json schema for the zen-board Go renderer. Serialization/CLI contract, not a network API.
framework: Go 1.25 binary `zen-board`; FFmpeg + external TTS/paint servers
---

# zen-board Scripting & Config Reference

Declarative `.zen` script → renderer contract; full schema of `config.json`.

> **SERVER_API_URL**: There is no HTTP endpoint. Invoke the single binary with a script path and optional config-path/flag overrides — flags override `config.json`, which overrides compiled defaults.
>
> ```bash
> ./zen-board -script examples/demo.zen            # config.json in ./ or binary dir
> ./zen-board assets <index|list|process-bg|generate|ui>   # asset/catalog subcommands
> ```

Every example below assumes `./zen-board -script <file>.zen`.

---

## config.json (`Project`)

| Field | Type | Default | Description |
|---|---|---|---|
| `script_path` | string | — | `.zen` source; also via `-script` (required at runtime) |
| `assets_dir` | string | `./assets` | Directory of `.png`/`.svg` assets + `index.json` + `cursors/` |
| `output_path` | string | `output.mp4` | Muxed video output |
| `fps` | int | `30` | Frames per second |
| `width` / `height` | int | `1920` / `1080` | Canvas resolution |
| `tts_addr` | string | `http://localhost:5000` | zen-tts synthesis endpoint (HTTP) |
| `speed` | float | `1.0` | TTS speed multiplier |
| `voice` | string | `am_adam` | TTS voice ID |
| `cursor` | string | `hand` | Default cursor preset (`hand`, `magic_wand`, `two_finger`) |
| `hand_tip_x` / `hand_tip_y` | int | `30` / `20` | Hand sprite tip offset (deprecated `-hand`) |
| `disable_transcript` | bool | `false` | Skip ASS subtitle burn-in |
| `bgm_path` | string | — | Background music file |
| `bgm_volume` | float | `0.05` | BGM volume multiplier |
| `camera_enabled` | bool | `false` | Enable pan/zoom camera effects |
| `freeze_frames` | int | `60` | Leading freeze frames appended to duration |
| `tts_cache_dir` | string | `/tmp/zen-board/tts` | TTS audio/timing cache |

```json
{
  "fps": 30, "width": 1080, "height": 720,
  "assets_dir": "./assets", "output_path": "output.mp4",
  "tts_addr": "http://localhost:5055", "voice": "en_US-ryan-medium",
  "camera_enabled": true, "bgm_volume": 0.45,
  "disable_transcript": true, "tts_cache_dir": "/tmp/zen-board/tts"
}
```

**Flag overrides** (all optional): `-script`, `-assets`, `-o`, `-fps`, `-w`, `-h`, `-tts`, `-speed`, `-voice`, `-cursor`, `-bgm`, `-bgm-vol`, `-camera`, `-freeze`, `-tts-cache`, `-disable-transcript`, `-preview` (ffplay), `-fast` (½ res + ultrafast), `-eventlog <path>`, `-hand` (deprecated).

---

## DSL Overview

Plain-text lines; words drive TTS timing, inline `[command:args]` tags insert actions. Tags are stripped from spoken text. Actions trigger at the **start of the preceding word** (or line start if before any word).

| Shared param type | Format | Example |
|---|---|---|
| Asset ref | asset ID (from `index.json`) or `asset.png`/`.svg` | `robot`, `shield_test` |
| Coordinates | `@x,y,w,h` (absolute px) | `@100,200,300,300` |
| Preset layout | one of the layout names below | `TL`, `RH`, `fullscreen` |
| Duration | float seconds | `2.0` |
| Color hex | `#RRGGBB` | `#e74c3c` |
| Quoted value | `"text with spaces"` | `"AI Innovation"` |

**Preset layouts** (apply to any lookup: draw/text/move/slide/arrow/highlight/overlay/counter):

| Preset | Region | Preset | Region |
|---|---|---|---|
| `TL` | top-left quarter | `BL` | bottom-left quarter |
| `TR` | top-right quarter | `BR` | bottom-right quarter |
| `HT` | top half | `HB` | bottom half |
| `LH` | left half | `RH` | right half |
| `fullscreen` — default/fallback | `center` = full canvas | — | — |

**Trigger modifier**: trailing `+` on any tag fires the action **after** the preceding word ends (instead of at its start).

> **Auto-grid placement**: drawing with no preset and no `@coords` falls back to a 3×2 grid indexed left-to-right across the canvas (grid resets on `[clear]`/`[erase:*]`/`[transition:]`).

---

## Content Actions: draw / gen / text / slide / erase / move / clear

### `[draw]` — place an existing asset with hand-drawn reveal
`[draw:<asset>[:<preset>][:<variant>...][@x,y,w,h][:<revealDur>]]`
`<variant>= key=value` (recolor SVG fill) or bare token (appended to asset tag). `cursor` variant sets cursor per-draw.
Default reveal duration `2.0s`; default cursor `hand`; mask `diagonal`.

```zen
[draw:world:LH]
[draw:robot:cursor=magic_wand:TR]
[draw:shield_test:shield-body=#e74c3c:star-shape=#f1c40c@600,500,400,400]
```

### `[gen]` — AI-generate paint asset from prompt (zen-lights)
`[gen:<prompt>[:<preset>][@x,y,w,h]]` — auto-asset `__gen_%d`; default preset/zoom focus.
Generates via `POST http://localhost:8765/paint/generate` `{"prompt","width","height","steps"}` → `{"path"}`.

```zen
[gen:a neon city skyline:fullscreen]
```

### `[text]` — render text overlay
`[text:"<content>"[:<preset>:<fontFamily>:<fontSize>:<weight>][@x,y,w,h][:revealDur]]`
Defaults: preset = current zoom focus, `sans` / `48` / `regular`; `bold` → IsBold. Text color auto-inverts on `blackboard`/`glassboard`. Persists to end of video.

```zen
[text:"AI Innovation":RH:sans:48:bold]
```

### `[slide]` — drop an image with a transition (no hand-draw)
`[slide:<asset>[:<preset>][:<transition>][:<fitMode>][@x,y,w,h][:revealDur]]`
Defaults: `none` / `fit`. Persists to end.

```zen
[slide:pyramids:center:pop]
[slide:city:TR:fade:fill]
```

### `[erase]` — animate out a placed asset
`[erase:<asset>]` (or `[erase:<asset>@x,y,w,h]`). Binds to latest event for that asset; eraser hand, `ttb` mask. Warns if never placed.

```zen
[erase:world]
```

### `[erase:*]` / `[clear]` — wipe the board
Both truncate all active events at the action frame and reset the auto-grid.

```zen
[erase:*]
[clear]
```

### `[move]` — reposition an asset
`[move:<asset>[:<preset>|@x,y,w,h]]` — target from latest event; animates to preset/absolute dest. Hand style `pencil`.

```zen
[move:robot:BL]
```

---

## Annotation & Effect Actions: arrow / highlight / compare / overlay / counter / transition / banner

### `[arrow]`
`[arrow:<from>:<to>[:<style>][:<duration>]]` — `<from>`/`<to>` are `x,y` points or presets (center). Styles: `straight`(d), `curved`, `double`. Default `1.0s`; persists.

```zen
[arrow:TL:BR:curved:0.5]
```

### `[highlight]`
`[highlight:<region>[:<style>][:<duration>]]` — region `x,y,w,h` or preset. Styles: `rect`(d, marching ants), `circle`, `pulse`. Default `2.0s`.

```zen
[highlight:100,200,400,150:pulse:2.0]
```

### `[compare]`
`[compare:<left>:<right>[:<"lblLeft">:<"lblRight">]]` — side-by-side split. Labels optional. Persists.

```zen
[compare:robot:world:"Code World":"Real World"]
```

### `[overlay]`
`[overlay:<asset>[:<opacity>][:<preset>]]` — semi-transparent, persists **until** explicitly erased/cleared. Defaults `0.5` / `fullscreen`.

```zen
[overlay:world:0.3:fullscreen]
```

### `[counter]`
`[counter:<start>:<end>[:<dur>[:<format>[:<preset>]]]]` — animates start→end numeric. Defaults `2.0s` / `%d` / `center`. Holds at `<end>` after.

```zen
[counter:0:100:2.0:%.1f:center]
```

### `[transition]`
`[transition:<type>[:<dur>]]` — fades canvas to black/white and back; truncates active events at midpoint and resets grid. Default `0.5s`. Types: `fade-black`(d), `fade-white`, `flash`.

```zen
[transition:fade-black:0.5]
```

### `[banner]` — lower-third / title card
`[banner:"<title>":["<subtitle>"]:[<dur>]:[<colorHex>]]` — default `4.0s`, auto color. Slides in/out.

```zen
[banner:"Lower Third Demo":"Animated name card":4.0:FF4444]
```

---

## Scene-Control Actions: chapter / style / zoom / subtitle / wait / sfx

### `[chapter:"<title>"]`
Writes an FFmpeg chapter marker at the trigger time.

```zen
[chapter:"Intro"]
```

### `[style:<name>]`
Sets active board style for subsequent text/banners. `whiteboard`(default, white bg/inverted text), `blackboard`(dark #0f0f0f), `glassboard`(#181c25). Persists until next `[style:]`.

```zen
[style:blackboard]
```

### `[zoom:<focus>]`
`[zoom:reset]` or any preset (`TL/TR/BL/BR/HT/HB/LH/RH`). Sets the active camera focus and centers camera over that region when enabled (`camera_enabled`). Subsequent untagged draws move reveal to the settled camera (1s transition window).

```zen
[zoom:BR]
[zoom:reset]
```

### `[subtitle:<mode>]`
`on` / `off` / `top` — controls ASS transcript placement.

```zen
[subtitle:top]
[subtitle:off]
```

### `[wait:<sec>]`
Hold gap (no narration). Parser emits `WAIT:<sec>`; timeline builder skips it, treating it as audio silence period.

```zen
[wait:1.5]
```

### `[sfx:<name>]` *(unconfirmed)*
Parsed and emitted as an action but **no-op** in the timeline builder (`continue`); accepted for forward-compat.

```zen
[sfx:whoosh]
```

---

## Enums / Valid Values

| Field | Values |
|---|---|
| Draw mask | `diagonal` (draw/gen), `ltr` (text), `ttb` (erase) |
| Hand styles | `pencil`, `marker`, `eraser` |
| Section color | arrow default `#EB4034` (red), highlight default `#FFA500` (orange) — none are user-settable via DSL (ColorHex unexported to DSL) |
| Cursor presets | `hand`, `magic_wand`, `two_finger` (from `assets/cursors/`) |
| Text font | `sans` (embedded; `:bold` weight) |
| Recolor variants | any `element=#hex` pair resolved against SVG `id`/`class` via `ModifySVG` |

---

## Timing & Priority Quick-Reference

Actions are keyed to word boundaries from TTS; ordering is authoritative within a line.

| Modifier | Effect |
|---|---|
| (none) | Action fires at start of preceding word |
| trailing `+` | Action fires at end of preceding word |

| Concept | Value |
|---|---|
| Draw/gen/text reveal delay | Pushed past any active 1s zoom-transition window (`adjustForZoom`) |
| `[clear]` / `[erase:*]` / `[transition:]` | Truncate all prior events + reset auto-grid |
| `[transition:]` | Truncation at midpoint, not start |
| Overlay | Persistent until erased — never auto-clear |
| Asset resolution | `index.json` entry → else `<assets_dir>/<id>.png`; missing → compile error |
| SVG handling | Rasterized via oksvg max 4096px, XML color variants applied first |

---

## Open Questions / Unconfirmed

- `[sfx:]` syntax and behavior unconfirmed (accepted, skipped in builder).
- `[banner:...]` trailing `+` trigger modifier — paths for it reference a trimmed tag (dead branch); treat as unsupported.
- `[slide:]` `fitMode` fills supported for `fit`(d)/`fill`/`stretch`; `cover` not present in source.
- Color injection into `[arrow]`/`[highlight]` (ColorHex) is implemented in the renderer but not exposed through the DSL parser.