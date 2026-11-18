"""Turns screencast frames into a README clip.

    python docs/showcase/encode.py <frames-dir> <out-base> [--width 1200] [--max-hold 0.7] [--fps 12]

Writes <out-base>.webp (animated) and <out-base>.png (the last frame, used as
the poster). Frames keep their real timing, except that any stretch where the
screen does not change is cut down to --max-hold seconds, so waiting on the
model never drags the clip out.
"""

import argparse
import json
import os

from PIL import Image, ImageChops

ap = argparse.ArgumentParser()
ap.add_argument("frames")
ap.add_argument("out")
ap.add_argument("--width", type=int, default=1200)
ap.add_argument("--max-hold", type=float, default=0.7)
ap.add_argument("--fps", type=float, default=12)
a = ap.parse_args()

log = json.load(open(os.path.join(a.frames, "frames.json"), encoding="utf8"))
step = 1 / a.fps
out, durations = [], []
last_img, last_t = None, None
for i, f in enumerate(log):
    t = f["t"]
    if last_t is not None and t - last_t < step and i != len(log) - 1:
        continue
    img = Image.open(os.path.join(a.frames, f["name"])).convert("RGB")
    if a.width and img.width > a.width:
        img = img.resize((a.width, round(img.height * a.width / img.width)), Image.LANCZOS)
    if last_img is not None:
        gap = min(t - last_t, a.max_hold)
        if ImageChops.difference(img, last_img).getbbox() is None:
            durations[-1] = min(durations[-1] + gap, a.max_hold)
            last_t = t
            continue
        durations[-1] = gap
    out.append(img)
    durations.append(step)
    last_img, last_t = img, t
durations[-1] = 2.5  # Hold on the result.

ms = [max(40, round(d * 1000)) for d in durations]
out[0].save(a.out + ".webp", save_all=True, append_images=out[1:], duration=ms, loop=0, quality=82, method=6)
out[-1].save(a.out + ".png", optimize=True)
print(f"{len(out)} frames, {sum(ms) / 1000:.1f}s, {os.path.getsize(a.out + '.webp') // 1024} KB")
