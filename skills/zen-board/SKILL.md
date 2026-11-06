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
./zen-board check -script s.zen -json; echo "exit=$?"     # never renders, never costs TTS
./zen-board snapshot -script s.zen -events end -max 24    # PNGs, no ffmpeg
./zen-board timeline -script s.zen -json                  # the compiler's decisions
./zen-board -script s.zen -o out.mp4                      # full render, spends TTS
```

- `check` exits **10** when blocked, **3** on an environment problem, **2** on bad
  flags. Findings are `file:line:col` + code + `Fix:`, and it issues **zero** TTS
  requests — so run it first, always.
- `snapshot` needs a **warm TTS cache** (it refuses a cold one, and never fills it
  over the network). It writes PNGs + `manifest.json` + a contact sheet to
  `frames/`, and `-max` truncation is reported as `I0001` rather than silent.
- **Open the PNG before claiming a frame is right.** Never infer layout from the
  MP4 or from the script.
- `timeline` is the cheap feedback signal: event type, identity, `src_line`, box,
  camera focus, layer, exit window — no pixels, no encoder.

## Genre vocabulary

Whiteboard, mascot and meme are the same engine with three idioms. Prefer these
over the low-level primitives they wrap.

| Verb | Form | Behaviour |
|---|---|---|
| character | `[character:robot id=bot anchor=br size=45%]` | overlay layer: stays visible when `[zoom:]` points elsewhere, survives `[clear]`, draws above the board |
| character state | `[character:robot_thinking id=bot]` | same `id=` **closes the previous state** at the new start frame — no ghost |
| sticker | `[sticker:meme_lol anchor=tr w=280 h=200 dur=2 exit=pop]` | pop-in decoration that leaves without `[clear]` |
| caption | `[caption:"NO WAY" anchor=bc size=110 color=#FFFF00 outline=8 dur=2 exit=pop]` | `size=` is glyph pixels and the raster keeps that size; `outline=` keeps it legible |
| clear | `[clear]` / `[clear:all]` | scene wipe that **keeps** characters / wipe that removes everything |
| element ref | `[arrow:@bot:@curve]`, `[highlight:@bot]` | `@id` resolves to the placed element's box at compile time |

Named parameters work on every placement tag: `id= dur= exit= layer= anchor= size=
w= h= fit= color= cursor= mask= hand= keep=`. Give `w=` and `h=` together — one alone cannot describe a box and is ignored with `W0012`.
Named boxes are resolution independent:
`TL TR BL BR HT HB LH RH` and anchors `l c r t b tl tc tr bl bc br third-l third-c
third-r center fullscreen`; `size=` is a percentage of canvas height so one script
stays sane at 1920×1080 and 1280×720.

## Tag reference

| Tag | Form | Notes |
|---|---|---|
| draw | `[asset]`, `[asset:preset]`, `[asset:preset:sec]`, `[asset@x,y,w,h]` | reveals with a hand; persists until `[clear]` unless `dur=` |
| text | `[text:"copy":preset:font:size:weight]` or `[text:"copy" anchor=TR size=64 color=#123456]` | needs the quotes or it emits nothing |
| erase | `[erase:asset]`, `[erase:*]` | one layer / whole board, sticky layers included |
| zoom | `[zoom:preset]` | camera pans for 1s; reveals wait for it to settle |
| move | `[move:asset:preset]` | animated reposition |
| gen | `[gen:"prompt":preset]` | needs zen-lights |
| slide | `[slide:asset:preset:transition:fit]` | full-bleed, no reveal |
| banner | `[banner:"title":"sub":sec:#hex]` | lower third, in and out |
| arrow | `[arrow:from:to:style:sec]` | endpoints: preset box, `x,y`, or `@id` |
| highlight | `[highlight:region:style:sec]` | region: `x,y,w,h`, preset box, or `@id` |
| compare | `[compare:a:b:"la":"lb"]` | full-screen split |
| overlay | `[overlay:asset:opacity:preset]` | low-level: camera-dependent, wiped by `[clear]` |
| counter | `[counter:from:to:sec:%fmt:preset]` | |
| transition | `[transition:type:sec]` | cuts the board at its midpoint; characters stay |
| watermark | `[watermark:src:pos]` | |
| style | `[style:whiteboard\|blackboard\|glassboard]` | |
| chapter / subtitle | `[chapter:"t"]`, `[subtitle:top\|bottom\|off]` | metadata only |
| wait | `[wait:sec]` | silence; a line of only `[wait:]` is a pause |
| sfx | `[sfx:name]` | **no-op** (`E0014`) — do not rely on it |

Full table, generated from the same source the parser uses: `check -dsl`.

## Hard rules

1. Asset ids come from `assets/index.json`: `./zen-board assets list`. Inventing
   an id is the single most common failure, and `check` catches it for free.
2. Never nest tags: `[text:"a [draw:x] b"]` loses the inner tag (`E0003`).
3. Legacy durations are **integers** (`:3`, not `:2.5`); use `dur=2.5` instead.
4. One `[zoom:]` per beat; a board layer drawn while the camera is elsewhere is
   invisible, and `[zoom:reset]` before a wide shot is not optional. Overlay
   layers are exempt — that is what they are for.
5. A `layer=overlay` event is composited after the camera crop: it keeps its position and pixel
   size while `[zoom:]` moves the board. Board layers do not — place them with the camera in mind.
6. `exit=` needs a lifetime: on a layer without `dur=` it is dropped (`W0009`),
   and one scheduled past the last frame is dropped too (`W0010`).
7. A caption larger than its box or the canvas is clipped and reported (`W0011`); lower
   `size=` or widen `anchor=` rather than guessing.
8. `[freeze]` only adds trailing held frames — a caption needs its own `dur=`.

## Debugging a wrong video

`timeline -json` shows what the compiler decided; `-eventlog log.jsonl` records
every ENTER/EXIT with its frame. `snapshot -events start,mid,end` renders exactly
those frames. Compare the manifest's `rect`/`camera_focus` against the script: a
rect covering the whole canvas means the box name was wrong, and an event with no
`src_line` did not come from a tag you wrote.
