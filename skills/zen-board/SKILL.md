---
name: zen-board
description: Author and verify whiteboard-explainer videos with the zen-board engine: a single .zen script of narration plus inline [tag:...] marks, rendered to MP4 with narration-synced drawing. Use when asked to make, change, or debug a whiteboard/mascot/meme video in this repo.
---

# zen-board authoring

One script, one video. Narration is plain text; `[...]` marks are compiled into
timeline events that fire at the word they follow. Timing is audio-first: a tag
placed after word N fires when word N finishes.

## Loop (do not skip step 2)

```bash
go build -o zen-board .                                    # once per session
./zen-board check -script s.zen -json; echo "exit=$?"     # 2   never renders
./zen-board -script s.zen -o out.mp4                      # full render, spends TTS
./zen-board snapshot -script s.zen -events end -max 24    # PNGs, no ffmpeg
```

- `check` exits **10** when blocked. Findings are `file:line:col` + code + `Fix:`.
  It issues **zero** TTS requests, so it is free.
- `snapshot` needs a **warm TTS cache** (it refuses a cold one) and writes PNGs +
  `manifest.json` + a contact sheet to `frames/`.
- **Open the PNG before claiming a frame is right.** Never infer layout from the
  MP4 or from the script.

## Tag reference

| Tag | Form | Notes |
|---|---|---|
| draw | `[asset]`, `[asset:preset]`, `[asset:preset:sec]`, `[asset@x,y,w,h]` | reveals with a hand; persists forever after |
| text | `[text:"copy":preset:font:size:weight@x,y]` | needs the quotes or it emits nothing |
| erase | `[erase:asset]`, `[erase:*]` | one layer / whole board |
| clear | `[clear]` | wipes the board |
| zoom | `[zoom:preset]` | camera pans for 1s; reveals are delayed until it settles |
| move | `[move:asset:preset]` | animated reposition |
| gen | `[gen:"prompt":preset]` | needs zen-lights |
| slide | `[slide:asset:preset:transition:fit]` | full-bleed, no reveal |
| banner | `[banner:"title":"sub":sec:#hex]` | lower third, in and out |
| arrow | `[arrow:from:to:style:sec]` | styles straight, curved, double, dashed |
| highlight | `[highlight:region:style:sec]` | region = `x,y,w,h` or a preset |
| compare | `[compare:a:b:"la":"lb"]` | full-screen split |
| overlay | `[overlay:asset:opacity:preset]` | low-level; unremovable today |
| counter | `[counter:from:to:sec:%fmt:preset]` | |
| transition | `[transition:type:sec]` | truncates every earlier event at its midpoint |
| watermark | `[watermark:src:pos]` | |
| style | `[style:whiteboard\|blackboard\|glassboard]` | |
| chapter / subtitle | `[chapter:"t"]`, `[subtitle:top\|bottom\|off]` | metadata only |
| wait | `[wait:sec]` | silence; a line of only `[wait:]` is a pause |
| sfx | `[sfx:name]` | **no-op** — do not rely on it |

Presets: `TL TR BL BR HT HB LH RH center fullscreen`.

## Hard rules

1. Asset ids come from `assets/index.json`: `./zen-board assets list`. Inventing
   an id is the single most common failure.
2. Never nest tags: `[text:"a [draw:x] b"]` loses the inner tag.
3. Legacy durations are **integers** (`:3`, not `:2.5`) unless you use `dur=`.
4. One `[zoom:]` per beat; anything drawn while the camera is elsewhere is
   invisible, and `[zoom:reset]` before a wide shot is not optional.
5. Only the last 60 frames are `[freeze]` — a caption must have its own window.

## Debugging a wrong video

`-eventlog log.jsonl` records every ENTER/EXIT of every event with its frame;
`snapshot -events start,mid,end` renders exactly those frames. Compare the
manifest's `rect`/`cameraFocus` against the script: a rect covering the whole
canvas means the preset name was wrong.
