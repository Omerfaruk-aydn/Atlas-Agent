"""Turns screencast frames into a README clip.

    python docs/showcase/encode.py <frames-dir> <out-base> [options]

Writes <out-base>.webp (animated) and <out-base>.png (the last frame, used as
the poster).

The first --intro seconds (the typing) play at normal speed. Everything after
that is sampled --speed times sparser, so a ten minute run becomes a short
time-lapse. Frames are picked from the recording's own clock one at a time, so
memory stays flat however long the recording is. Frames identical to the one
before are merged, but never held longer than --max-hold.
"""

import argparse
import json
import os

from PIL import Image, ImageChops

ap = argparse.ArgumentParser()
ap.add_argument("frames")
ap.add_argument("out")
ap.add_argument("--width", type=int, default=1000)
ap.add_argument("--fps", type=float, default=8)
ap.add_argument("--speed", type=float, default=1.0, help="Time-lapse factor after --intro seconds")
ap.add_argument("--intro", type=float, default=6.0, help="Seconds kept at normal speed")
ap.add_argument("--max-hold", type=float, default=0.5)
ap.add_argument("--quality", type=int, default=70)
ap.add_argument("--gif", action="store_true", help="Write a 256-colour GIF instead of a WebP")
ap.add_argument("--hold", type=float, default=3.5, help="Seconds to hold on the last frame")
a = ap.parse_args()

log = json.load(open(os.path.join(a.frames, "frames.json"), encoding="utf8"))
step = 1 / a.fps
t0 = log[0]["t"]

picked, next_t = [], t0
for i, f in enumerate(log):
    if f["t"] >= next_t or i == len(log) - 1:
        picked.append(f)
        next_t = f["t"] + (step if f["t"] - t0 < a.intro else step * a.speed)

out, ms = [], []
prev = None
for f in picked:
    img = Image.open(os.path.join(a.frames, f["name"])).convert("RGB")
    if a.width and img.width > a.width:
        img = img.resize((a.width, round(img.height * a.width / img.width)), Image.LANCZOS)
    if prev is not None and ImageChops.difference(img, prev).getbbox() is None:
        ms[-1] = min(ms[-1] + round(step * 1000), round(a.max_hold * 1000))
        continue
    out.append(img)
    ms.append(round(step * 1000))
    prev = img
ms[-1] = round(a.hold * 1000)

if a.gif:
    # One palette for the whole clip, built from frames spread across it, so
    # colours stay stable and text stays crisp; no dithering keeps flat areas
    # flat and lets the GIF writer store only the pixels that changed.
    probe = [out[i] for i in sorted({0, len(out) // 4, len(out) // 2, 3 * len(out) // 4, len(out) - 1})]
    sheet = Image.new("RGB", (probe[0].width, probe[0].height * len(probe)))
    for i, im in enumerate(probe):
        sheet.paste(im, (0, i * probe[0].height))
    pal = sheet.quantize(colors=256, method=Image.Quantize.MEDIANCUT, dither=Image.Dither.NONE)
    q = [im.quantize(palette=pal, dither=Image.Dither.NONE) for im in out]
    q[0].save(a.out + ".gif", save_all=True, append_images=q[1:], duration=ms, loop=0, optimize=False, disposal=1)
    out[-1].save(a.out + ".png", optimize=True)
    print(f"{len(out)} frames, {sum(ms) / 1000:.1f}s, {os.path.getsize(a.out + '.gif') // 1024} KB")
    raise SystemExit
out[0].save(a.out + ".webp", save_all=True, append_images=out[1:], duration=ms, loop=0, quality=a.quality, method=4)
out[-1].save(a.out + ".png", optimize=True)
print(f"{len(out)} frames, {sum(ms) / 1000:.1f}s, {os.path.getsize(a.out + '.webp') // 1024} KB")
