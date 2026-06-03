# Code Review — commit 0530946
## zen-board Advanced DSL & Rendering Enhancements

Overall: **solid implementation**. Everything proposed was shipped, the pipeline stays clean,
and the integration test covers the happy path. The notes below are in priority order.

---

## 🔴 Bugs / Correctness Issues

### 1. `TriggerAfterWord` is `true` for ALL non-wait actions — breaking draw timing

In `parser.go`, both the wait and non-wait branches unconditionally set:
```go
TriggerAfterWord: wordCount > 0,
```

This means `[draw:robot:HB]` placed after a word now triggers at that word's **end**, not its
start. The proposal intended `TriggerAfterWord` as an opt-in (`+` modifier), not as the
default. As shipped, every draw that follows any word is delayed by one extra word duration —
which will cause noticeable visual lag on existing scripts.

**Fix:** Only set `TriggerAfterWord: true` for WAIT actions, or introduce the explicit `+`
suffix modifier. Leave draw/text/erase/move as `TriggerAfterWord: false` by default.

---

### 2. `[gen:]` fallback is hardcoded to `robot.png`

```go
fallbackPath := filepath.Join(conf.AssetsDir, "robot.png")
```

If the paint server is down and the project has no `robot.png`, the gen asset silently
registers nothing, but the `FrameEvent` still references `__gen_N`. The asset lookup in
`RenderFrame` will find `ok == false` and skip the event silently — no draw happens, no
error is surfaced.

**Fix:** Log a clear warning that the gen event will be skipped entirely, or substitute a
1×1 transparent placeholder. The fallback should not assume a specific asset name.

---

### 3. `[move]` static tail event uses `EndFrame: 999999`

```go
staticDrawEvent := model.FrameEvent{
    EndFrame: 999999,
    ...
}
```

This sentinel will produce wrong results the moment `totalFrames > 999999` (a ~9-minute video
at 30fps). More importantly, `999999` bleeds into the `resultsMap` key space — harmless
today but fragile.

**Fix:** Set `EndFrame: totalFrames - 1` after the total frame count is known, or compute it
lazily. Alternatively, clamp all `EndFrame` values to `totalFrames - 1` in a post-processing
pass before rendering starts.

---

### 4. `pipe.go` — BGM filter_complex uses hardcoded stream index `[2:a]`

```go
"-filter_complex", fmt.Sprintf("[2:a]volume=%.4f...", bgmVolume),
```

The input ordering is: `0` = raw video, `1` = audio WAV, `2` = BGM (when present), `3` =
metadata. With metadata present and BGM present, `[2:a]` is still correct. But the `else`
branch (no BGM) maps `-map 0:v -map 1:a` — this is fine. However the **preview pipe** has a
subtly different ordering (no `assPath` input), and the metadata index computation there
counts `audioPath` separately (`metaIdx++` if `audioPath != ""`). This is correct but
fragile and diverges from the non-preview pipe logic.

**Recommendation:** Extract a helper `buildInputArgs(video, audio, bgm, metadata) (args []string, indices map[string]int)` shared by both pipes to keep stream indices in one place.

---

## 🟠 Logic / Design Issues

### 5. `[erase:asset]` copies position from the FIRST `"draw"` event found — but `[move]` leaves `EventType: "draw"` on the static tail

After `[move:test:BR]`, there are two events for `test`: the original draw (type `draw`,
clipped EndFrame) and the static tail (also type `draw`, at `BR` coords). The erase lookup
walks backward and stops at the tail — which is correct. But if someone writes:

```
[draw:test:TL]
[move:test:BR]
[erase:test]
```

The erase will find the static tail's position, set `eraseEvent.X/Y/W/H` from it, and
also truncate the tail's `EndFrame`. The original draw event is left intact with
`EndFrame = startFrame` (already clipped by the move). This seems correct by accident —
worth a unit test to lock in the behaviour.

---

### 6. `[text:]` style is captured at **timeline-build time**, not at **render time**

```go
textJobs = append(textJobs, textRenderJob{
    Style: currentStyle,  // captured from timeline loop
})
```

The text color (black vs white) is baked into the texture at render-prep time based on
the style active when the `[text]` line is processed. This is fine for a single style
per text block. But if the board style changes mid-reveal (e.g. `[style:blackboard]` fires
on the same frame the text starts), the texture colour won't update.

This is an acceptable simplification for now — just document it: the text colour is locked
to the board style active at the line where `[text:]` appears.

---

### 7. `splitLinesWithInlineWaits` — off-by-one in action range check

```go
if lastWordIdx == 0 {
    isMatch = (act.WordIndex >= 0 && act.WordIndex <= splitWordIdx)
} else {
    isMatch = (act.WordIndex > lastWordIdx && act.WordIndex <= splitWordIdx)
}
```

On the first segment (`lastWordIdx == 0`), `WordIndex == 0` is included. But `WordIndex == 0`
means "trigger at line start, before any word." After the split, the adjusted `WordIndex`
becomes `0 - 0 = 0`, which is still "trigger at line start." This is correct behaviour —
just non-obvious. Consider adding a comment.

