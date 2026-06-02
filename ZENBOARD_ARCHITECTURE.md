# Zen-Board Architecture & Feature Progress

Zen-Board is a lightweight, pure-scripting whiteboard video generator. It synthesizes voiceover audio, aligns visual reveals using precise word timings, and composites frames in a multi-threaded Go rendering pool before streaming them to FFmpeg/ffplay.

---

## 1. Architectural Overview

```
              [ .zen Script File ]
                       │
                       ▼
           [ 1. Script Parsing & AST ]
                       │
        ┌──────────────┴──────────────┐
        ▼                             ▼
[ 2a. Parallel TTS ]          [ 2b. Coordinate Layout ]
 (with Word Timings)           (Auto 3x2 grid or absolute)
        │                             │
        └──────────────┬──────────────┘
                       ▼
            [ 3. Timeline Generator ]
                  (FrameEvents)
                       │
        ┌──────────────┴──────────────┐
        ▼                             ▼
[ 4a. ASS Subtitle ]          [ 4b. Render Pool ]
(Karaoke highlight)            (Parallel frame workers)
        │                             │
        │                       [ White Background ]
        │                             │
        │                       [ Masked reveals ]
        │                             │
        │                       [ Hand tracking ]
        │                             │
        └──────────────┬──────────────┘
                       ▼
           [ 5. FFmpeg/ffplay Pipe ]
                       │
                       ▼
            [ Output Video (MP4) ]
```

### Components

1. **Script Parsing (`internal/script/`)**:
   - Parses the custom `.zen` text format.
   - Extracts inline actions like `[draw:asset_name@x,y,w,h]`, `[wait:seconds]`, and `[clear]`.
   - Associates each draw action with the preceding word index in the script line.

2. **TTS Client & Parsing (`internal/tts/`)**:
   - Sends sentences in parallel to the local `zen-tts` server.
   - Retrieves WAV files and exact token-level word timestamps (falling back to syllable-count heuristics if the server does not support timestamps).
   - Concatenates the individual WAV chunks and silent intervals into a single master audio track.

3. **Layout & Timeline Engine (`internal/model/`, `cmd/zen-board/`)**:
   - Positions visual assets. If coordinates are omitted, centers the images inside cells of a dynamic 3-column, 2-row grid.
   - Maps actions to start/end frames based on the synchronized word timings.
   - Creates a unified `Timeline` of frame events.

4. **Multi-Threaded Rendering Pool (`internal/render/`)**:
   - Launches a pool of parallel workers to process frame jobs.
   - Applies a diagonal sine-wave alpha-mask to reveal drawing assets progressively.
   - Overlays the drawing hand sprite, tracking the current reveal frontier with breathing jitter.

5. **Subtitle Generation (`internal/subtitle/`)**:
   - Generates an Advanced Substation Alpha (`.ass`) file with karaoke timing (`{\k}`) tags for word-by-word highlighted captions.

6. **FFmpeg Integration (`internal/ffmpeg/`)**:
   - Streams raw RGBA buffers directly via standard input to `ffmpeg` or `ffplay`.
   - Combines video frames, the master audio track, and the ASS subtitles without writing temporary uncompressed videos to disk.

---

## 2. Feature Progress Checklist

### Core Pipeline
- [x] Parallel TTS synthesis with `zen-tts` integration.
- [x] Precise word/token timestamp extraction from ONNX model durations.
- [x] Robust syllable-based timing fallback for legacy TTS engines.
- [x] Flexible `.zen` script syntax (`[draw]`, `[wait]`, `[clear]`).
- [x] Auto-positioning 3-column, 2-row grid layout.
- [x] Absolute coordinate positioning override (`@x,y,w,h`).
- [x] Multi-threaded render pool with reusable RGBA buffer pools.
- [x] Progressive diagonal sine-wave reveal mask (simulating pencil sketching).
- [x] Floating hand sprite tracking the reveal frontier with breathing jitter.
- [x] Precise ASS karaoke subtitle generation and styling.
- [x] Real-time preview mode via `ffplay` integration (`-preview`).

### Roadmap Features (Golpo AI Alignment)
- [x] **Background Music (BGM) constant mix** (Done: mix background loop at constant low volume).
- [x] **Dynamic Virtual Camera zoom/pan presets** (Done: smoothly zoom in/out on grid quadrants using [zoom:preset] commands).
- [x] **Extended layout presets** (Done: support TL, TR, BL, BR, HT, HB, LH, RH layout presets in transcription scripts).
- [x] **YAML Manifest / Intermediate Representation (IR)** (Discarded: kept simple text transcription format per user feedback).
- [x] **Drawing SFX** (Discarded: kept simple and elegant per user feedback).

