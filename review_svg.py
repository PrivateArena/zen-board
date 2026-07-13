#!/usr/bin/env python3
"""
Batch review SVG rendering output:
  1. Extract frames from rendered video
  2. Upload to bridge AI for visual quality inspection
  3. Print PASS/FAIL with diagnostics

Usage:
  ./review_svg.py [video-path] [--fps 2] [--max-frames 10] [--provider gemini|claude]
"""
import argparse, json, os, subprocess, sys, tempfile, time
from pathlib import Path

BRIDGE_URL = "http://127.0.0.1:9999"

def extract_frames(video: str, outdir: str, fps: int) -> list[Path]:
    print(f"== Extracting frames at {fps} fps ==")
    pat = str(Path(outdir) / "frame-%04d.png")
    subprocess.run(
        ["ffmpeg", "-y", "-i", video, "-vf", f"fps={fps}", "-q:v", "3", pat],
        capture_output=True, check=True,
    )
    frames = sorted(Path(outdir).glob("frame-*.png"))
    print(f"  Extracted {len(frames)} frames")
    return frames

def upload_frames(frames: list[Path], provider: str, message: str) -> dict:
    print(f"== Uploading {len(frames)} frames to {provider} ==")
    body = {
        "action": "chat",
        "provider": provider,
        "message": message,
        "path": [str(f) for f in frames],
        "take_screenshot": False,
    }
    print(f"  Body: {len(json.dumps(body))} bytes, {len(frames)} files")
    try:
        import urllib.request
        req = urllib.request.Request(
            BRIDGE_URL,
            data=json.dumps(body).encode(),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=120) as resp:
            result = json.loads(resp.read().decode())
    except Exception as e:
        print(f"  Upload failed: {e}")
        sys.exit(1)

    print(f"  Response: {json.dumps(result, indent=2)[:2000]}")
    return result

def check_frames_content(frames: list[Path]) -> list[dict]:
    """Quick local sanity check: detect blank frames."""
    results = []
    from PIL import Image
    import numpy as np
    for f in frames:
        arr = np.array(Image.open(f))
        non_white = np.sum(np.any(arr != 255, axis=-1))
        total = arr.shape[0] * arr.shape[1]
        pct = 100 * non_white / total
        results.append({"frame": f.name, "non_white_pct": round(pct, 2), "non_white_px": int(non_white)})
    return results

DEFAULT_MESSAGE = """You are a rendering quality assurance tool.
Review the video frames for SVG rendering accuracy.
Check for:
1) Are the SVG graphics rendering properly?
2) Are variant colors correctly applied?
3) Are non-variant shapes present?
4) Overall rendering quality.
Provide a status (PASS/FAIL) and a short summary."""

def main():
    parser = argparse.ArgumentParser(description="Review SVG rendering output")
    parser.add_argument("video", nargs="?", default="/tmp/zen-svg-test11.mp4", help="Path to rendered video")
    parser.add_argument("--fps", type=int, default=2, help="Frames per second to extract")
    parser.add_argument("--max-frames", type=int, default=10, help="Max frames to upload (0 = all)")
    parser.add_argument("--provider", choices=["gemini", "claude"], default="gemini", help="AI provider")
    parser.add_argument("--message", default=DEFAULT_MESSAGE, help="Prompt for AI review")
    parser.add_argument("--keep-frames", action="store_true", help="Keep extracted frames after review")
    args = parser.parse_args()

    if not os.path.isfile(args.video):
        print(f"ERROR: Video not found: {args.video}")
        sys.exit(1)

    with tempfile.TemporaryDirectory(prefix="zen-svg-review-") as tmpdir:
        frames = extract_frames(args.video, tmpdir, args.fps)
        if not frames:
            print("ERROR: No frames extracted")
            sys.exit(1)

        print("\n== Local sanity check ==")
        checks = check_frames_content(frames)
        blank_frames = [c for c in checks if c["non_white_pct"] < 0.5]
        content_frames = [c for c in checks if c["non_white_pct"] >= 0.5]
        if blank_frames:
            print(f"  WARNING: {len(blank_frames)}/{len(checks)} frames appear blank (<0.5% non-white)")
            for c in blank_frames[:5]:
                print(f"    {c['frame']}: {c['non_white_pct']}% non-white")
        else:
            print(f"  All {len(checks)} frames have content")
        if content_frames:
            avg_pct = sum(c["non_white_pct"] for c in content_frames) / len(content_frames)
            print(f"  Content frames: {len(content_frames)}, avg {avg_pct:.2f}% non-white")

        selected = frames[:args.max_frames] if args.max_frames > 0 else frames
        print(f"\n  Uploading {len(selected)}/{len(frames)} frames for AI review")

        result = upload_frames(selected, args.provider, args.message)

        ai_text = ""
        if isinstance(result, dict):
            ai_text = result.get("response", result.get("text", json.dumps(result)))
        print(f"\n== AI Review Result ==")
        print(ai_text[:3000])

        if not args.keep_frames:
            print("  (frames cleaned up)")

if __name__ == "__main__":
    main()