The `>` vs `>=` asymmetry (first segment uses `>=0`, subsequent use `>lastWordIdx`) means
a draw action sitting exactly at a wait boundary is included in the preceding segment. That
is the right call, but again worth a comment.

---

## 🟡 Polish / Minor Issues

### 8. `text.go` — font paths are Linux-only

```go
fontPath := "/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf"
```

This will silently fall through to the fallback on macOS and Windows. The fallback also
reads the same path, so `RenderText` will return an error on any non-Linux host.

**Fix:** Bundle a small TTF (e.g. Go's `golang.org/x/image/font/gofont/goregular`) as a
compile-time embed, and use the system font only as an override. This removes the host
dependency entirely.

```go
import "golang.org/x/image/font/gofont/goregular"
// f, _ := opentype.Parse(goregular.TTF)
```

---

### 9. `[erase:asset]` — no position fallback when asset is not in timeline

If `[erase:nonexistent]` is written, `found` stays false (the loop finds nothing), and
`eraseEvent.X/Y/W/H` remain zero. The event is still appended to the timeline with zero
dimensions. In `RenderFrame`, `img.Bounds().Dx() == 0` → `destRect` is a zero-sized rect →
`draw.DrawMask` silently draws nothing. No crash, but no warning either.

**Fix:** Skip appending the erase event if `!found`, and log a warning.

---

### 10. `invertImageColors` — premultiplied alpha not handled

```go
r8 := uint8(r >> 8)
dst.Set(x, y, color.RGBA{255 - r8, 255 - g8, 255 - b8, a8})
```

`color.RGBA.RGBA()` returns premultiplied alpha values (components already multiplied by
`a/255`). For a semi-transparent pixel, `r >> 8` is already `< r_original`. Inverting
premultiplied values produces incorrect results for partially-transparent assets.

**Fix:** Un-premultiply before inverting:
```go
r8 := uint8((r >> 8) * 255 / uint32(a8))
```
Or use `color.NRGBAModel.Convert(c)` to get non-premultiplied components.

For fully-opaque assets (the common case) this makes no visible difference — but chalk
drawings often have soft anti-aliased edges that are semi-transparent.

---

### 11. `[subtitle:off]` hides subtitles but not for the next chunk boundary

The style lookup in `ass.go` sets the style at chunk start time:
```go
for _, ev := range events {
    if startTimeSec >= ev.Time {
        state = ev.State
    }
}
```

A subtitle chunk that started before the `off` event but ends after it will use the style
active at chunk-start. So a subtitle line that spans `[subtitle:off]` will remain visible
until the next chunk boundary (every 8 words by default). This is a minor visual artifact
— visible only if `[subtitle:off]` fires mid-chunk.

No action needed, just worth knowing.

---

## ✅ What's Done Well

- **`scaleImage` → `CatmullRom`**: exactly the right swap, clean.
- **Multi-style mask** (`ltr`/`ttb`/`diagonal`) with per-event `MaskStyle`: well-structured,
  makes the mask a real component rather than a hardcoded algorithm.
- **`RegisterAsset`**: clean API addition to `Engine`, avoids touching the file-loading path.
- **Chapter metadata via FFMETADATA**: correct use of `TIMEBASE=1/1000`, proper index
  arithmetic for the `-map_metadata` argument.
- **`styleStates []string` per frame**: simple and correct; avoids any synchronisation
  issues since it's written before workers start.
- **`FreezeFrames` default 60**: good default, properly extends both `totalFrames` and
  `extendedDuration`.
- **Hand sprite variants** loaded from `hand_pencil.png` etc. with graceful fallback to
  default: the right approach. No crash if files are absent.
- **`splitLinesWithInlineWaits`**: the inline wait fix is substantial work, and the approach
  of splitting into sub-lines before TTS is the correct architecture (avoids any
  timing-re-estimation downstream).

---

## Summary Table

| # | Severity | Issue |
|---|---|---|
| 1 | 🔴 Bug | `TriggerAfterWord: true` for all actions breaks draw timing |
| 2 | 🔴 Bug | `gen` fallback hardcoded to `robot.png` — silent miss if absent |
| 3 | 🔴 Bug | `EndFrame: 999999` sentinel breaks on long videos |
| 4 | 🟠 Fragile | BGM stream index hardcoded in filter_complex; diverges between preview/non-preview |
| 5 | 🟠 Logic | `erase` after `move` works by accident — needs a test |
| 6 | 🟠 Design | Text colour baked at parse time; document the limitation |
| 7 | 🟡 Minor | `splitLinesWithInlineWaits` boundary conditions worth commenting |
| 8 | 🟡 Minor | Font paths Linux-only; embed a fallback TTF |
| 9 | 🟡 Minor | `[erase:nonexistent]` silently appends zero-sized event |
| 10 | 🟡 Minor | `invertImageColors` premultiplied-alpha bug affects anti-aliased edges |
| 11 | 🟡 Minor | `[subtitle:off]` mid-chunk artifact (cosmetic) |

Fix issues 1–3 before next release. The rest can be deferred.
