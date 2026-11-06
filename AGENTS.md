# AGENTS.md — zen-board

zen-board renders a narrated whiteboard video from one plain-text `.zen` script:
it parses inline tags, synthesises TTS with per-word timing, compiles an event
timeline, then rasterises frames deterministically and pipes them to ffmpeg.
Single Go binary. No browser, no DOM.

## Run it

```bash
go build -o zen-board .
./zen-board -script examples/demo.zen -o out.mp4      # full render (needs TTS + ffmpeg)
./zen-board assets list                               # asset catalog (id -> file)
./zen-board assets ui                                 # web asset manager
go test ./...                                         # unit + integration (~10s)
```

Flags that matter: `-script` (required), `-assets` (default `./assets`),
`-tts` (zen-lights address, default from `config.json`), `-tts-cache` (default
`/tmp/zen-board/tts`), `-w/-h/-fps`, `-freeze` (trailing held frames),
`-camera`, `-eventlog <file>` (JSONL of every event ENTER/EXIT).

## The loop

1. Edit the `.zen` script.
2. Validate it **before** spending TTS: `zen-board check -script s.zen -json`
   (exit 10 = blocked, each finding carries `file:line:col` and a `Fix:`).
3. Render: `zen-board -script s.zen -o out.mp4`.
4. Look at the screen without an encode:
   `zen-board snapshot -script s.zen -events end -max 24` writes ≤24 PNGs plus a
   contact sheet and `manifest.json` into `frames/`.
5. Open the PNG you produced. Edit. Repeat.

## DSL

`[tag:arg:arg@x,y,w,h]` inside a narration line; the tag fires at the word that
precedes it. Bare `[asset]` / `[asset:preset:sec]` draws. Tags: `draw text erase
move gen wait clear zoom style chapter subtitle sfx slide banner arrow highlight
compare transition overlay counter watermark`. Presets: `TL TR BL BR HT HB LH RH
center fullscreen`. `cursor=` and SVG `key=value` variants ride in the same tail.
Full table: `internal/script/dsl.go` (`zen-board check --dsl`).

## Traps (all measured, not folklore)

- **Typos are silent.** `[drw:robot]` is not consumed by any pattern: it is read
  aloud by TTS and nothing is drawn. Nothing checks a prefix that failed to
  match — validate with `check`.
- **Nested tags panic-free but lossy.** `[text:"a [draw:x] b"]` consumes the outer
  span only; the inner tag is gone and you get `E0003`. Never nest tags.
- **Durations must be integers in the legacy tail form.** `[draw:robot:2.5]`
  parses as a preset named `2.5`: 0 events, read aloud.
- **Unknown preset = full canvas**, and the same unknown name becomes a camera
  state, so an unrelated `[zoom:]` can go wrong later.
- **`cursor=magic_wand` needs the preset installed** (`assets/cursors/<name>/`),
  otherwise nothing is drawn; the renderer now falls back to `hand`.
- **`[overlay:x]` cannot be removed.** Every persistent layer is `FRAME_FOREVER`
  and `[clear]` wipes the whole board, including the mascot.
- **A `[clear]`/`[transition:]` mid-video rewrites the `EndFrame` of every earlier
  event** — that is why an overlay placed before it disappears.
- **`[sfx:]` is a documented no-op.** It compiles to nothing.
- **Wrong asset id** is a hard error only after the whole TTS stage has run
  (~16s cold). Validate first.
- `-fast`/`-preview` halve the canvas **before** compile, so every absolute
  `@x,y` lands somewhere else. Never trust a `-fast` frame for layout.
- Frame output is a pure function of `(frame, events, camera, style)`; a PNG is
  the only artifact that tells you what the screen looks like. An MP4 tells a
  text agent nothing.

## Authoring rules

- Assets: reference `assets/index.json` ids only (`assets list`). `test` is not
  an asset.
- Put `[zoom:reset]` before a wide shot; every event placed while the camera is
  zoomed elsewhere is invisible.
- Keep one line = one sentence = one beat; timing is audio-first.
