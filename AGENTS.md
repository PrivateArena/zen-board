# AGENTS.md — zen-board

zen-board renders a narrated whiteboard video from one plain-text `.zen` script:
it parses inline tags, synthesises TTS with per-word timing, compiles an event
timeline, then rasterises frames deterministically and pipes them to ffmpeg.
Single Go binary. No browser, no DOM.

## The loop

```bash
go build -o zen-board .
./zen-board check     -script s.zen -json      # validate; no TTS, no network, no ffmpeg
./zen-board snapshot  -script s.zen -events end -max 24   # PNGs + sheet, needs warm TTS cache
./zen-board timeline  -script s.zen -json      # what the compiler decided, as data
./zen-board -script s.zen -o out.mp4           # render (bare flags = `render`)
./zen-board assets list                        # asset catalog (id -> file)
```

1. Edit, then `check`. Exit 10 means blocked; every finding carries
   `file:line:col`, a code, and a `Fix:`. Exit 3 = environment (missing ffmpeg,
   cold TTS cache). Exit 2 = bad flags. `check` spends zero TTS requests.
2. `snapshot` writes PNGs of the frames you asked for, in `frames/`, plus
   `manifest.json` and a contact sheet (JPEG, ≤1568px, one tile for a vision
   model). It never spawns ffmpeg and never issues a TTS request: frame numbering
   comes from real narration timings, so warm the cache with one render first.
3. **Open the PNG.** Edit. Repeat. An MP4 tells a text agent nothing.

Flags that matter: `-script`, `-assets` (`./assets`), `-tts`, `-tts-cache`
(`/tmp/zen-board/tts`), `-w/-h/-fps`, `-freeze` (trailing held frames),
`-eventlog <file>`. For `snapshot`: `-at 3s,120`, `-every 0.5s`,
`-events start|mid|end|all`, `-frame N`, `-max` (default 24, truncation is
reported, never silent), `-max-w`, `-sheet jpg|png|none`.

## DSL

`[tag:arg:arg key=value @x,y,w,h]` inside a narration line; a tag fires at the
word that precedes it. Bare `[asset]` / `[asset:preset:sec]` draws. The table in
`internal/script/dsl.go` is the grammar: the parser, the linter, the compiler and
`zen-board check -dsl` all read it, so run `check -dsl` instead of guessing.

Named parameters work on every placement tag: `id= dur= exit= layer= anchor=
size= w= h= fit= color= cursor= mask= hand=` — and `w=`/`h=` must come as a pair. `cursor=` and SVG `key=value`
variants ride in the same tail.

| Genre verb | What it is for |
|---|---|
| `[character:robot id=bot anchor=br size=45%]` | persistent presenter: stays visible when `[zoom:]` points elsewhere, survives `[clear]`; re-using `id=` **replaces** the previous state instead of stacking a ghost |
| `[sticker:meme_lol anchor=tr w=280 h=200 dur=2 exit=pop]` | pop-in decoration that leaves on its own |
| `[caption:"NO WAY" anchor=HB size=110 color=#FFFF00 outline=8 dur=2 exit=pop]` | impact text; `size=` is glyph pixels and the raster keeps that size |
| `[clear]` / `[clear:all]` | board wipe that keeps characters / wipe that does not |
| `[arrow:@bot:@curve]`, `[highlight:@bot]` | `@id` addresses a placed element's box, resolved at compile time |

`[overlay:]` stays as the low-level primitive (camera-dependent, removed by
`[clear]`); prefer the three verbs above. A `layer=overlay` event is composited
**after** the camera crop, so it keeps its position and pixel size while `[zoom:]`
moves the board underneath — that is what makes a mascot a presenter and not scenery.

## Traps (all measured, not folklore)

- **Typos are silent.** `[drw:robot]` matches no pattern: TTS reads it aloud and
  nothing is drawn. Nothing checks a failed prefix at render time — `check` does.
- **Nested tags are lossy.** `[text:"a [draw:x] b"]` consumes the outer span only
  and reports `E0003`. Never nest tags.
- **`[sfx:]` is a documented no-op** (`E0014`).
- **`cursor=` needs the preset installed** under `assets/cursors/<name>/`,
  otherwise the renderer falls back to `hand`.
- **A `[clear]`/`[transition:]` rewrites the `EndFrame` of every earlier event.**
  That is why a plain `[overlay:]` placed before one disappears; a `[character:]`
  does not.
- **`-fast`/`-preview` halve the canvas before compile**, so every absolute `@x,y`
  lands elsewhere. `snapshot` refuses `-fast` for that reason.
- `exit=` on a layer with no `dur=` is dropped (`W0009`); an exit scheduled past the
  last frame is dropped (`W0010`); a caption bigger than its box or the canvas is
  reported (`W0011`); `w=` without `h=` is ignored (`W0012`).

## Authoring rules

- Reference `assets/index.json` ids only (`assets list`). `test` is not an asset.
- Put `[zoom:reset]` before a wide shot; a board layer placed while the camera
  looks elsewhere is invisible. Overlay layers are exempt by design.
- Keep one line = one sentence = one beat; timing is audio-first.
- Frames are a pure function of `(frame, events, camera, style)`; the same script
  renders to identical PNG bytes, so diff a snapshot instead of eyeballing a video.
